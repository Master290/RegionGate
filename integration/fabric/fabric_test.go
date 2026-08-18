//go:build integration

package fabric_test

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

func TestFabricProxyLiteVelocityForwarding(t *testing.T) {
	if os.Getenv("REGIONGATE_RUN_FABRIC_INTEGRATION") != "1" {
		t.Skip("set REGIONGATE_RUN_FABRIC_INTEGRATION=1 to run the FabricProxy-Lite integration test")
	}
	secret := "regiongate-fabric-integration-secret"
	server := minecrafttest.Start(t, minecrafttest.Options{
		Type:    "FABRIC",
		Version: "1.20.4",
		ExtraEnvironment: map[string]string{
			"MODRINTH_PROJECTS":   "fabricproxy-lite:2.7.0",
			"FABRIC_PROXY_SECRET": secret,
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	t.Run("valid secret reaches configuration", func(t *testing.T) {
		identity := forwarding.PlayerIdentity{Address: "203.0.113.20", UUID: login.OfflineUUID("RGFabricValid"), Username: "RGFabricValid"}
		connection := dialFabric(t, ctx, server.Address(), identity)
		defer connection.Close()
		forwarder, _ := forwarding.NewModernForwarding([]byte(secret))
		if _, err := backend.CompleteLogin(ctx, connection, forwarder, identity, backend.LoginConfig{Timeout: 30 * time.Second}); err != nil {
			t.Fatalf("FabricProxy-Lite login failed: %v\n%s", err, server.Logs())
		}
		configuration, err := backend.CompleteConfiguration(ctx, connection, backend.ConfigurationConfig{Timeout: 30 * time.Second})
		if err != nil {
			t.Fatalf("Fabric configuration failed: %v\n%s", err, server.Logs())
		}
		if len(configuration.Packets) == 0 {
			t.Fatal("Fabric returned no configuration packets")
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		identity := forwarding.PlayerIdentity{Address: "203.0.113.21", UUID: login.OfflineUUID("RGFabricBad"), Username: "RGFabricBad"}
		connection := dialFabric(t, ctx, server.Address(), identity)
		defer connection.Close()
		forwarder, _ := forwarding.NewModernForwarding([]byte("wrong-secret"))
		if _, err := backend.CompleteLogin(ctx, connection, forwarder, identity, backend.LoginConfig{Timeout: 30 * time.Second}); err == nil {
			t.Fatal("FabricProxy-Lite accepted an invalid Velocity forwarding secret")
		}
	})
}

func dialFabric(t *testing.T, ctx context.Context, address string, identity forwarding.PlayerIdentity) *transport.Transport {
	t.Helper()
	dialer := backend.NewDialer(backend.Config{Address: address, Host: "localhost", Port: 25565, ConnectTimeout: 10 * time.Second})
	connection, err := dialer.Dial(ctx, identity.Username, identity.UUID)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}
