package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr string
}

func MustLoad() Config {
	loadDotEnvIfPresent()
	port := envInt("PLUGIN_SERVER_PORT", 8091)
	return Config{
		ListenAddr: ":" + strconv.Itoa(port),
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
