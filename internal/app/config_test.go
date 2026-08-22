package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/server"
	"github.com/Master290/RegionGate/internal/transfer"
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

func TestLoadConfigAdminToken(t *testing.T) {
	valid := "01234567890123456789012345678901"
	config, err := loadConfig(func(key string) string {
		if key == "REGIONGATE_ADMIN_TOKEN" {
			return valid
		}
		return ""
	})
	if err != nil || config.AdminToken != valid {
		t.Fatalf("config=%+v error=%v", config, err)
	}
	if _, err := loadConfig(func(key string) string {
		if key == "REGIONGATE_ADMIN_TOKEN" {
			return "short"
		}
		return ""
	}); err == nil {
		t.Fatal("short admin token was accepted")
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

func TestLoadConfigSessionLimits(t *testing.T) {
	values := map[string]string{
		"REGIONGATE_MAX_PACKET_SIZE":       "1048576",
		"REGIONGATE_HANDSHAKE_TIMEOUT":     "2s",
		"REGIONGATE_STATUS_TIMEOUT":        "3s",
		"REGIONGATE_LOGIN_TIMEOUT":         "4s",
		"REGIONGATE_WRITE_TIMEOUT":         "5s",
		"REGIONGATE_KEEPALIVE_INTERVAL":    "6s",
		"REGIONGATE_KEEPALIVE_TIMEOUT":     "7s",
		"REGIONGATE_QUEUE_STATUS_INTERVAL": "8s",
		"REGIONGATE_CHALLENGE_TIMEOUT":     "9s",
	}
	config, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.MaxPacketSize != 1048576 || config.HandshakeTimeout != 2*time.Second || config.StatusTimeout != 3*time.Second || config.LoginTimeout != 4*time.Second || config.WriteTimeout != 5*time.Second || config.KeepAliveInterval != 6*time.Second || config.KeepAliveTimeout != 7*time.Second || config.QueueStatusInterval != 8*time.Second || config.ChallengeTimeout != 9*time.Second {
		t.Fatalf("config=%+v", config)
	}
}

func TestLoadConfigRejectsInvalidSessionLimit(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "packet size", value: "0"},
		{name: "login timeout", value: "invalid"},
		{name: "keepalive timeout", value: "0s"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			switch test.name {
			case "packet size":
				values["REGIONGATE_MAX_PACKET_SIZE"] = test.value
			case "login timeout":
				values["REGIONGATE_LOGIN_TIMEOUT"] = test.value
			case "keepalive timeout":
				values["REGIONGATE_KEEPALIVE_TIMEOUT"] = test.value
			}
			if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
				t.Fatal("invalid session limit was accepted")
			}
		})
	}
}

func TestHealthHandlerReportsSessionCount(t *testing.T) {
	request := httptest.NewRequest("GET", "/healthz", nil)
	response := httptest.NewRecorder()
	healthHandler(server.New(server.Config{}, nil), "").ServeHTTP(response, request)
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
	healthHandler(server.New(server.Config{}, nil), "").ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("health handler exposed pprof status=%d", response.Code)
	}
}

func TestMetricsAndAdminStatusExposeGatewaySnapshot(t *testing.T) {
	gateway := server.New(server.Config{}, nil)
	readyResponse := httptest.NewRecorder()
	healthHandler(gateway, "").ServeHTTP(readyResponse, httptest.NewRequest("GET", "/readyz", nil))
	if readyResponse.Code != http.StatusOK || !strings.Contains(readyResponse.Body.String(), `"ready":true`) {
		t.Fatalf("readiness status=%d body=%q", readyResponse.Code, readyResponse.Body.String())
	}

	metricsResponse := httptest.NewRecorder()
	metricsHandler(gateway).ServeHTTP(metricsResponse, httptest.NewRequest("GET", "/metrics", nil))
	if metricsResponse.Code != 200 || !strings.Contains(metricsResponse.Body.String(), "regiongate_sessions_active 0\n") || !strings.Contains(metricsResponse.Body.String(), "regiongate_backend_health_state 0\n") {
		t.Fatalf("metrics status=%d body=%q", metricsResponse.Code, metricsResponse.Body.String())
	}
	if got := metricsResponse.Header().Get("Content-Type"); got != "text/plain; version=0.0.4" {
		t.Fatalf("metrics content type=%q", got)
	}

	adminResponse := httptest.NewRecorder()
	adminRequest := httptest.NewRequest("GET", "/admin/status", nil)
	adminRequest.Header.Set("Authorization", "Bearer 01234567890123456789012345678901")
	adminStatusHandler(gateway, "01234567890123456789012345678901").ServeHTTP(adminResponse, adminRequest)
	var snapshot server.MetricsSnapshot
	if err := json.NewDecoder(adminResponse.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if adminResponse.Code != 200 || snapshot.Sessions != 0 {
		t.Fatalf("admin status=%d snapshot=%+v", adminResponse.Code, snapshot)
	}
}

func TestReadinessBackendStates(t *testing.T) {
	testCases := []struct {
		name       string
		configured bool
		state      backend.HealthState
		wantReady  bool
		wantStatus int
	}{
		{name: "backend disabled", wantReady: true, wantStatus: http.StatusOK},
		{name: "backend unknown", configured: true, state: backend.HealthUnknown, wantReady: true, wantStatus: http.StatusOK},
		{name: "backend healthy", configured: true, state: backend.HealthHealthy, wantReady: true, wantStatus: http.StatusOK},
		{name: "backend unhealthy", configured: true, state: backend.HealthUnhealthy, wantReady: false, wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			gateway := server.New(server.Config{}, nil)
			if test.configured {
				dialer := backend.NewDialer(backend.Config{Address: "127.0.0.1:25566"})
				forwarder, err := forwarding.NewModernForwarding([]byte("readiness-test-secret"))
				if err != nil {
					t.Fatal(err)
				}
				coordinator := transfer.NewCoordinator(dialer, forwarder, transfer.Config{})
				gateway = server.New(server.Config{TransferCoordinator: coordinator}, nil)
				switch test.state {
				case backend.HealthHealthy:
					dialer.MarkHealthy()
				case backend.HealthUnhealthy:
					dialer.MarkUnhealthy(errors.New("backend unavailable"))
				}
			}
			response := httptest.NewRecorder()
			healthHandler(gateway, "").ServeHTTP(response, httptest.NewRequest("GET", "/readyz", nil))
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"ready":`+strconv.FormatBool(test.wantReady)) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminStatusRequiresConfiguredBearerToken(t *testing.T) {
	gateway := server.New(server.Config{}, nil)
	const token = "01234567890123456789012345678901"
	for _, test := range []struct {
		name   string
		token  string
		header string
		want   int
	}{
		{name: "disabled", want: http.StatusNotFound},
		{name: "missing", token: token, want: http.StatusUnauthorized},
		{name: "wrong", token: token, header: "Bearer wrong", want: http.StatusUnauthorized},
		{name: "valid", token: token, header: "Bearer " + token, want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/admin/status", nil)
			request.Header.Set("Authorization", test.header)
			response := httptest.NewRecorder()
			adminStatusHandler(gateway, test.token).ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d", response.Code, test.want)
			}
		})
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
