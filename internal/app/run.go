package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Master290/RegionGate/internal/auth"
	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/botfilter"
	"github.com/Master290/RegionGate/internal/challenge"
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
	var onlineAuthenticator *auth.Authenticator
	var botManager *botfilter.Manager
	var challengeHook challenge.Hook
	if config.BotFilterConfigFile != "" {
		policy, err := botfilter.LoadFile(config.BotFilterConfigFile)
		if err != nil {
			if os.IsNotExist(err) {
				logger.Warn("bot filter disabled: policy file does not exist", "path", config.BotFilterConfigFile)
			} else {
				return fmt.Errorf("load bot filter policy: %w", err)
			}
		} else {
			botManager = botfilter.New(policy, config.BotFilterConfigFile)
			botManager.Start(runCtx)
			if policy.BotFilter.Enabled {
				challengeHook = botManager
			}
		}
	} else {
		logger.Warn("bot filter disabled: REGIONGATE_CONFIG_FILE is not set")
	}
	if config.OnlineMode {
		var err error
		onlineAuthenticator, err = auth.NewAuthenticator(auth.SessionService{URL: config.SessionServerURL})
		if err != nil {
			return err
		}
	}
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
		OnlineAuthenticator: onlineAuthenticator,
		BotFilter:           botManager,
		ChallengeHook:       challengeHook,
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

	health := &http.Server{Addr: config.HealthAddress, Handler: healthHandler(gateway, config.AdminToken), ReadHeaderTimeout: 5 * time.Second}
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

func healthHandler(gateway *server.Server, adminToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"status": "ok", "sessions": gateway.SessionCount()})
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		metrics := gateway.Metrics()
		// A backend is probed during admission, so the initial unknown state must
		// not prevent the process from becoming ready. Explicitly unhealthy is
		// the only state that should fail readiness.
		ready := !metrics.BackendConfigured || metrics.BackendHealthState != 2
		response.Header().Set("Content-Type", "application/json")
		if !ready {
			response.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"ready": ready, "backend_health_state": metrics.BackendHealthState})
	})
	mux.Handle("GET /metrics", metricsHandler(gateway))
	mux.Handle("GET /admin/status", adminStatusHandler(gateway, adminToken))
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
			{"regiongate_backend_health_state", metrics.BackendHealthState},
		}
		for _, metric := range values {
			_, _ = response.Write([]byte(metric.name + " " + strconv.FormatUint(metric.value, 10) + "\n"))
		}
		for _, metric := range []struct {
			name  string
			value uint64
		}{
			{"regiongate_botfilter_allowed_total", metrics.BotFilter.Allowed},
			{"regiongate_botfilter_observed_total", metrics.BotFilter.Observed},
			{"regiongate_botfilter_denied_total", metrics.BotFilter.Denied},
			{"regiongate_botfilter_active_observations", metrics.BotFilter.ActiveObservations},
			{"regiongate_botfilter_reputation_entries", metrics.BotFilter.ReputationEntries},
			{"regiongate_botfilter_reload_failures_total", metrics.BotFilter.ReloadFailures},
		} {
			_, _ = response.Write([]byte(metric.name + " " + strconv.FormatUint(metric.value, 10) + "\n"))
		}
	})
}

func adminStatusHandler(gateway *server.Server, expectedToken string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if expectedToken == "" {
			http.NotFound(response, request)
			return
		}
		provided, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(expectedToken)) != 1 {
			response.Header().Set("WWW-Authenticate", `Bearer realm="regiongate-admin"`)
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(gateway.Metrics())
	})
}
