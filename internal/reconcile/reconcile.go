// Package reconcile keeps the corpus and the native stores in step.
//
// The corpus is the single source of truth. The native stores are
// projections: they exist so that each tool reads the memory in the format
// it already expects. Reconciliation runs both ways, so a tool that writes
// straight into its own store never loses the fact.
package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/native"
	"github.com/lindoelio/smart-agents-memory/internal/projection"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

// Reconciler reconciles a corpus with the native projections.
type Reconciler struct {
	// Vault is the corpus.
	Vault vault.Vault
	// Projections is the local native store.
	Projections *projection.Store
}

// Import brings native entries into the corpus.
//
// An entry the corpus does not know yet is added. An entry both sides have
// is resolved by modification time: the most recent wins, so a manual edit
// of the corpus is never reverted by an older projection.
func (r *Reconciler) Import(ctx context.Context, corpus []memory.Entry) (int, int, error) {
	byTitle := map[string]memory.Entry{}
	for _, e := range corpus {
		byTitle[memory.NormTitle(e.Title)] = e
	}

	added, updated := 0, 0
	for _, candidate := range r.candidates() {
		// A projection that this tool wrote is not news: only what a
		// tool wrote after the last projection is worth importing.
		if !r.Projections.LastProjection().IsZero() &&
			!candidate.ModTime.IsZero() &&
			!candidate.ModTime.After(r.Projections.LastProjection()) {
			continue
		}
		key := memory.NormTitle(candidate.Title)
		current, ok := byTitle[key]
		if !ok {
			stored, err := r.Vault.Create(ctx, candidate)
			if err != nil {
				return added, updated, fmt.Errorf("cannot import %q: %w", candidate.Title, err)
			}
			corpus = append(corpus, stored)
			byTitle[key] = stored
			added++
			continue
		}
		if memory.NormBody(candidate.Body) == memory.NormBody(current.Body) {
			continue
		}
		if !current.ModTime.IsZero() && !candidate.ModTime.IsZero() && current.ModTime.After(candidate.ModTime) {
			// The corpus was edited more recently: it wins, and the
			// projection is regenerated on export.
			continue
		}
		current.Body = candidate.Body
		if candidate.Date != "" {
			current.Date = candidate.Date
		}
		if candidate.Reason != "" {
			current.Reason = candidate.Reason
		}
		if candidate.NativeFile != "" {
			current.NativeFile = candidate.NativeFile
		}
		current.Source = candidate.Source
		current.Native = candidate.Native
		current.NativeType = candidate.NativeType
		if err := r.Vault.Update(ctx, current); err != nil {
			return added, updated, fmt.Errorf("cannot update %q: %w", current.Title, err)
		}
		byTitle[key] = current
		updated++
	}
	return added, updated, nil
}

