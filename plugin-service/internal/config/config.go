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
	ListenAddr      string
	SessionTTL      time.Duration
	HistoryEnabled  bool
	DevLoginEnabled bool
}

func MustLoad() Config {
	loadDotEnvIfPresent()
	ttlSeconds := envInt("PLUGIN_SERVER_SESSION_TTL_SECONDS", 3600)
	port := envInt("PLUGIN_SERVER_PORT", 8091)
	return Config{
		ListenAddr:      ":" + strconv.Itoa(port),
		SessionTTL:      time.Duration(ttlSeconds) * time.Second,
		HistoryEnabled:  envBool("PLUGIN_SERVER_HISTORY_ENABLED", true),
		DevLoginEnabled: envBool("PLUGIN_SERVER_DEV_LOGIN_ENABLED", false),
	}
}

func loadDotEnvIfPresent() {
	for _, candidate := range []string{
		".env",
		filepath.Join("..", ".env"),
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
