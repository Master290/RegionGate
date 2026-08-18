package app

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddress     string
	HealthAddress     string
	BackendAddress    string
	BackendHost       string
	BackendPort       uint16
	VelocitySecret    string
	MaxConnections    int
	MaxPacketSize     int
	KeepAliveInterval time.Duration
	KeepAliveTimeout  time.Duration
}

func LoadConfig() (Config, error) { return loadConfig(os.Getenv) }

func loadConfig(getenv func(string) string) (Config, error) {
	config := Config{
		ListenAddress:     valueOr(getenv("REGIONGATE_LISTEN"), ":25565"),
		HealthAddress:     valueOr(getenv("REGIONGATE_HEALTH_LISTEN"), "127.0.0.1:8080"),
		BackendAddress:    getenv("REGIONGATE_BACKEND_ADDRESS"),
		BackendHost:       valueOr(getenv("REGIONGATE_BACKEND_HOST"), "localhost"),
		BackendPort:       25565,
		VelocitySecret:    getenv("REGIONGATE_VELOCITY_SECRET"),
		MaxConnections:    10000,
		KeepAliveInterval: 15 * time.Second,
		KeepAliveTimeout:  30 * time.Second,
	}
	var err error
	if config.BackendPort, err = uint16Value(getenv("REGIONGATE_BACKEND_PORT"), config.BackendPort); err != nil {
		return Config{}, err
	}
	if config.MaxConnections, err = intValue(getenv("REGIONGATE_MAX_CONNECTIONS"), config.MaxConnections); err != nil || config.MaxConnections <= 0 {
		return Config{}, errors.New("REGIONGATE_MAX_CONNECTIONS must be positive")
	}
	if (config.BackendAddress == "") != (config.VelocitySecret == "") {
		return Config{}, errors.New("backend address and Velocity secret must be configured together")
	}
	return config, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func intValue(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func uint16Value(value string, fallback uint16) (uint16, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 16)
	return uint16(parsed), err
}
