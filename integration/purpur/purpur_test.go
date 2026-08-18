//go:build integration

package purpur_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Master290/RegionGate/integration/internal/minecrafttest"
	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/transport"
)

func TestPurpurVelocityForwarding(t *testing.T) {
	if os.Getenv("REGIONGATE_RUN_PURPUR_INTEGRATION") != "1" {
		t.Skip("set REGIONGATE_RUN_PURPUR_INTEGRATION=1 to run the Purpur integration test")
	}
	secret := "regiongate-purpur-integration-secret"
	server := minecrafttest.Start(t, minecrafttest.Options{
		Type:    "PURPUR",
		Version: "1.20.4",
		ExtraEnvironment: map[string]string{
			"VELOCITY_SECRET": secret,
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	t.Run("valid secret reaches configuration", func(t *testing.T) {
		identity := forwarding.PlayerIdentity{Address: "203.0.113.30", UUID: login.OfflineUUID("RGPurpurValid"), Username: "RGPurpurValid"}
		connection := dialPurpur(t, ctx, server.Address(), identity)
		defer connection.Close()
		forwarder, _ := forwarding.NewModernForwarding([]byte(secret))
		if _, err := backend.CompleteLogin(ctx, connection, forwarder, identity, backend.LoginConfig{Timeout: 30 * time.Second}); err != nil {
			t.Fatalf("Purpur login failed: %v\n%s", err, server.Logs())
		}
		configuration, err := backend.CompleteConfiguration(ctx, connection, backend.ConfigurationConfig{Timeout: 30 * time.Second})
		if err != nil {
			t.Fatalf("Purpur configuration failed: %v\n%s", err, server.Logs())
		}
		if len(configuration.Packets) == 0 {
			t.Fatal("Purpur returned no configuration packets")
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		identity := forwarding.PlayerIdentity{Address: "203.0.113.31", UUID: login.OfflineUUID("RGPurpurBad"), Username: "RGPurpurBad"}
		connection := dialPurpur(t, ctx, server.Address(), identity)
		defer connection.Close()
		forwarder, _ := forwarding.NewModernForwarding([]byte("wrong-secret"))
		if _, err := backend.CompleteLogin(ctx, connection, forwarder, identity, backend.LoginConfig{Timeout: 30 * time.Second}); err == nil {
			t.Fatal("Purpur accepted an invalid Velocity forwarding secret")
		}
	})
}

func dialPurpur(t *testing.T, ctx context.Context, address string, identity forwarding.PlayerIdentity) *transport.Transport {
	t.Helper()
	dialer := backend.NewDialer(backend.Config{Address: address, Host: "localhost", Port: 25565, ConnectTimeout: 10 * time.Second})
	connection, err := dialer.Dial(ctx, identity.Username, identity.UUID)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}
