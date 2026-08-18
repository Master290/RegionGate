package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultSessionURL = "https://sessionserver.mojang.com/session/minecraft/hasJoined"
	maxSessionBody    = 1 << 20
)

var ErrNotJoined = errors.New("player has not joined the Minecraft session")

type Property struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type Profile struct {
	UUID       [16]byte
	Username   string
	Properties []Property
}

type Verifier interface {
	HasJoined(context.Context, string, string, string) (Profile, error)
}

type VerifierFunc func(context.Context, string, string, string) (Profile, error)

func (f VerifierFunc) HasJoined(ctx context.Context, username, serverHash, address string) (Profile, error) {
	return f(ctx, username, serverHash, address)
}

type SessionService struct {
	Client *http.Client
	URL    string
}

func (s SessionService) HasJoined(ctx context.Context, username, serverHash, address string) (Profile, error) {
	endpoint := s.URL
	if endpoint == "" {
		endpoint = defaultSessionURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return Profile{}, err
	}
	query := parsed.Query()
	query.Set("username", username)
	query.Set("serverId", serverHash)
	if address != "" {
		query.Set("ip", address)
	}
	parsed.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Profile{}, err
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return Profile{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusForbidden {
		return Profile{}, ErrNotJoined
	}
	if response.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("Mojang session server returned %s", response.Status)
	}
	var document struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		Properties []Property `json:"properties"`
	}
	limited := &io.LimitedReader{R: response.Body, N: maxSessionBody + 1}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(&document); err != nil {
		return Profile{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Profile{}, errors.New("invalid Mojang session response")
	}
	if limited.N == 0 {
		return Profile{}, errors.New("Mojang session response exceeds limit")
	}
	if document.Name == "" || !strings.EqualFold(document.Name, username) {
		return Profile{}, ErrNotJoined
	}
	uuid, err := parseUUID(document.ID)
	if err != nil {
		return Profile{}, err
	}
	return Profile{UUID: uuid, Username: document.Name, Properties: document.Properties}, nil
}

func parseUUID(value string) ([16]byte, error) {
	var uuid [16]byte
	value = strings.ReplaceAll(value, "-", "")
	if len(value) != 32 {
		return uuid, errors.New("invalid Mojang profile UUID")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return uuid, errors.New("invalid Mojang profile UUID")
	}
	copy(uuid[:], decoded)
	return uuid, nil
}
