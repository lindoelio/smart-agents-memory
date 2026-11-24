// Package llm defines the optional language-model port.
//
// The memory features that need a model — recall selection and Dream
// consolidation — depend on this interface, never on a vendor adapter.
package llm

import "context"

// Request is one completion request.
type Request struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float64
}

// Client is a JSON-completion backend.
type Client interface {
	// Kind names the adapter.
	Kind() string
	// CompleteJSON asks for a JSON object and unmarshals it into out.
	CompleteJSON(ctx context.Context, req Request, out any) error
}
