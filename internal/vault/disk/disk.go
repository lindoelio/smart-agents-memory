// Package disk is the plain directory gateway: one Markdown note per memory,
// under corpus/<type>/, with no Obsidian conventions.
//
// Use it when the store must not depend on any note application.
package disk

import (
	"github.com/lindoelio/smart-agents-memory/internal/vault"
	"github.com/lindoelio/smart-agents-memory/internal/vault/filevault"
)

func init() {
	vault.Register("disk", func(cfg vault.Config) (vault.Vault, error) {
		return filevault.New(cfg.MemoryHome, filevault.Options{Kind: "disk"}), nil
	})
}
