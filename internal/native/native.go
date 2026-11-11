// Package native knows the projection formats of the AI tools.
//
// The projections are dictated by the tools, not by the vault: MiniMax Code
// and Qwen each have their own shape. That is why this package sits in the
// domain, not behind the vault port.
package native

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
)

// Target names a native store.
type Target string

const (
	// TargetMiniMaxUser is the MiniMax Code user profile store.
	TargetMiniMaxUser Target = "minimax-user"
	// TargetMiniMaxAgent is the MiniMax Code agent store.
	TargetMiniMaxAgent Target = "minimax-agent"
	// TargetQwen is the Qwen Code topic store.
	TargetQwen Target = "qwen"
)

// Targets lists every supported native projection.
//
// The newer MiniMax store is not here: its format is not known, and a
// directory nothing writes is not a projection.
var Targets = []Target{TargetMiniMaxUser, TargetMiniMaxAgent, TargetQwen}

// Valid reports whether t is a known native target.
func (t Target) Valid() bool {
	for _, known := range Targets {
		if t == known {
			return true
		}
	}
	return false
}

// TitleMark is the marker that carries the full title inside a topic file.
// The runtime only injects the name, the description and the size; the body
// is read on demand, and the title lives in this marker.
const TitleMark = "<!-- smart-agents-memory:title:"

// HotProfileStart and HotProfileEnd delimit the hot tier block inside the
// MiniMax user store.
const (
	HotProfileStart = "<!-- smart-agents-memory:hot:start -->"
	HotProfileEnd   = "<!-- smart-agents-memory:hot:end -->"
)

// TopicDescription is the max length of the description the runtime injects.
const TopicDescription = 120

var (
	headingRe = regexp.MustCompile(`(?m)^### (.*)$`)
	dateRe    = regexp.MustCompile(`\s*\(\d{4}-\d{2}-\d{2}\)\s*$`)
	titleMark = regexp.MustCompile(`(?s)<!-- smart-agents-memory:title:(.*?)-->`)
)

// RenderAgent renders an entry in the MiniMax agent store format: a header
// with the date, the type line, then the body.
func RenderAgent(e memory.Entry) string {
	date := e.Date
	if date == "" {
		date = today()
	}
	ntype := e.NativeType
	if ntype == "" {
		ntype = "reference"
	}
	return "### " + e.Title + " (" + date + ")\nType: " + ntype + "\n\n" + strings.TrimSpace(e.Body) + "\n"
}

// RenderUser renders an entry in the MiniMax user store format.
func RenderUser(e memory.Entry) string {
	head := ""
	if e.Reason != "" {
		head = "<!-- mem-append-reason: " + e.Reason + " -->\n"
	}
	return head + "### " + e.Title + "\n\n" + strings.TrimSpace(e.Body) + "\n"
}

// RenderTopic renders an entry as a MiniMax topic file. Only the
// frontmatter enters the context; the body is read on demand.
func RenderTopic(e memory.Entry) string {
	desc := e.Description
	if desc == "" {
		desc = e.Title
	}
	if len(desc) > TopicDescription {
		desc = desc[:TopicDescription-3]
		if idx := strings.LastIndex(desc, " "); idx > 0 {
			desc = desc[:idx]
		}
		desc += "..."
	}
	body := strings.TrimSpace(e.Body)
	return "---\ndescription: " + yamlScalar(desc) + "\n---\n\n" +
		TitleMark + e.Title + " -->\n" + body + "\n"
}

// RenderHot renders the hot tier: every hot entry, as a heading and a body,
// in type order then title order. The same text is what every harness reads.
func RenderHot(entries []memory.Entry) string {
	var hot []memory.Entry
	for _, e := range entries {
		if e.EffectiveTier() == memory.TierHot {
			hot = append(hot, e)
		}
	}
	sort.SliceStable(hot, func(i, j int) bool {
		ti, tj := typeIndex(hot[i].Type), typeIndex(hot[j].Type)
		if ti != tj {
			return ti < tj
		}
		return strings.ToLower(hot[i].Title) < strings.ToLower(hot[j].Title)
	})
	var b strings.Builder
	for _, e := range hot {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimRight(RenderUser(e), "\n"))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func typeIndex(t memory.Type) int {
	for i, known := range memory.Types {
		if t == known {
			return i
		}
	}
	return len(memory.Types)
}

// yamlScalar renders one YAML scalar. A space stays a space: the description
// is injected into the context, and a query escape would show a plus.
func yamlScalar(s string) string {
	data, err := yaml.Marshal(s)
	if err != nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(data))
}

// RenderQwen renders an entry in the Qwen topic format.
func RenderQwen(e memory.Entry) string {
	ntype := e.NativeType
	if ntype == "" {
		ntype = "user"
	}
	desc := e.Description
	if desc == "" {
		desc = e.Title
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + e.Title + "\n")
	b.WriteString("description: " + desc + "\n")
	b.WriteString("type: " + ntype + "\n")
	b.WriteString("category: shared_memory\n")
	if len(e.Keywords) > 0 {
		b.WriteString("keywords:\n")
		for _, k := range e.Keywords {
			b.WriteString("  - " + k + "\n")
		}
	}
	b.WriteString("usage_scenarios:\n")
	b.WriteString("  - \"query the user's shared memory\"\n")
	b.WriteString("---\n")
	b.WriteString("\n" + strings.TrimSpace(e.Body) + "\n")
	return b.String()
}

