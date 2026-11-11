// Package vault defines the storage gateway of Smart Agents Memory.
//
// The corpus lives behind a port. An adapter is a gateway: Obsidian (the
// default), a plain local directory, or SQLite. The domain never learns
// which one is in use, and a new one is a package plus one registration
// line.
package vault

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

// DefaultBackend is the recommended gateway.
const DefaultBackend = "obsidian"

// Vault is the storage gateway.
//
// Entries is the whole corpus. Create and Update write one entry. Search
// returns the matching lines of the store, with the path they came from.
// AppendLog adds one line to the append-only write log.
type Vault interface {
	// Kind is the name the backend is registered under.
	Kind() string
	// Root is where the store lives, for the user to see.
	Root() string
	// Entries returns every entry, ordered by type then title.
	Entries(ctx context.Context) ([]memory.Entry, error)
	// Create stores a new entry and returns it as persisted, with its
	// address and modification time filled in.
	Create(ctx context.Context, e memory.Entry) (memory.Entry, error)
	// Update overwrites an existing entry.
	Update(ctx context.Context, e memory.Entry) error
	// Search returns the lines of the store that match the query.
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
	// AppendLog appends one line to the append-only write log.
	AppendLog(ctx context.Context, line string) error
	// LogLines returns the lines of the write log, newest first.
	LogLines(ctx context.Context, limit int) ([]string, error)
	// Ready reports whether the store exists and can be read. A missing
	// store is not a failure of Open: install creates it.
	Ready(ctx context.Context) error
}

// Layout is implemented by gateways that can create their store. It is
// optional: install calls it when the gateway has a layout to materialize.
type Layout interface {
	// Ensure creates the store if it is missing. It is idempotent.
	Ensure(ctx context.Context) error
}

// Diagnostics is implemented by gateways that can tell a missing note from
// a note that could not be read. A note without frontmatter is not an
// entry, and it is not a problem.
type Diagnostics interface {
	// Problems lists the notes that could not be read.
	Problems(ctx context.Context) ([]string, error)
}

// ProjectNotes is implemented by gateways that keep one note per project.
// It is optional: a gateway that has no project notes simply does not
// implement it.
type ProjectNotes interface {
	ProjectNote(ctx context.Context, name string) (string, bool, error)
}

// Renamer is implemented by gateways that can move an entry to a new
// address, which is what a title change needs. It is optional.
type Renamer interface {
	// Rename writes the entry under a new title and removes the old one.
	Rename(ctx context.Context, e memory.Entry, title string) (memory.Entry, error)
}

// Remover is implemented by gateways that can delete an entry. It is
// optional.
type Remover interface {
	// Remove deletes an entry from the store.
	Remove(ctx context.Context, e memory.Entry) error
}

// IndexLocator is implemented by gateways that publish an index of the
// corpus. It is optional.
type IndexLocator interface {
	// IndexPath is where the index lives.
	IndexPath() string
}

// IndexWriter is implemented by gateways that publish an index of the
// corpus. It is optional.
type IndexWriter interface {
	// WriteIndex publishes the index and returns where it was written.
	WriteIndex(ctx context.Context, corpus []memory.Entry) (string, error)
}

// Hit is one search result: where it was found and what it says.
type Hit struct {
	// Path is the address of the note the match came from.
	Path string
	// Line is the 1-based line number, or 0 when the backend has no lines.
	Line int
	// Text is the matching line.
	Text string
	// Title is the entry the match belongs to, when there is one.
	Title string
}

// Factory builds a gateway from a configuration.
type Factory func(cfg Config) (Vault, error)

// Config is everything a gateway needs to find its store. It is also the
// configuration file of the product.
type Config struct {
	// Backend is the registered name of the gateway.
	Backend string `json:"vault,omitempty"`
	// MemoryHome is the root of the store. For the Obsidian and disk
	// gateways it is the folder that holds the corpus.
	MemoryHome string `json:"memory_home,omitempty"`
	// SQLitePath is the database file of the SQLite gateway.
	SQLitePath string `json:"sqlite_path,omitempty"`
	// ProjectionRoot is where the native projections are materialized.
	ProjectionRoot string `json:"projection_root,omitempty"`
}

var registry = map[string]Factory{}

// Register adds a gateway to the registry. Adapters call it from init.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// Names lists the registered gateways, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Open builds the gateway named by the configuration.
func Open(cfg Config) (Vault, error) {
	name := cfg.Backend
	if name == "" {
		name = DefaultBackend
	}
	if name == removedNotion {
		return nil, fmt.Errorf("the notion gateway was removed in 0.2.0 because it was not verified against the Notion API")
	}
	factory, ok := registry[name]
	if !ok {
		known := strings.Join(Names(), ", ")
		if known == "" {
			known = "none"
		}
		return nil, fmt.Errorf("unknown vault backend %q (registered: %s)", name, known)
	}
	return factory(cfg)
}

// removedNotion is the backend name of the gateway that left in 0.2.0.
// The configuration of an existing install may still name it, and the
// error has to say why it is gone.
const removedNotion = "notion"
