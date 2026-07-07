package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMustLoadLoadsDotEnvFromCurrentDir(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVER_HISTORY_ENABLED",
		"PLUGIN_SERVER_DEV_LOGIN_ENABLED",
	)

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVER_HISTORY_ENABLED=false\nPLUGIN_SERVER_DEV_LOGIN_ENABLED=true\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.HistoryEnabled {
		t.Fatal("expected history disabled from .env")
	}
	if !cfg.DevLoginEnabled {
		t.Fatal("expected dev login enabled from .env")
	}
}

func TestMustLoadLoadsSharedDotEnvFromParentDir(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVER_HISTORY_ENABLED",
		"PLUGIN_SERVER_DEV_LOGIN_ENABLED",
	)

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVER_HISTORY_ENABLED=false\nPLUGIN_SERVER_DEV_LOGIN_ENABLED=true\n")
	if err := os.MkdirAll(filepath.Join(tempDir, "plugin-service"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(filepath.Join(tempDir, "plugin-service")); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.HistoryEnabled {
		t.Fatal("expected history disabled from shared parent .env")
	}
	if !cfg.DevLoginEnabled {
		t.Fatal("expected dev login enabled from shared parent .env")
	}
}

func TestMustLoadPrefersProcessEnvOverDotEnv(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVER_HISTORY_ENABLED",
		"PLUGIN_SERVER_DEV_LOGIN_ENABLED",
	)

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVER_HISTORY_ENABLED=false\nPLUGIN_SERVER_DEV_LOGIN_ENABLED=true\n")

	if err := os.Setenv("PLUGIN_SERVER_HISTORY_ENABLED", "true"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PLUGIN_SERVER_DEV_LOGIN_ENABLED", "false"); err != nil {
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
	if !cfg.HistoryEnabled {
		t.Fatal("expected process env to override .env and keep history enabled")
	}
	if cfg.DevLoginEnabled {
		t.Fatal("expected process env to override .env and keep dev login disabled")
	}
}

func TestMustLoadUsesPluginServerPort(t *testing.T) {
	unsetEnv(t, "PLUGIN_SERVER_PORT")

	if err := os.Setenv("PLUGIN_SERVER_PORT", "19091"); err != nil {
		t.Fatal(err)
	}
	cfg := MustLoad()
	if cfg.ListenAddr != ":19091" {
		t.Fatalf("listen addr = %q, want %q", cfg.ListenAddr, ":19091")
	}
}

func TestMustLoadIgnoresLegacyPluginServiceEnvNames(t *testing.T) {
	unsetEnv(t,
		"PLUGIN_SERVER_PORT",
		"PLUGIN_SERVER_DEV_LOGIN_ENABLED",
		"PLUGIN_SERVICE_LISTEN_ADDR",
		"PLUGIN_SERVICE_DEV_LOGIN_ENABLED",
	)

	if err := os.Setenv("PLUGIN_SERVICE_LISTEN_ADDR", ":19091"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PLUGIN_SERVICE_DEV_LOGIN_ENABLED", "true"); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.ListenAddr != ":8091" {
		t.Fatalf("listen addr = %q, want default %q", cfg.ListenAddr, ":8091")
	}
	if cfg.DevLoginEnabled {
		t.Fatal("expected legacy PLUGIN_SERVICE_* env to be ignored")
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