// Export projects the corpus into the native stores.
//
// It only creates: a projection is never merged, it is regenerated. An
// orphan projection is removed by the migrate command and reported by the
// doctor.
func (r *Reconciler) Export(ctx context.Context, corpus []memory.Entry) (int, error) {
	added := 0

	// MiniMax agent: hot entries live in the injected MEMORY.md.
	agentPath := "MEMORY.md"
	agentContent, _ := r.Projections.Read(native.TargetMiniMaxAgent, agentPath)
	have := native.AgentTitles(agentContent)
	var chunks []string
	for _, e := range corpus {
		if e.Native != string(native.TargetMiniMaxAgent) || e.EffectiveTier() != memory.TierHot {
			continue
		}
		key := memory.NormTitle(e.Title)
		if have[key] {
			continue
		}
		have[key] = true
		chunks = append(chunks, native.RenderAgent(e))
	}
	if len(chunks) > 0 {
		prefix := ""
		if strings.TrimSpace(agentContent) != "" {
			prefix = strings.TrimRight(agentContent, "\n") + "\n\n"
		}
		if err := r.Projections.Write(native.TargetMiniMaxAgent, agentPath, prefix+strings.Join(chunks, "\n")); err != nil {
			return added, err
		}
		added += len(chunks)
	}

	// Cold entries that the runtime wrote straight into the injected store
	// become topic files, so they stop growing the context of every session.
	if err := r.evictCold(native.TargetMiniMaxAgent, agentPath, corpus); err != nil {
		return added, err
	}

	// MiniMax user: hot entries live in the injected user.md.
	userPath := "user.md"
	userContent, _ := r.Projections.Read(native.TargetMiniMaxUser, userPath)
	haveUser := native.UserTitles(userContent)
	var userChunks []string
	for _, e := range corpus {
		if e.Native != string(native.TargetMiniMaxUser) || e.EffectiveTier() != memory.TierHot {
			continue
		}
		key := memory.NormTitle(e.Title)
		if haveUser[key] {
			continue
		}
		haveUser[key] = true
		userChunks = append(userChunks, native.RenderUser(e))
	}
	if len(userChunks) > 0 {
		prefix := ""
		if strings.TrimSpace(userContent) != "" {
			prefix = strings.TrimRight(userContent, "\n") + "\n\n"
		}
		if err := r.Projections.Write(native.TargetMiniMaxUser, userPath, prefix+strings.Join(userChunks, "\n")); err != nil {
			return added, err
		}
		added += len(userChunks)
	}
	if err := r.evictCold(native.TargetMiniMaxUser, userPath, corpus); err != nil {
		return added, err
	}

	// Cold entries become topic files: only the name and the description
	// enter the context, the body is read on demand.
	for _, e := range corpus {
		if e.EffectiveTier() == memory.TierHot {
			continue
		}
		if e.Native != string(native.TargetMiniMaxAgent) && e.Native != string(native.TargetMiniMaxUser) {
			continue
		}
		rel := "topics/" + memory.ASCIISlug(e.Title) + ".md"
		if r.Projections.Exists(native.TargetMiniMaxAgent, rel) {
			continue
		}
		if err := r.Projections.Write(native.TargetMiniMaxAgent, rel, native.RenderTopic(e)); err != nil {
			return added, err
		}
		added++
	}

	// Qwen: one file per entry, plus an index.
	var indexLines []string
	for _, e := range corpus {
		if e.Native != string(native.TargetQwen) {
			continue
		}
		name := e.NativeFile
		if name == "" {
			name = memory.Slugify(e.Title) + ".md"
		}
		if !r.Projections.Exists(native.TargetQwen, name) {
			if err := r.Projections.Write(native.TargetQwen, name, native.RenderQwen(e)); err != nil {
				return added, err
			}
			added++
		}
		desc := e.Description
		if desc == "" {
			desc = e.Title
		}
		indexLines = append(indexLines, fmt.Sprintf("- [%s](%s) — %s", e.Title, name, desc))
	}
	if len(indexLines) > 0 {
		existing, _ := r.Projections.Read(native.TargetQwen, "MEMORY.md")
		var lines []string
		for _, l := range strings.Split(existing, "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
		seen := map[string]bool{}
		for _, l := range lines {
			if m := mdLink.FindStringSubmatch(l); m != nil {
				seen[m[1]] = true
			}
		}
		for _, line := range indexLines {
			m := mdLink.FindStringSubmatch(line)
			if m != nil {
				if seen[m[1]] {
					continue
				}
				seen[m[1]] = true
			}
			lines = append(lines, line)
		}
		if err := r.Projections.Write(native.TargetQwen, "MEMORY.md", strings.Join(lines, "\n")+"\n"); err != nil {
			return added, err
		}
	}

	return added, nil
}

// SyncProfile keeps the hot tier block of the MiniMax user store in step
// with the hot tier of the corpus.
func (r *Reconciler) SyncProfile(hot string) error {
	if hot == "" {
		return nil
	}
	content, err := r.Projections.Read(native.TargetMiniMaxUser, "user.md")
	if err != nil {
		return nil
	}
	block := ""
	if m := profileBlock.FindStringSubmatch(hot); m != nil {
		block = strings.TrimSpace(m[1])
	}
	var updated string
	if strings.Contains(content, native.HotProfileStart) {
		updated = profileBlock.ReplaceAllString(content,
			native.HotProfileStart+"\n"+block+"\n"+native.HotProfileEnd)
	} else {
		updated = native.HotProfileStart + "\n" + block + "\n" + native.HotProfileEnd + "\n\n" + strings.TrimLeft(content, "\n")
	}
	if updated == content {
		return nil
	}
	return r.Projections.Write(native.TargetMiniMaxUser, "user.md", updated)
}

// evictCold removes cold entries from an injected store.
func (r *Reconciler) evictCold(target native.Target, rel string, corpus []memory.Entry) error {
	content, err := r.Projections.Read(target, rel)
	if err != nil || content == "" {
		return nil
	}
	cold := map[string]bool{}
	for _, e := range corpus {
		if e.Native == string(target) && e.EffectiveTier() != memory.TierHot {
			cold[memory.NormTitle(e.Title)] = true
		}
	}
	if len(cold) == 0 {
		return nil
	}
	var kept []string
	dropped := false
	for _, block := range native.SplitBlocks(content) {
		if strings.TrimSpace(block) == "" {
			continue
		}
		m := regexp.MustCompile(`(?m)^### (.*)$`).FindStringSubmatch(block)
		if m != nil {
			title := strings.TrimSpace(regexp.MustCompile(`\s*\(\d{4}-\d{2}-\d{2}\)\s*$`).ReplaceAllString(m[1], ""))
			if cold[memory.NormTitle(title)] {
				dropped = true
				continue
			}
		}
		kept = append(kept, strings.TrimRight(block, "\n"))
	}
	if !dropped {
		return nil
	}
	out := strings.Join(kept, "\n\n")
	if out != "" {
		out += "\n"
	}
	return r.Projections.Write(target, rel, out)
}

// candidates reads every entry the native stores hold.
func (r *Reconciler) candidates() []memory.Entry {
	var out []memory.Entry

	if content, err := r.Projections.Read(native.TargetMiniMaxAgent, "MEMORY.md"); err == nil {
		for _, e := range native.ParseAgent(content) {
			if mod, err := r.Projections.ModTime(native.TargetMiniMaxAgent, "MEMORY.md"); err == nil {
				e.ModTime = mod
			}
			out = append(out, e)
		}
	}
	if content, err := r.Projections.Read(native.TargetMiniMaxUser, "user.md"); err == nil {
		for _, e := range native.ParseUser(content) {
			if mod, err := r.Projections.ModTime(native.TargetMiniMaxUser, "user.md"); err == nil {
				e.ModTime = mod
			}
			out = append(out, e)
		}
	}
	if files, err := r.Projections.List(native.TargetMiniMaxAgent, "topics"); err == nil {
		for _, rel := range files {
			content, err := r.Projections.Read(native.TargetMiniMaxAgent, rel)
			if err != nil {
				continue
			}
			e, ok := native.ParseTopic(content, filepath.Base(rel))
			if !ok {
				continue
			}
			if mod, err := r.Projections.ModTime(native.TargetMiniMaxAgent, rel); err == nil {
				e.ModTime = mod
			}
			out = append(out, e)
		}
	}
	if files, err := r.Projections.List(native.TargetQwen, ""); err == nil {
		for _, rel := range files {
			if rel == "MEMORY.md" {
				continue
			}
			content, err := r.Projections.Read(native.TargetQwen, rel)
			if err != nil {
				continue
			}
			e, ok := native.ParseQwen(content, filepath.Base(rel))
			if !ok {
				continue
			}
			if mod, err := r.Projections.ModTime(native.TargetQwen, rel); err == nil {
				e.ModTime = mod
			}
			out = append(out, e)
		}
	}
	return out
}

// Orphans returns the topic files that no corpus entry backs.
func (r *Reconciler) Orphans(corpus []memory.Entry) ([]string, error) {
	known := map[string]bool{}
	for _, e := range corpus {
		known[memory.NormTitle(e.Title)] = true
	}
	var out []string
	files, err := r.Projections.List(native.TargetMiniMaxAgent, "topics")
	if err != nil {
		return nil, err
	}
	for _, rel := range files {
		content, err := r.Projections.Read(native.TargetMiniMaxAgent, rel)
		if err != nil {
			continue
		}
		e, ok := native.ParseTopic(content, filepath.Base(rel))
		if !ok {
			out = append(out, rel)
			continue
		}
		if !known[memory.NormTitle(e.Title)] {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

var (
	mdLink       = regexp.MustCompile(`\(([^)]+\.md)\)`)
	profileBlock = regexp.MustCompile(
		regexp.QuoteMeta(native.HotProfileStart) + `(?s)(.*?)` + regexp.QuoteMeta(native.HotProfileEnd))
)

// WriteHot writes the hot tier file of the local store.
func WriteHot(localRoot, content string) error {
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(localRoot, "MEMORY.md"), []byte(content), 0o644)
}
