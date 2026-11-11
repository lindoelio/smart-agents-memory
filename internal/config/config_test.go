package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNotionIsRejected(t *testing.T) {
	_, _, err := Resolve(Options{Vault: "notion"})
	if err == nil || !strings.Contains(err.Error(), "0.2.0") || !strings.Contains(err.Error(), "notion") {
		t.Fatalf("the error is %v", err)
	}
}

func TestAMissingFileStoreUsesTheDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SMART_AGENTS_MEMORY_HOME", "")
	t.Setenv("SMART_AGENTS_MEMORY_VAULT", "")
	cfg, _, err := Resolve(Options{Vault: "disk", ConfigPath: filepath.Join(home, "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Smart Agents Memory")
	if cfg.MemoryHome != want {
		t.Fatalf("the memory home is %q, want %q", cfg.MemoryHome, want)
	}
}
