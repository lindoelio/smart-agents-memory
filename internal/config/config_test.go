package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

func TestNotionIsRejected(t *testing.T) {
	_, _, err := Resolve(Options{Vault: "notion"})
	if err == nil || !strings.Contains(err.Error(), "0.2.0") || !strings.Contains(err.Error(), "notion") {
		t.Fatalf("the error is %v", err)
	}
}

func TestLLMConfigurationUsesTheEnvironmentNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLLMProvider, "openai")
	t.Setenv(EnvLLMBaseURL, "http://127.0.0.1:9999/v1")
	t.Setenv(EnvLLMModel, "local-model")
	t.Setenv(EnvLLMKeyEnv, "MY_LOCAL_KEY")
	cfg, _, err := Resolve(Options{Vault: "disk", ConfigPath: filepath.Join(home, "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMProvider != "openai" || cfg.LLMBaseURL != "http://127.0.0.1:9999/v1" ||
		cfg.LLMModel != "local-model" || cfg.LLMKeyEnv != "MY_LOCAL_KEY" {
		t.Fatalf("the llm configuration is %+v", cfg)
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

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	file := File{
		Vault: "disk", MemoryHome: "/tmp/memory", SyncRoots: map[string][]string{"cursor": {"/tmp/cursor"}},
		SyncDisabled: []string{"codex"}, SyncMaxBytes: 1024, SyncInterval: "10m", WatchDirs: []string{"/tmp/watch"},
	}
	if err := Save(path, file); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Vault != "disk" || loaded.SyncRoots["cursor"][0] != "/tmp/cursor" ||
		loaded.SyncDisabled[0] != "codex" || loaded.SyncMaxBytes != 1024 || loaded.SyncInterval != "10m" {
		t.Fatalf("the loaded file is %+v", loaded)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
}

func TestResolveCarriesSyncConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "config.json")
	if err := Save(path, File{
		Vault: "disk", MemoryHome: "/tmp/memory",
		SyncRoots:    map[string][]string{"cursor": {"/tmp/cursor"}},
		SyncDisabled: []string{"codex"}, SyncMaxBytes: 2048, SyncInterval: "15m",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Resolve(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncRoots["cursor"][0] != "/tmp/cursor" || cfg.SyncDisabled[0] != "codex" ||
		cfg.SyncMaxBytes != 2048 || cfg.SyncInterval != "15m" {
		t.Fatalf("the sync configuration is %+v", cfg)
	}
}

func TestExpandAndLocalRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := Expand("~/memory"); got != filepath.Join(home, "memory") {
		t.Fatalf("Expand returned %q", got)
	}
	if got := Expand("~"); got != home {
		t.Fatalf("Expand(~) returned %q", got)
	}
	if got := Expand("/absolute"); got != "/absolute" {
		t.Fatalf("Expand kept %q", got)
	}
	if got := Expand(""); got != "" {
		t.Fatalf("Expand empty returned %q", got)
	}
	if got := LocalRoot(vaultConfig("disk", "/tmp/memory", "/tmp/projection")); got != "/tmp/memory" {
		t.Fatalf("LocalRoot(disk) returned %q", got)
	}
	if got := LocalRoot(vaultConfig("sqlite", "", "/tmp/projection")); got != "/tmp/projection" {
		t.Fatalf("LocalRoot(sqlite) returned %q", got)
	}
}

func TestPathHelpers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := DefaultPath(); !strings.HasSuffix(got, filepath.Join(".config", AppName, "config.json")) {
		t.Fatalf("DefaultPath returned %q", got)
	}
	if got := ShareDir(); !strings.HasSuffix(got, filepath.Join(".local", "share", AppName)) {
		t.Fatalf("ShareDir returned %q", got)
	}
	if got := DefaultProjectionRoot(); got != ShareDir() {
		t.Fatalf("DefaultProjectionRoot returned %q", got)
	}
}

func TestDiscoverMemoryHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := filepath.Join(home, "Library", "Mobile Documents", "iCloud~md~obsidian", "Documents", "Store", AppName)
	if err := os.MkdirAll(filepath.Join(store, "corpus"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverMemoryHome()
	if err != nil || got != store {
		t.Fatalf("DiscoverMemoryHome returned %q %v", got, err)
	}
	if err := os.RemoveAll(filepath.Join(home, "Library")); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverMemoryHome(); err == nil {
		t.Fatal("a missing store was discovered")
	}
}

func vaultConfig(backend, home, projection string) vault.Config {
	return vault.Config{Backend: backend, MemoryHome: home, ProjectionRoot: projection}
}

func TestSaveFailsUnderAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(blocker, "config.json"), File{}); err == nil {
		t.Fatal("Save under a file succeeded")
	}
}

func TestResolveRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(Options{ConfigPath: path}); err == nil {
		t.Fatal("Resolve accepted malformed JSON")
	}
}

func TestResolveExplicitPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, _, err := Resolve(Options{
		Vault: "sqlite", Database: "~/db.sqlite", MemoryHome: "~/memory",
		ConfigPath: filepath.Join(home, "missing.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SQLitePath != filepath.Join(home, "db.sqlite") || cfg.MemoryHome != filepath.Join(home, "memory") {
		t.Fatalf("the config is %+v", cfg)
	}
	if got := LocalRoot(cfg); got != DefaultProjectionRoot() {
		t.Fatalf("LocalRoot(sqlite) returned %q", got)
	}
}

func TestResolveDiscoveryFailureFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvHome, "")
	t.Setenv(EnvVault, "")
	cfg, _, err := Resolve(Options{Vault: "obsidian", ConfigPath: filepath.Join(home, "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MemoryHome != filepath.Join(home, "Smart Agents Memory") {
		t.Fatalf("the fallback is %q", cfg.MemoryHome)
	}
}

func TestLLMToggleDefaultsOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLLMEnabled, "")
	cfg, _, err := Resolve(Options{Vault: "disk", ConfigPath: filepath.Join(home, "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMEnabled {
		t.Fatal("the llm toggle defaulted on")
	}
}

func TestLLMToggleComesFromTheFileAndTheEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLLMEnabled, "")
	path := filepath.Join(home, "config.json")
	if err := Save(path, File{Vault: "disk", LLMEnabled: true}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Resolve(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LLMEnabled {
		t.Fatal("the file toggle was ignored")
	}
	t.Setenv(EnvLLMEnabled, "false")
	cfg, _, err = Resolve(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMEnabled {
		t.Fatal("the environment did not win over the file")
	}
}
