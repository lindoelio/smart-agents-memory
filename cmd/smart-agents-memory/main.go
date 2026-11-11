// Command smart-agents-memory is the command line of Smart Agents Memory.
//
// It is one entrypoint of the application: the same use cases will be
// served by an MCP server, without changing the domain.
package main

import (
	"context"
	"os"

	"github.com/lindoelio/smart-agents-memory/internal/cli"

	// The gateways are registered by their packages. An adapter is one
	// package plus one registration line.
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/disk"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/obsidian"
	_ "github.com/lindoelio/smart-agents-memory/internal/vault/sqlite"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
