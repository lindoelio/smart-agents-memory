package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHookScriptQuotesAPathWithSpaces(t *testing.T) {
	script := hookScript("/Users/a b/Applications/smart-agents-memory")
	if !strings.Contains(script, "bin='/Users/a b/Applications/smart-agents-memory'") {
		t.Fatalf("the script does not quote the binary:\n%s", script)
	}
	if !strings.Contains(script, `"$bin" extract --stdin --auto-dream --quiet`) {
		t.Fatalf("the script does not invoke the quoted binary:\n%s", script)
	}
	if !strings.Contains(script, "|| true") {
		t.Fatalf("the script can block the harness:\n%s", script)
	}
}

func TestShellQuoteEscapesASingleQuote(t *testing.T) {
	if got, want := shellQuote("it's a path"), `'it'\''s a path'`; got != want {
		t.Fatalf("shellQuote returned %q, want %q", got, want)
	}
}

func TestHookSnippetIsValidJSON(t *testing.T) {
	path := `/tmp/a b/"post-turn".sh`
	var decoded struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(hookSnippet(path)), &decoded); err != nil {
		t.Fatalf("the snippet is not JSON: %v", err)
	}
	if decoded.Command != path {
		t.Fatalf("the snippet carries %q, want %q", decoded.Command, path)
	}
}
