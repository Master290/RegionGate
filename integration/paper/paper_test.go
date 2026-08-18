//go:build integration

package paper_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Master290/RegionGate/internal/backend"
	"github.com/Master290/RegionGate/internal/forwarding"
	"github.com/Master290/RegionGate/internal/protocol/login"
	"github.com/Master290/RegionGate/internal/transport"
)

const paperImage = "itzg/minecraft-server:java21"

func TestPaperVelocityForwarding(t *testing.T) {
	if os.Getenv("REGIONGATE_RUN_PAPER_INTEGRATION") != "1" {
		t.Skip("set REGIONGATE_RUN_PAPER_INTEGRATION=1 to run the Paper integration test")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	secret := "regiongate-paper-integration-secret"
	container := fmt.Sprintf("regiongate-paper-%d-%d", os.Getpid(), time.Now().UnixNano())
	runDocker(t, ctx, "run", "--rm", "--detach", "--name", container,
		"--publish", "127.0.0.1::25565",
		"--env", "EULA=TRUE",
		"--env", "TYPE=PAPER",
		"--env", "VERSION=1.20.4",
		"--env", "ONLINE_MODE=FALSE",
		"--env", "ENFORCE_SECURE_PROFILE=FALSE",
		"--env", "ENABLE_RCON=FALSE",
		"--env", "MEMORY=1G",
		"--env", "VELOCITY_SECRET="+secret,
		paperImage,
	)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "--force", container).Run()
	})

	address := paperAddress(t, ctx, container)
	waitForPaper(t, ctx, container, address)

	t.Run("valid secret reaches configuration", func(t *testing.T) {
		identity := forwarding.PlayerIdentity{
			Address:  "203.0.113.10",
			UUID:     login.OfflineUUID("RegionGateValid"),
			Username: "RegionGateValid",
		}
		connection := dialPaper(t, ctx, address, identity)
		defer connection.Close()
		forwarder, err := forwarding.NewModernForwarding([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backend.CompleteLogin(ctx, connection, forwarder, identity, backend.LoginConfig{Timeout: 30 * time.Second}); err != nil {
			t.Fatalf("Paper login failed: %v\n%s", err, dockerLogs(container))
		}
		configuration, err := backend.CompleteConfiguration(ctx, connection, backend.ConfigurationConfig{Timeout: 30 * time.Second})
		if err != nil {
			t.Fatalf("Paper configuration failed: %v\n%s", err, dockerLogs(container))
		}
		if len(configuration.Packets) == 0 {
			t.Fatal("Paper returned no configuration packets")
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		identity := forwarding.PlayerIdentity{
			Address:  "203.0.113.11",
			UUID:     login.OfflineUUID("RegionGateInvalid"),
			Username: "RegionGateInvalid",
		}
		connection := dialPaper(t, ctx, address, identity)
		defer connection.Close()
		forwarder, err := forwarding.NewModernForwarding([]byte("wrong-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := backend.CompleteLogin(ctx, connection, forwarder, identity, backend.LoginConfig{Timeout: 30 * time.Second}); err == nil {
			t.Fatal("Paper accepted an invalid Velocity forwarding secret")
		}
	})
}

func dialPaper(t *testing.T, ctx context.Context, address string, identity forwarding.PlayerIdentity) *transport.Transport {
	t.Helper()
	dialer := backend.NewDialer(backend.Config{
		Address:        address,
		Host:           "localhost",
		Port:           25565,
		ConnectTimeout: 10 * time.Second,
	})
	connection, err := dialer.Dial(ctx, identity.Username, identity.UUID)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func paperAddress(t *testing.T, ctx context.Context, container string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.CommandContext(ctx, "docker", "port", container, "25565/tcp").Output()
		if err == nil {
			address := strings.TrimSpace(string(output))
			if _, port, splitErr := net.SplitHostPort(address); splitErr == nil {
				if _, parseErr := strconv.ParseUint(port, 10, 16); parseErr == nil {
					return address
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("Paper port was not published\n%s", dockerLogs(container))
	return ""
}

func waitForPaper(t *testing.T, ctx context.Context, container, address string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		logs := dockerLogs(container)
		if strings.Contains(logs, "Done (") {
			connection, err := net.DialTimeout("tcp", address, time.Second)
			if err == nil {
				_ = connection.Close()
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Paper startup context expired: %v\n%s", ctx.Err(), logs)
		case <-time.After(time.Second):
		}
	}
	t.Fatalf("Paper did not become ready\n%s", dockerLogs(container))
}

func runDocker(t *testing.T, ctx context.Context, arguments ...string) {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func dockerLogs(container string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "logs", container).CombinedOutput()
	if err != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("docker logs failed: %v", err)
	}
	return string(output)
}
