package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMustLoadLoadsDotEnvFromCurrentDir(t *testing.T) {
	unsetEnv(t, "PLUGIN_SERVER_PORT")

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVER_PORT=19091\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.ListenAddr != ":19091" {
		t.Fatalf("listen addr = %q, want %q", cfg.ListenAddr, ":19091")
	}
}

func TestMustLoadLoadsSharedDotEnvFromParentDir(t *testing.T) {
	unsetEnv(t, "PLUGIN_SERVER_PORT")

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVER_PORT=18091\n")
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
	if cfg.ListenAddr != ":18091" {
		t.Fatalf("listen addr = %q, want %q", cfg.ListenAddr, ":18091")
	}
}

func TestMustLoadPrefersProcessEnvOverDotEnv(t *testing.T) {
	unsetEnv(t, "PLUGIN_SERVER_PORT")

	tempDir := t.TempDir()
	writeFile(t, filepath.Join(tempDir, ".env"), "PLUGIN_SERVER_PORT=18091\n")
	if err := os.Setenv("PLUGIN_SERVER_PORT", "17091"); err != nil {
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
	if cfg.ListenAddr != ":17091" {
		t.Fatalf("listen addr = %q, want %q", cfg.ListenAddr, ":17091")
	}
}

func TestMustLoadUsesDefaultPortWhenUnset(t *testing.T) {
	unsetEnv(t, "PLUGIN_SERVER_PORT")

	cfg := MustLoad()
	if cfg.ListenAddr != ":8091" {
		t.Fatalf("listen addr = %q, want default %q", cfg.ListenAddr, ":8091")
	}
}

func TestMustLoadIgnoresLegacyPluginServiceEnvNames(t *testing.T) {
	unsetEnv(t, "PLUGIN_SERVER_PORT", "PLUGIN_SERVICE_LISTEN_ADDR")

	if err := os.Setenv("PLUGIN_SERVICE_LISTEN_ADDR", ":19091"); err != nil {
		t.Fatal(err)
	}

	cfg := MustLoad()
	if cfg.ListenAddr != ":8091" {
		t.Fatalf("listen addr = %q, want default %q", cfg.ListenAddr, ":8091")
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
