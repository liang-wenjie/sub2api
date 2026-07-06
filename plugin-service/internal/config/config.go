package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr         string
	BaseURL            string
	LaunchSharedSecret string
	MainSiteOrigin     string
	SessionTTL         time.Duration
	HistoryEnabled     bool
	PluginKey          string
	DevLoginEnabled    bool
}

func MustLoad() Config {
	loadDotEnvIfPresent()
	ttlSeconds := envInt("PLUGIN_SERVICE_SESSION_TTL_SECONDS", 3600)
	return Config{
		ListenAddr:         envString("PLUGIN_SERVICE_LISTEN_ADDR", ":8091"),
		BaseURL:            strings.TrimRight(envString("PLUGIN_SERVICE_BASE_URL", "http://localhost:8091"), "/"),
		LaunchSharedSecret: envString("PLUGIN_SERVICE_LAUNCH_SHARED_SECRET", "dev-only-change-me"),
		MainSiteOrigin:     strings.TrimRight(envString("PLUGIN_SERVICE_MAIN_SITE_ORIGIN", "http://localhost:8088"), "/"),
		SessionTTL:         time.Duration(ttlSeconds) * time.Second,
		HistoryEnabled:     envBool("PLUGIN_SERVICE_HISTORY_ENABLED", true),
		PluginKey:          envString("PLUGIN_SERVICE_PLUGIN_KEY", "gen"),
		DevLoginEnabled:    envBool("PLUGIN_SERVICE_DEV_LOGIN_ENABLED", false),
	}
}

func loadDotEnvIfPresent() {
	for _, candidate := range []string{
		".env",
		filepath.Join("plugin-service", ".env"),
	} {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		applyDotEnvFile(candidate)
		return
	}
}

func applyDotEnvFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		_ = os.Setenv(key, value)
	}
}

func envString(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
