package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/status"
	admissionqueue "github.com/Master290/RegionGate/internal/queue"
	"github.com/Master290/RegionGate/internal/server"
	"github.com/Master290/RegionGate/internal/transfer"
)

func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var coordinator *transfer.Coordinator
	var fifo *admissionqueue.FIFO
	if config.BackendAddress != "" {
		forwarder, err := forwarding.NewModernForwarding([]byte(config.VelocitySecret))
		if err != nil {
			return err
		}
		dialer := backend.NewDialer(backend.Config{Address: config.BackendAddress, Host: config.BackendHost, Port: config.BackendPort, MaxPacketSize: config.MaxPacketSize})
		coordinator = transfer.NewCoordinator(dialer, forwarder, transfer.Config{})
		fifo = admissionqueue.New(config.QueueSize)
	}

	gateway := server.New(server.Config{
		MaxConnections: config.MaxConnections, MaxConnectionsPerIP: config.MaxConnectionsPerIP, MaxPacketSize: config.MaxPacketSize,
		KeepAliveInterval: config.KeepAliveInterval, KeepAliveTimeout: config.KeepAliveTimeout,
		LoginRateLimit: config.LoginRateLimit, LoginRateWindow: config.LoginRateWindow,
		TransferCoordinator: coordinator,
		AdmissionQueue:      fifo,
		Status: status.Response{
			Version:     status.Version{Name: "1.20.4", Protocol: handshake.ProtocolVersion},
			Players:     status.Players{Max: config.MaxConnections},
			Description: status.Description{Text: "RegionGate"},
		},
	}, logger)
	if fifo != nil {
		scheduler := admissionqueue.Scheduler{Queue: fifo, Interval: config.AdmissionInterval, OnResult: func(item admissionqueue.Item, err error) {
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Warn("queued admission failed", "id", item.ID, "error", err)
			}
		}}
		go scheduler.Run(runCtx)
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return err
	}
	defer listener.Close()

	health := &http.Server{Addr: config.HealthAddress, Handler: healthHandler(gateway), ReadHeaderTimeout: 5 * time.Second}
	healthErrors := make(chan error, 1)
	go func() {
		logger.Info("health endpoint listening", "address", config.HealthAddress)
		healthErrors <- health.ListenAndServe()
	}()
	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = health.Shutdown(shutdownCtx)
	}()
	var pprofErrors <-chan error
	if config.PprofAddress != "" {
		profileServer := &http.Server{Addr: config.PprofAddress, Handler: pprofHandler(), ReadHeaderTimeout: 5 * time.Second}
		errorsChannel := make(chan error, 1)
		pprofErrors = errorsChannel
		go func() {
			logger.Info("pprof endpoint listening", "address", config.PprofAddress)
			errorsChannel <- profileServer.ListenAndServe()
		}()
		go func() {
			<-runCtx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = profileServer.Shutdown(shutdownCtx)
		}()
	}

	logger.Info("minecraft gateway listening", "address", listener.Addr())
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- gateway.Serve(runCtx, listener) }()
	select {
	case err := <-serveErrors:
		return err
	case err := <-healthErrors:
		if errors.Is(err, http.ErrServerClosed) && runCtx.Err() != nil {
			return nil
		}
		return err
	case err := <-pprofErrors:
		if errors.Is(err, http.ErrServerClosed) && runCtx.Err() != nil {
			return nil
		}
		return err
	case <-runCtx.Done():
		return <-serveErrors
	}
}

func pprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

func healthHandler(gateway *server.Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"status": "ok", "sessions": gateway.SessionCount()})
	})
	mux.Handle("GET /metrics", metricsHandler(gateway))
	mux.Handle("GET /admin/status", adminStatusHandler(gateway))
	return mux
}

func metricsHandler(gateway *server.Server) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		metrics := gateway.Metrics()
		response.Header().Set("Content-Type", "text/plain; version=0.0.4")
		values := []struct {
			name  string
			value uint64
		}{
			{"regiongate_connections_active", metrics.ActiveConnections},
			{"regiongate_sessions_active", metrics.Sessions},
			{"regiongate_queue_length", metrics.QueueLength},
			{"regiongate_connections_accepted_total", metrics.Accepted},
			{"regiongate_connections_rejected_capacity_total", metrics.RejectedCapacity},
			{"regiongate_connections_rejected_ip_total", metrics.RejectedPerIP},
			{"regiongate_login_rate_limited_total", metrics.LoginRateLimited},
		}
		for _, metric := range values {
			_, _ = response.Write([]byte(metric.name + " " + strconv.FormatUint(metric.value, 10) + "\n"))
		}
	})
}

func adminStatusHandler(gateway *server.Server) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(gateway.Metrics())
	})
}
