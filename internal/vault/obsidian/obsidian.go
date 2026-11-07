// Package obsidian is the Obsidian gateway, and the recommended one.
//
// The store is a vault: notes live in Markdown with YAML frontmatter that
// Obsidian reads as properties. Discovery is recursive, the way Obsidian
// itself is, so a memory can live in any folder of the vault; the index is
// published as an Obsidian-native note with wikilinks, so the memory is
// navigable from the vault graph and through backlinks.
package obsidian

import (
	"errors"

	"github.com/lindoelio/smart-agents-memory/internal/vault"
	"github.com/lindoelio/smart-agents-memory/internal/vault/filevault"
)

// IndexName is the Obsidian-native index note of the memory.
const IndexName = "Smart Agents Memory.md"

func init() {
	vault.Register("obsidian", func(cfg vault.Config) (vault.Vault, error) {
		if cfg.MemoryHome == "" {
			return nil, errors.New("the obsidian gateway needs a memory home")
		}
		return filevault.New(cfg.MemoryHome, filevault.Options{
			Kind:          "obsidian",
			Recursive:     true,
			ObsidianIndex: true,
		}), nil
	})
}