// ParseAgent reads the MiniMax agent store and returns its entries.
func ParseAgent(content string) []memory.Entry {
	var out []memory.Entry
	for _, block := range SplitBlocks(content) {
		m := headingRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		title := strings.TrimSpace(dateRe.ReplaceAllString(m[1], ""))
		body := block[strings.Index(block, "\n")+1:]
		ntype := "reference"
		// The type line is the first line RenderAgent writes. A block that
		// was edited by hand may have it later, after a reason comment.
		if rest, ok := strings.CutPrefix(body, "Type: "); ok {
			ntype = strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])
			if i := strings.Index(rest, "\n"); i >= 0 {
				body = rest[i+1:]
			} else {
				body = ""
			}
		} else if idx := strings.Index(body, "\nType: "); idx >= 0 {
			rest := body[idx+len("\nType: "):]
			ntype = strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])
			if i := strings.Index(rest, "\n"); i >= 0 {
				body = body[:idx] + rest[i+1:]
			} else {
				body = body[:idx]
			}
		}
		out = append(out, memory.Entry{
			Title:      title,
			Type:       memory.TypeLesson,
			Body:       strings.TrimSpace(body),
			NativeType: ntype,
			Source:     "minimax-agent-memory",
			Native:     string(TargetMiniMaxAgent),
		})
	}
	return out
}

// ParseUser reads the MiniMax user store and returns its entries.
func ParseUser(content string) []memory.Entry {
	var out []memory.Entry
	// RenderUser writes the reason on the line before the heading, so the
	// splitter leaves it in the previous block.
	pendingReason := ""
	for _, block := range SplitBlocks(content) {
		m := headingRe.FindStringSubmatch(block)
		if m == nil {
			pendingReason = reasonComment(block)
			continue
		}
		title := strings.TrimSpace(m[1])
		body := block[strings.Index(block, "\n")+1:]
		reason := pendingReason
		pendingReason = ""
		if strings.HasPrefix(body, "<!-- mem-append-reason: ") {
			if idx := strings.Index(body, "-->\n"); idx >= 0 {
				reason = strings.TrimSpace(body[len("<!-- mem-append-reason: "):idx])
				body = body[idx+4:]
			}
		}
		out = append(out, memory.Entry{
			Title:      title,
			Type:       memory.TypeUser,
			Body:       strings.TrimSpace(body),
			NativeType: "user",
			Source:     "minimax-user-memory",
			Native:     string(TargetMiniMaxUser),
			Reason:     reason,
		})
	}
	return out
}

// reasonComment reads a mem-append-reason comment, when the block is one.
func reasonComment(block string) string {
	const prefix = "<!-- mem-append-reason: "
	trimmed := strings.TrimSpace(block)
	if !strings.HasPrefix(trimmed, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, prefix)
	if idx := strings.Index(rest, "-->"); idx >= 0 {
		return strings.TrimSpace(rest[:idx])
	}
	return ""
}

// ParseTopic reads a MiniMax topic file and returns its entry.
func ParseTopic(content, fileName string) (memory.Entry, bool) {
	head, body, ok := splitFrontmatter(content)
	if !ok {
		return memory.Entry{}, false
	}
	desc := ""
	meta := map[string]any{}
	if err := yaml.Unmarshal([]byte(head), &meta); err == nil {
		if raw, ok := meta["description"]; ok {
			desc = scalar(raw)
		}
	}
	title := ""
	if m := titleMark.FindStringSubmatch(body); m != nil {
		title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), "-->"))
	}
	if title == "" {
		title = desc
	}
	if title == "" {
		return memory.Entry{}, false
	}
	// The marker carries the title for the runtime; it is not part of the
	// body of the memory.
	clean := strings.TrimSpace(titleMark.ReplaceAllString(body, ""))
	return memory.Entry{
		Title:       title,
		Type:        memory.TypeLesson,
		Body:        clean,
		NativeType:  "reference",
		Source:      "minimax-topic",
		Native:      string(TargetMiniMaxAgent),
		NativeFile:  fileName,
		Description: desc,
	}, true
}

// ParseQwen reads a Qwen topic file and returns its entry.
func ParseQwen(content, fileName string) (memory.Entry, bool) {
	head, body, ok := splitFrontmatter(content)
	if !ok {
		return memory.Entry{}, false
	}
	meta := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	title := meta["name"]
	if title == "" {
		return memory.Entry{}, false
	}
	typ := memory.TypeLesson
	if meta["type"] == "user" {
		typ = memory.TypeUser
	}
	return memory.Entry{
		Title:       title,
		Type:        typ,
		Body:        strings.TrimSpace(body),
		NativeType:  meta["type"],
		Source:      "qwen-memory",
		Native:      string(TargetQwen),
		NativeFile:  fileName,
		Description: meta["description"],
	}, true
}

// AgentTitles returns the titles already present in a MiniMax agent store,
// without the trailing date: in that store the date is header metadata.
func AgentTitles(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatch(content, -1) {
		out[memory.NormTitle(dateRe.ReplaceAllString(m[1], ""))] = true
	}
	return out
}

// UserTitles returns the titles already present in a MiniMax user store.
func UserTitles(content string) map[string]bool {
	out := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatch(content, -1) {
		out[memory.NormTitle(m[1])] = true
	}
	return out
}

// today is the current date in the format the stores use.
func today() string {
	return time.Now().Format("2006-01-02")
}

// SplitBlocks cuts a store into its "### Title" blocks.
//
// The text before the first heading is a block of its own: a store may open
// with the hot tier block, which is not an entry.
func SplitBlocks(content string) []string {
	var out []string
	var current strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "### ") && current.Len() > 0 {
			out = append(out, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if strings.TrimSpace(current.String()) != "" {
		out = append(out, strings.TrimRight(current.String(), "\n"))
	}
	return out
}

// splitFrontmatter cuts the YAML block from the body.
func splitFrontmatter(text string) (head, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", false
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return "", "", false
	}
	return text[4 : 4+end], text[4+end+5:], true
}

func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
