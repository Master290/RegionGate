package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/server"
)

func TestLoadConfigDefaultsAndBackendValidation(t *testing.T) {
	config, err := loadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddress != ":25565" || config.HealthAddress != "127.0.0.1:8080" || config.PprofAddress != "" || config.MaxConnections != 10000 {
		t.Fatalf("config=%+v", config)
	}
	values := map[string]string{"REGIONGATE_BACKEND_ADDRESS": "127.0.0.1:25566"}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected incomplete backend configuration error")
	}
}

func TestLoadConfigPprofAddress(t *testing.T) {
	values := map[string]string{"REGIONGATE_PPROF_LISTEN": "127.0.0.1:6060"}
	config, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.PprofAddress != "127.0.0.1:6060" {
		t.Fatalf("pprof address=%q", config.PprofAddress)
	}
}

func TestLoadConfigOnlineMode(t *testing.T) {
	values := map[string]string{
		"REGIONGATE_ONLINE_MODE":        "true",
		"REGIONGATE_SESSION_SERVER_URL": "http://session.test/hasJoined",
	}
	config, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !config.OnlineMode || config.SessionServerURL != values["REGIONGATE_SESSION_SERVER_URL"] {
		t.Fatalf("config=%+v", config)
	}
	values["REGIONGATE_ONLINE_MODE"] = "sometimes"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("invalid online-mode value was accepted")
	}
}

func TestLoadConfigLoginRateLimit(t *testing.T) {
	values := map[string]string{
		"REGIONGATE_LOGIN_RATE_LIMIT":  "25",
		"REGIONGATE_LOGIN_RATE_WINDOW": "30s",
	}
	config, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.LoginRateLimit != 25 || config.LoginRateWindow != 30*time.Second {
		t.Fatalf("login rate config=%+v", config)
	}

	values["REGIONGATE_LOGIN_RATE_WINDOW"] = "invalid"
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected invalid login rate window error")
	}
}

func TestHealthHandlerReportsSessionCount(t *testing.T) {
	request := httptest.NewRequest("GET", "/healthz", nil)
	response := httptest.NewRecorder()
	healthHandler(server.New(server.Config{}, nil)).ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status=%d", response.Code)
	}
	var payload struct {
		Status   string `json:"status"`
		Sessions int    `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "ok" || payload.Sessions != 0 {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestPprofHandlerIsSeparateFromHealthHandler(t *testing.T) {
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		pprofHandler().ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("path=%s status=%d", path, response.Code)
		}
	}

	request := httptest.NewRequest("GET", "/debug/pprof/", nil)
	response := httptest.NewRecorder()
	healthHandler(server.New(server.Config{}, nil)).ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("health handler exposed pprof status=%d", response.Code)
	}
}

func TestMetricsAndAdminStatusExposeGatewaySnapshot(t *testing.T) {
	gateway := server.New(server.Config{}, nil)
	metricsResponse := httptest.NewRecorder()
	metricsHandler(gateway).ServeHTTP(metricsResponse, httptest.NewRequest("GET", "/metrics", nil))
	if metricsResponse.Code != 200 || !strings.Contains(metricsResponse.Body.String(), "regiongate_sessions_active 0\n") || !strings.Contains(metricsResponse.Body.String(), "regiongate_backend_health_state 0\n") {
		t.Fatalf("metrics status=%d body=%q", metricsResponse.Code, metricsResponse.Body.String())
	}
	if got := metricsResponse.Header().Get("Content-Type"); got != "text/plain; version=0.0.4" {
		t.Fatalf("metrics content type=%q", got)
	}

	adminResponse := httptest.NewRecorder()
	adminStatusHandler(gateway).ServeHTTP(adminResponse, httptest.NewRequest("GET", "/admin/status", nil))
	var snapshot server.MetricsSnapshot
	if err := json.NewDecoder(adminResponse.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if adminResponse.Code != 200 || snapshot.Sessions != 0 {
		t.Fatalf("admin status=%d snapshot=%+v", adminResponse.Code, snapshot)
	}
}

func TestRunServesAndStopsPprofEndpoint(t *testing.T) {
	pprofAddress := freeTCPAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		done <- Run(ctx, Config{
			ListenAddress: "127.0.0.1:0",
			HealthAddress: "127.0.0.1:0",
			PprofAddress:  pprofAddress,
		}, logger)
	}()

	deadline := time.Now().Add(5 * time.Second)
	client := &http.Client{Timeout: time.Second}
	for {
		response, err := client.Get("http://" + pprofAddress + "/debug/pprof/")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("pprof status=%d", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pprof endpoint did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
