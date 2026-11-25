// Package config resolves where the memory store lives.
//
// Resolution is explicit first, then the configuration file, then discovery:
// a flag, an environment variable, the config file, and finally the vault
// found in iCloud.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// Names used across the configuration.
const (
	// AppName is the name of the product, and the name of its folders.
	AppName = "smart-agents-memory"
	// EnvHome overrides the memory home.
	EnvHome = "SMART_AGENTS_MEMORY_HOME"
	// EnvVault overrides the backend.
	EnvVault = "SMART_AGENTS_MEMORY_VAULT"
	// EnvSource overrides the source recorded on new entries.
	EnvSource = "SMART_AGENTS_MEMORY_SOURCE"
	// EnvDatabase overrides the SQLite database path.
	EnvDatabase = "SMART_AGENTS_MEMORY_DATABASE"
	// EnvLLMProvider overrides the LLM adapter.
	EnvLLMProvider = "SMART_AGENTS_MEMORY_LLM_PROVIDER"
	// EnvLLMEnabled turns the optional LLM client on or off.
	EnvLLMEnabled = "SMART_AGENTS_MEMORY_LLM_ENABLED"
	// EnvLLMBaseURL overrides the LLM base URL.
	EnvLLMBaseURL = "SMART_AGENTS_MEMORY_LLM_URL"
	// EnvLLMModel overrides the LLM model.
	EnvLLMModel = "SMART_AGENTS_MEMORY_LLM_MODEL"
	// EnvLLMKeyEnv names the variable that holds the LLM key.
	EnvLLMKeyEnv = "SMART_AGENTS_MEMORY_LLM_KEY_ENV"
	// DefaultLLMKeyEnv is the default variable holding the LLM key.
	DefaultLLMKeyEnv = "SMART_AGENTS_MEMORY_LLM_API_KEY"
)

// DefaultPath is where the configuration file lives.
func DefaultPath() string {
	return filepath.Join(userHome(), ".config", AppName, "config.json")
}

// ShareDir is where the product keeps what it owns on the machine: the
// materialized skill, and the local projections of the gateways that are
// not a local directory.
func ShareDir() string {
	return filepath.Join(userHome(), ".local", "share", AppName)
}

// DefaultProjectionRoot is where the local projections live by default.
func DefaultProjectionRoot() string {
	return ShareDir()
}

// File is the configuration file of the product.
type File struct {
	// Vault is the backend: obsidian (default), disk, sqlite.
	Vault string `json:"vault,omitempty"`
	// MemoryHome is the root of the store, for the file gateways.
	MemoryHome string `json:"memory_home,omitempty"`
	// SQLitePath is the database of the SQLite gateway.
	SQLitePath string `json:"sqlite_path,omitempty"`
	// ProjectionRoot is where the native projections are materialized,
	// for the gateways that are not a local directory.
	ProjectionRoot string `json:"projection_root,omitempty"`
	// LLMProvider is the JSON client adapter (default openai).
	LLMProvider string `json:"llm_provider,omitempty"`
	// LLMEnabled turns the optional client on. Off by default, so a
	// command never reaches the network unless the deployment asked it
	// to; the adapter applies the local gateway URL when none is set.
	LLMEnabled bool `json:"llm_enabled"`
	// LLMBaseURL is the OpenAI-compatible base URL.
	LLMBaseURL string `json:"llm_base_url,omitempty"`
	// LLMModel is the model name.
	LLMModel string `json:"llm_model,omitempty"`
	// LLMKeyEnv is the environment variable holding the key, never the key.
	LLMKeyEnv string `json:"llm_key_env,omitempty"`
	// WatchDirs are extra transcript directories for `watch`.
	WatchDirs []string `json:"watch_dirs,omitempty"`
	// SyncRoots overrides the roots of a built-in sync source, by source ID.
	SyncRoots map[string][]string `json:"sync_roots,omitempty"`
	// SyncDisabled lists sync source IDs that must not run.
	SyncDisabled []string `json:"sync_disabled,omitempty"`
	// SyncMaxBytes caps the bytes read from one file in one sync run.
	SyncMaxBytes int64 `json:"sync_max_bytes,omitempty"`
	// SyncInterval is the scheduler interval, as a Go duration string.
	SyncInterval string `json:"sync_interval,omitempty"`
}

