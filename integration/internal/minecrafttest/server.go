//go:build integration

package minecrafttest

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

type Options struct {
	Type             string
	Version          string
	ExtraEnvironment map[string]string
}

type Server struct {
	name   string
	ctx    context.Context
	cancel context.CancelFunc
	addr   string
}

func Start(t *testing.T, options Options) *Server {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	server := &Server{
		name:   fmt.Sprintf("regiongate-%s-%d-%d", strings.ToLower(options.Type), os.Getpid(), time.Now().UnixNano()),
		ctx:    ctx,
		cancel: cancel,
	}
	args := []string{
		"run", "--detach", "--name", server.name,
		"--publish", "127.0.0.1::25565",
		"--env", "EULA=TRUE",
		"--env", "TYPE=" + options.Type,
		"--env", "VERSION=" + options.Version,
		"--env", "ONLINE_MODE=FALSE",
		"--env", "ENFORCE_SECURE_PROFILE=FALSE",
		"--env", "ENABLE_RCON=FALSE",
		"--env", "MEMORY=1G",
	}
	if jar := os.Getenv("REGIONGATE_PAPER_JAR"); jar != "" && strings.EqualFold(options.Type, "PAPER") {
		args = append(args,
			"--volume", jar+":/data/paper.jar:ro",
			"--env", "TYPE=CUSTOM",
			"--env", "CUSTOM_SERVER=/data/paper.jar",
		)
	}
	for key, value := range options.ExtraEnvironment {
		args = append(args, "--env", key+"="+value)
	}
	args = append(args, "itzg/minecraft-server:java21")
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		cancel()
		t.Fatalf("docker run: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "--force", server.name).Run()
		cancel()
	})
	server.addr = server.port(t)
	server.waitReady(t)
	return server
}

func (s *Server) Address() string { return s.addr }

func (s *Server) Logs() string {
	output, err := exec.CommandContext(s.ctx, "docker", "logs", s.name).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("docker logs failed: %v", err)
	}
	return string(output)
}

func (s *Server) port(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.CommandContext(s.ctx, "docker", "port", s.name, "25565/tcp").Output()
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
	t.Fatalf("Minecraft server port was not published\n%s", s.Logs())
	return ""
}

func (s *Server) waitReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		logs := s.Logs()
		if strings.Contains(logs, "Done (") {
			connection, err := net.DialTimeout("tcp", s.addr, time.Second)
			if err == nil {
				_ = connection.Close()
				return
			}
		}
		select {
		case <-s.ctx.Done():
			t.Fatalf("Minecraft server startup context expired: %v\n%s", s.ctx.Err(), logs)
		case <-time.After(time.Second):
		}
	}
	t.Fatalf("Minecraft server did not become ready\n%s", s.Logs())
}
