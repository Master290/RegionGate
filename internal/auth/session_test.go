package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionServiceHasJoined(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("username") != "Daniar" || request.URL.Query().Get("serverId") != "hash" || request.URL.Query().Get("ip") != "203.0.113.4" {
			t.Errorf("query=%v", request.URL.Query())
		}
		_, _ = response.Write([]byte(`{"id":"00112233445566778899aabbccddeeff","name":"Daniar","properties":[{"name":"textures","value":"value","signature":"signature"}]}`))
	}))
	defer server.Close()

	profile, err := (SessionService{Client: server.Client(), URL: server.URL}).HasJoined(context.Background(), "Daniar", "hash", "203.0.113.4")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Username != "Daniar" || profile.UUID[0] != 0 || profile.UUID[15] != 0xff || len(profile.Properties) != 1 {
		t.Fatalf("profile=%+v", profile)
	}
}

func TestSessionServiceRejectsMissingJoin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, err := (SessionService{Client: server.Client(), URL: server.URL}).HasJoined(context.Background(), "Daniar", "hash", "")
	if !errors.Is(err, ErrNotJoined) {
		t.Fatalf("error=%v", err)
	}
}

func TestSessionServiceRejectsMismatchedProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"id":"00112233445566778899aabbccddeeff","name":"Other"}`))
	}))
	defer server.Close()
	_, err := (SessionService{Client: server.Client(), URL: server.URL}).HasJoined(context.Background(), "Daniar", "hash", "")
	if !errors.Is(err, ErrNotJoined) {
		t.Fatalf("error=%v", err)
	}
}
