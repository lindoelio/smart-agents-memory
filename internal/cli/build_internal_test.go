package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/lindoelio/smart-agents-memory/internal/config"
)

func TestBuildEnablesTheLLMFromTheToggle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.DefaultLLMKeyEnv, "")
	store := filepath.Join(home, "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.json")
	body := `{"vault":"obsidian","memory_home":"` + store + `","llm_enabled":true}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := build(config.Options{ConfigPath: path, MemoryHome: store, Vault: "obsidian"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if a.LLM == nil || a.LLM.Kind() != "openai" {
		t.Fatalf("the toggle did not enable the openai client: %+v", a.LLM)
	}
}

func TestBuildLeavesTheLLMOFFByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// An ambient key exported for another tool must not enable the client.
	t.Setenv(config.DefaultLLMKeyEnv, "sk-another-tool")
	store := filepath.Join(home, "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.json")
	body := `{"vault":"obsidian","memory_home":"` + store + `"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := build(config.Options{ConfigPath: path, MemoryHome: store, Vault: "obsidian"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if a.LLM != nil {
		t.Fatalf("the llm was enabled without a toggle: %q", a.LLM.Kind())
	}
}
