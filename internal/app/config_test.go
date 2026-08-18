package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Master290/RegionGate/internal/server"
)

func TestLoadConfigDefaultsAndBackendValidation(t *testing.T) {
	config, err := loadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.ListenAddress != ":25565" || config.HealthAddress != "127.0.0.1:8080" || config.MaxConnections != 10000 {
		t.Fatalf("config=%+v", config)
	}
	values := map[string]string{"REGIONGATE_BACKEND_ADDRESS": "127.0.0.1:25566"}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("expected incomplete backend configuration error")
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
