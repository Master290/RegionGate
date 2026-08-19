package app

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddress       string
	HealthAddress       string
	PprofAddress        string
	AdminToken          string
	BackendAddress      string
	BackendHost         string
	BackendPort         uint16
	VelocitySecret      string
	OnlineMode          bool
	SessionServerURL    string
	MaxConnections      int
	MaxConnectionsPerIP int
	MaxPacketSize       int
	KeepAliveInterval   time.Duration
	KeepAliveTimeout    time.Duration
	QueueSize           int
	AdmissionInterval   time.Duration
	LoginRateLimit      int
	LoginRateWindow     time.Duration
	BotFilterConfigFile string
}

func LoadConfig() (Config, error) { return loadConfig(os.Getenv) }

func loadConfig(getenv func(string) string) (Config, error) {
	config := Config{
		ListenAddress:       valueOr(getenv("REGIONGATE_LISTEN"), ":25565"),
		HealthAddress:       valueOr(getenv("REGIONGATE_HEALTH_LISTEN"), "127.0.0.1:8080"),
		PprofAddress:        getenv("REGIONGATE_PPROF_LISTEN"),
		AdminToken:          getenv("REGIONGATE_ADMIN_TOKEN"),
		BackendAddress:      getenv("REGIONGATE_BACKEND_ADDRESS"),
		BackendHost:         valueOr(getenv("REGIONGATE_BACKEND_HOST"), "localhost"),
		BackendPort:         25565,
		VelocitySecret:      getenv("REGIONGATE_VELOCITY_SECRET"),
		SessionServerURL:    getenv("REGIONGATE_SESSION_SERVER_URL"),
		MaxConnections:      10000,
		MaxConnectionsPerIP: 16,
		KeepAliveInterval:   15 * time.Second,
		KeepAliveTimeout:    30 * time.Second,
		QueueSize:           1024,
		AdmissionInterval:   time.Second,
		LoginRateLimit:      10,
		LoginRateWindow:     10 * time.Second,
		BotFilterConfigFile: getenv("REGIONGATE_CONFIG_FILE"),
	}
	var err error
	if config.AdminToken != "" && len(config.AdminToken) < 32 {
		return Config{}, errors.New("REGIONGATE_ADMIN_TOKEN must contain at least 32 bytes")
	}
	if value := getenv("REGIONGATE_ONLINE_MODE"); value != "" {
		config.OnlineMode, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, errors.New("REGIONGATE_ONLINE_MODE must be a boolean")
		}
	}
	if config.BackendPort, err = uint16Value(getenv("REGIONGATE_BACKEND_PORT"), config.BackendPort); err != nil {
		return Config{}, err
	}
	if config.MaxConnections, err = intValue(getenv("REGIONGATE_MAX_CONNECTIONS"), config.MaxConnections); err != nil || config.MaxConnections <= 0 {
		return Config{}, errors.New("REGIONGATE_MAX_CONNECTIONS must be positive")
	}
	if config.MaxConnectionsPerIP, err = intValue(getenv("REGIONGATE_MAX_CONNECTIONS_PER_IP"), config.MaxConnectionsPerIP); err != nil || config.MaxConnectionsPerIP <= 0 {
		return Config{}, errors.New("REGIONGATE_MAX_CONNECTIONS_PER_IP must be positive")
	}
	if config.QueueSize, err = intValue(getenv("REGIONGATE_QUEUE_SIZE"), config.QueueSize); err != nil || config.QueueSize <= 0 {
		return Config{}, errors.New("REGIONGATE_QUEUE_SIZE must be positive")
	}
	if value := getenv("REGIONGATE_ADMISSION_INTERVAL"); value != "" {
		config.AdmissionInterval, err = time.ParseDuration(value)
		if err != nil || config.AdmissionInterval <= 0 {
			return Config{}, errors.New("REGIONGATE_ADMISSION_INTERVAL must be a positive duration")
		}
	}
	if config.LoginRateLimit, err = intValue(getenv("REGIONGATE_LOGIN_RATE_LIMIT"), config.LoginRateLimit); err != nil || config.LoginRateLimit <= 0 {
		return Config{}, errors.New("REGIONGATE_LOGIN_RATE_LIMIT must be positive")
	}
	if value := getenv("REGIONGATE_LOGIN_RATE_WINDOW"); value != "" {
		config.LoginRateWindow, err = time.ParseDuration(value)
		if err != nil || config.LoginRateWindow <= 0 {
			return Config{}, errors.New("REGIONGATE_LOGIN_RATE_WINDOW must be a positive duration")
		}
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