// Load reads the configuration file. A missing file is not an error.
func Load(path string) (File, error) {
	cfg := File{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the configuration file.
func Save(path string, cfg File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Options are the values that come from the command line.
type Options struct {
	// ConfigPath is the configuration file to use.
	ConfigPath string
	// Vault overrides the backend.
	Vault string
	// MemoryHome overrides the root of the store.
	MemoryHome string
	// Database overrides the SQLite database.
	Database string
}

// Resolve produces the configuration of the run and the path of the file it
// came from.
func Resolve(opts Options) (vault.Config, string, error) {
	path := opts.ConfigPath
	if path == "" {
		path = DefaultPath()
	}
	file, err := Load(path)
	if err != nil {
		return vault.Config{}, path, err
	}

	backend := firstNonEmpty(opts.Vault, os.Getenv(EnvVault), file.Vault, vault.DefaultBackend)
	if backend == "notion" {
		return vault.Config{}, path, fmt.Errorf("the notion gateway was removed in 0.2.0 because it was not verified against the Notion API")
	}

	llmEnabled := file.LLMEnabled
	if raw := strings.TrimSpace(os.Getenv(EnvLLMEnabled)); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			llmEnabled = value
		}
	}

	cfg := vault.Config{
		Backend:        backend,
		MemoryHome:     file.MemoryHome,
		SQLitePath:     file.SQLitePath,
		ProjectionRoot: file.ProjectionRoot,
		LLMProvider:    firstNonEmpty(os.Getenv(EnvLLMProvider), file.LLMProvider, "openai"),
		LLMEnabled:     llmEnabled,
		LLMBaseURL:     firstNonEmpty(os.Getenv(EnvLLMBaseURL), file.LLMBaseURL),
		LLMModel:       firstNonEmpty(os.Getenv(EnvLLMModel), file.LLMModel),
		LLMKeyEnv:      firstNonEmpty(os.Getenv(EnvLLMKeyEnv), file.LLMKeyEnv, DefaultLLMKeyEnv),
		WatchDirs:      file.WatchDirs,
		SyncRoots:      file.SyncRoots,
		SyncDisabled:   file.SyncDisabled,
		SyncMaxBytes:   file.SyncMaxBytes,
		SyncInterval:   file.SyncInterval,
	}

	if home := firstNonEmpty(opts.MemoryHome, os.Getenv(EnvHome)); home != "" {
		cfg.MemoryHome = Expand(home)
	} else if file.MemoryHome != "" {
		cfg.MemoryHome = Expand(file.MemoryHome)
	}

	if db := firstNonEmpty(opts.Database, os.Getenv(EnvDatabase), file.SQLitePath); db != "" {
		cfg.SQLitePath = Expand(db)
	} else {
		cfg.SQLitePath = filepath.Join(DefaultProjectionRoot(), "memory.db")
	}

	if cfg.ProjectionRoot == "" {
		cfg.ProjectionRoot = DefaultProjectionRoot()
	} else {
		cfg.ProjectionRoot = Expand(cfg.ProjectionRoot)
	}

	if backend == "obsidian" || backend == "disk" {
		if cfg.MemoryHome == "" {
			// A store that was not found is not an error: the install
			// command creates one, and the other commands report it.
			if home, err := DiscoverMemoryHome(); err == nil {
				cfg.MemoryHome = home
			} else {
				cfg.MemoryHome = DefaultStorePath()
			}
		}
	}

	return cfg, path, nil
}

// DefaultStorePath is where a store is created when there is none.
func DefaultStorePath() string {
	return filepath.Join(userHome(), "Smart Agents Memory")
}

// DiscoverMemoryHome finds the store in the iCloud vaults of the machine.
func DiscoverMemoryHome() (string, error) {
	home := userHome()
	patterns := []string{
		filepath.Join(home, "Library", "Mobile Documents", "iCloud~md~obsidian", "Documents", "*", AppName),
		filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs", "*", AppName),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if info, err := os.Stat(filepath.Join(m, "corpus")); err == nil && info.IsDir() {
				return m, nil
			}
		}
	}
	return "", errors.New("no memory store found: pass --memory-home, set " + EnvHome +
		", or add \"memory_home\" to " + DefaultPath())
}

// Expand expands a leading ~ in a path.
func Expand(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		return userHome()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(userHome(), path[2:])
	}
	return path
}

// LocalRoot returns the directory that holds the hot tier and the native
// projections.
//
// The file gateways keep them inside the store, which is what the iCloud
// sync carries. The other gateways materialize them locally.
func LocalRoot(cfg vault.Config) string {
	switch cfg.Backend {
	case "obsidian", "disk":
		if cfg.MemoryHome != "" {
			return cfg.MemoryHome
		}
		return DefaultStorePath()
	}
	return cfg.ProjectionRoot
}

// firstNonEmpty returns the first value that is not empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// userHome returns the home directory of the user.
func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.Getenv("HOME")
	}
	return home
}
