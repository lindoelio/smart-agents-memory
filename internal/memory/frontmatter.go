package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Known frontmatter keys, in the order Format writes them. Anything else is
// preserved through Extra so a round-trip never drops what the user wrote.
var knownKeys = []string{
	"title", "type", "date", "source", "native", "tier",
	"native_file", "native_type", "reason", "description", "keywords",
}

// Parse splits a corpus note into its frontmatter and its body.
//
// The frontmatter is real YAML, so notes edited by hand — or by Obsidian,
// which writes its own YAML — round-trip cleanly. A note without frontmatter,
// or without a title, is not an entry.
func Parse(text string) (Entry, bool) {
	head, body, ok := splitFrontmatter(text)
	if !ok {
		return Entry{}, false
	}

	meta := map[string]any{}
	if err := yaml.Unmarshal([]byte(head), &meta); err != nil {
		// Fall back to a line-based read. A hand-edited note can carry YAML
		// that a strict parser rejects; the title and the body still matter.
		meta = map[string]any{}
		for _, line := range strings.Split(head, "\n") {
			k, v, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			meta[strings.TrimSpace(k)] = scalar(strings.TrimSpace(v))
		}
	}

	flat := map[string]string{}
	for k, v := range meta {
		flat[k] = scalar(v)
	}

	title := flat["title"]
	if title == "" {
		return Entry{}, false
	}

	keywords := []string{}
	if raw, ok := meta["keywords"]; ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				keywords = append(keywords, scalar(item))
			}
		} else if s := scalar(raw); s != "" {
			keywords = append(keywords, s)
		}
	}

	entry := Entry{
		Title:       title,
		Type:        Type(flat["type"]),
		Date:        flat["date"],
		Source:      flat["source"],
		Native:      flat["native"],
		Tier:        Tier(flat["tier"]),
		NativeFile:  flat["native_file"],
		NativeType:  flat["native_type"],
		Reason:      flat["reason"],
		Description: flat["description"],
		Keywords:    keywords,
		Body:        strings.TrimSpace(stripTitleHeading(body)),
	}
	entry.Extra = map[string]string{}
	for k, v := range flat {
		if !isKnownKey(k) {
			entry.Extra[k] = v
		}
	}
	return entry, true
}

// Format renders an entry as a corpus note: frontmatter, then the title as
// a heading, then the body.
func Format(e Entry) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + quote(e.Title) + "\n")
	b.WriteString("type: " + string(e.Type) + "\n")
	if e.Date == "" {
		e.Date = time.Now().Format("2006-01-02")
	}
	b.WriteString("date: " + e.Date + "\n")
	source := e.Source
	if source == "" {
		source = DefaultSource
	}
	b.WriteString("source: " + quote(source) + "\n")
	if e.Native != "" {
		b.WriteString("native: " + e.Native + "\n")
	}
	if e.Tier != "" {
		b.WriteString("tier: " + string(e.Tier) + "\n")
	}
	if e.NativeFile != "" {
		b.WriteString("native_file: " + e.NativeFile + "\n")
	}
	if e.NativeType != "" {
		b.WriteString("native_type: " + e.NativeType + "\n")
	}
	if e.Reason != "" {
		b.WriteString("reason: " + quote(e.Reason) + "\n")
	}
	if e.Description != "" {
		b.WriteString("description: " + quote(e.Description) + "\n")
	}
	if len(e.Keywords) > 0 {
		b.WriteString("keywords:\n")
		for _, k := range e.Keywords {
			b.WriteString("  - " + k + "\n")
		}
	}
	for _, k := range sortedKeys(e.Extra) {
		b.WriteString(k + ": " + e.Extra[k] + "\n")
	}
	b.WriteString("---\n")
	b.WriteString("\n## " + e.Title + "\n\n")
	b.WriteString(strings.TrimSpace(e.Body) + "\n")
	return b.String()
}

// DefaultSource is written when an entry does not name its origin.
const DefaultSource = "smart-agents-memory-cli"

// splitFrontmatter cuts the YAML block from the body.
func splitFrontmatter(text string) (head, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", false
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		if strings.HasSuffix(text, "\n---") {
			return text[4 : len(text)-4], "", true
		}
		return "", "", false
	}
	return text[4 : 4+end], text[4+end+5:], true
}

// stripTitleHeading removes the "## Title" heading the writer adds, so the
// stored body and the rendered body are the same thing.
func stripTitleHeading(body string) string {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "## ") {
		if idx := strings.Index(trimmed, "\n"); idx >= 0 {
			return trimmed[idx+1:]
		}
		return ""
	}
	return trimmed
}

// isKnownKey reports whether the key is written by Format itself.
func isKnownKey(k string) bool {
	for _, known := range knownKeys {
		if k == known {
			return true
		}
	}
	return false
}

// sortedKeys returns the extra keys in a stable order.
func sortedKeys(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	out := make([]string, 0, len(extra))
	for k := range extra {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// scalar renders a YAML value as the plain string the corpus uses.
func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case time.Time:
		return t.Format("2006-01-02")
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, scalar(item))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", t)
	}
}

// quote renders a string as a double-quoted JSON string, without HTML
// escaping, so accented characters stay readable in the note.
func quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimRight(buf.String(), "\n")
}
