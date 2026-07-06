package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMustLoadLoadsDotEnvFromCurrentDir(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVICE_PLUGIN_KEY",
		"PLUGIN_SERVICE_DEV_LOGIN_ENABLED",
		"PLUGIN_SERVICE_IMAGE_PROVIDER_BASE_URL",
	)

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVICE_PLUGIN_KEY=from-dotenv\nPLUGIN_SERVICE_DEV_LOGIN_ENABLED=true\nPLUGIN_SERVICE_IMAGE_PROVIDER_BASE_URL=https://provider.example.com\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.PluginKey != "from-dotenv" {
		t.Fatalf("plugin key = %q, want %q", cfg.PluginKey, "from-dotenv")
	}
	if !cfg.DevLoginEnabled {
		t.Fatal("expected dev login enabled from .env")
	}
	if cfg.ImageProviderBaseURL != "https://provider.example.com" {
		t.Fatalf("image provider base url = %q, want %q", cfg.ImageProviderBaseURL, "https://provider.example.com")
	}
}

func TestMustLoadLoadsDotEnvFromRepoRootPluginServiceDir(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVICE_PLUGIN_KEY",
	)

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, "plugin-service", ".env"), "PLUGIN_SERVICE_PLUGIN_KEY=root-dotenv\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.PluginKey != "root-dotenv" {
		t.Fatalf("plugin key = %q, want %q", cfg.PluginKey, "root-dotenv")
	}
}

func TestMustLoadPrefersProcessEnvOverDotEnv(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVICE_PLUGIN_KEY",
		"PLUGIN_SERVICE_DEV_LOGIN_ENABLED",
	)

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVICE_PLUGIN_KEY=from-dotenv\nPLUGIN_SERVICE_DEV_LOGIN_ENABLED=true\n")

	if err := os.Setenv("PLUGIN_SERVICE_PLUGIN_KEY", "from-env"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PLUGIN_SERVICE_DEV_LOGIN_ENABLED", "false"); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.PluginKey != "from-env" {
		t.Fatalf("plugin key = %q, want %q", cfg.PluginKey, "from-env")
	}
	if cfg.DevLoginEnabled {
		t.Fatal("expected process env to override .env and keep dev login disabled")
	}
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		value, exists := os.LookupEnv(key)
		if exists {
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, value)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
