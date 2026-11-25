package config

import (
	"path/filepath"
	"testing"
)

func FuzzResolve(f *testing.F) {
	f.Add("disk", "/tmp/memory")
	f.Add("sqlite", "")
	f.Add("obsidian", "~/memory")
	f.Add("notion", "/tmp/x")
	f.Fuzz(func(t *testing.T, backend, home string) {
		_, _, _ = Resolve(Options{
			Vault: backend, MemoryHome: home,
			ConfigPath: filepath.Join(t.TempDir(), "missing.json"),
		})
	})
}
