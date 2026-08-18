package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/handshake"
	"github.com/Master290/RegionGate/internal/protocol/status"
	"github.com/Master290/RegionGate/internal/server"
	"github.com/Master290/RegionGate/internal/transfer"
)

func Run(ctx context.Context, config Config, logger *slog.Logger) error {
	var coordinator *transfer.Coordinator
	if config.BackendAddress != "" {
		forwarder, err := forwarding.NewModernForwarding([]byte(config.VelocitySecret))
		if err != nil {
			return err
		}
		dialer := backend.NewDialer(backend.Config{Address: config.BackendAddress, Host: config.BackendHost, Port: config.BackendPort, MaxPacketSize: config.MaxPacketSize})
		coordinator = transfer.NewCoordinator(dialer, forwarder, transfer.Config{})
	}

	gateway := server.New(server.Config{
		MaxConnections: config.MaxConnections, MaxConnectionsPerIP: config.MaxConnectionsPerIP, MaxPacketSize: config.MaxPacketSize,
		KeepAliveInterval: config.KeepAliveInterval, KeepAliveTimeout: config.KeepAliveTimeout,
		TransferCoordinator: coordinator,
		Status: status.Response{
			Version:     status.Version{Name: "1.20.4", Protocol: handshake.ProtocolVersion},
			Players:     status.Players{Max: config.MaxConnections},
			Description: status.Description{Text: "RegionGate"},
		},
	}, logger)
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
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = health.Shutdown(shutdownCtx)
	}()

	logger.Info("minecraft gateway listening", "address", listener.Addr())
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- gateway.Serve(ctx, listener) }()
	select {
	case err := <-serveErrors:
		return err
	case err := <-healthErrors:
		if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
			return nil
		}
		return err
	case <-ctx.Done():
		return <-serveErrors
	}
}

func healthHandler(gateway *server.Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"status": "ok", "sessions": gateway.SessionCount()})
	})
	return mux
}
