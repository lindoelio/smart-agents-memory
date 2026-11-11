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
	"time"

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
		if candidate.ModTime.IsZero() || (!current.ModTime.IsZero() && !candidate.ModTime.After(current.ModTime)) {
			// The corpus is newer, or the two share a second. It wins,
			// and the projection is regenerated on export. A projection
			// has to be strictly newer to replace the corpus.
			continue
		}
		// The projection parser invents a type and a native target. An
		// entry the corpus already has keeps both: a user note projected
		// as a topic must not come back as a lesson.
		current.Body = candidate.Body
		if candidate.Description != "" {
			current.Description = candidate.Description
		}
		if candidate.Date != "" {
			current.Date = candidate.Date
		}
		if candidate.Reason != "" {
			current.Reason = candidate.Reason
		}
		if candidate.NativeFile != "" {
			current.NativeFile = candidate.NativeFile
		}
		if err := r.Vault.Update(ctx, current); err != nil {
			return added, updated, fmt.Errorf("cannot update %q: %w", current.Title, err)
		}
		byTitle[key] = current
		updated++
	}
	return added, updated, nil
}

// ProfileTitle is the entry a freeform hot block becomes, once.
const ProfileTitle = "Profile"

// Export projects the corpus into the native stores.
//
// A projection is regenerated. A file whose bytes already match is left
// alone, so its modification time does not look like a newer edit. A
// projection no entry backs is removed.
func (r *Reconciler) Export(_ context.Context, corpus []memory.Entry) (int, error) {
	changed := 0
	hot := native.RenderHot(corpus)

	agent := renderAgent(corpus)
	wrote, err := r.Projections.WriteIfChanged(native.TargetMiniMaxAgent, "MEMORY.md", agent)
	if err != nil {
		return changed, err
	}
	if wrote {
		changed++
	}

	user := renderUserFile(hot, corpus)
	wrote, err = r.Projections.WriteIfChanged(native.TargetMiniMaxUser, "user.md", user)
	if err != nil {
		return changed, err
	}
	if wrote {
		changed++
	}

	wantedTopics := map[string]bool{}
	for _, e := range corpus {
		if e.EffectiveTier() == memory.TierHot {
			continue
		}
		if e.Native != string(native.TargetMiniMaxAgent) && e.Native != string(native.TargetMiniMaxUser) {
			continue
		}
		rel := "topics/" + memory.ASCIISlug(e.Title) + ".md"
		wantedTopics[rel] = true
		wrote, err := r.Projections.WriteIfChanged(native.TargetMiniMaxAgent, rel, native.RenderTopic(e))
		if err != nil {
			return changed, err
		}
		if wrote {
			changed++
		}
	}
	removed, err := r.removeAbsent(native.TargetMiniMaxAgent, "topics", wantedTopics, false)
	if err != nil {
		return changed, err
	}
	changed += removed

	wantedQwen := map[string]bool{"MEMORY.md": true}
	var indexLines []string
	for _, e := range corpus {
		if e.Native != string(native.TargetQwen) {
			continue
		}
		name := e.NativeFile
		if name == "" {
			name = memory.Slugify(e.Title) + ".md"
		}
		wantedQwen[name] = true
		wrote, err := r.Projections.WriteIfChanged(native.TargetQwen, name, native.RenderQwen(e))
		if err != nil {
			return changed, err
		}
		if wrote {
			changed++
		}
		desc := e.Description
		if desc == "" {
			desc = e.Title
		}
		indexLines = append(indexLines, fmt.Sprintf("- [%s](%s) — %s", e.Title, name, desc))
	}
	qwenIndex := ""
	if len(indexLines) > 0 {
		qwenIndex = strings.Join(indexLines, "\n") + "\n"
	}
	wrote, err = r.Projections.WriteIfChanged(native.TargetQwen, "MEMORY.md", qwenIndex)
	if err != nil {
		return changed, err
	}
	if wrote {
		changed++
	}
	removed, err = r.removeAbsent(native.TargetQwen, "", wantedQwen, true)
	if err != nil {
		return changed, err
	}
	changed += removed
	return changed, nil
}

// ReconcileHot makes the hot tier file a projection of the hot entries.
//
// A freeform block, with no headings, is imported once as Profile. A block
// edited after the last projection is imported when it is newer than the
// corpus. The frontmatter of the file is kept.
func (r *Reconciler) ReconcileHot(ctx context.Context, hotPath string, corpus []memory.Entry) (int, int, error) {
	content, err := os.ReadFile(hotPath)
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, err
	}
	text := string(content)
	interior := hotInterior(text)
	canonical := native.RenderHot(corpus)
	added, updated := 0, 0

	info, statErr := os.Stat(hotPath)
	edited := statErr == nil && (r.Projections.LastProjection().IsZero() || info.ModTime().After(r.Projections.LastProjection()))
	if edited && memory.NormBody(interior) != memory.NormBody(canonical) && strings.TrimSpace(interior) != "" {
		fileMod := info.ModTime()
		added, updated, err = r.importHot(ctx, interior, fileMod, corpus)
		if err != nil {
			return added, updated, err
		}
		corpus, err = r.Vault.Entries(ctx)
		if err != nil {
			return added, updated, err
		}
		canonical = native.RenderHot(corpus)
	}

	next := applyHotBlock(text, canonical)
	if next != text {
		if err := os.MkdirAll(filepath.Dir(hotPath), 0o755); err != nil {
			return added, updated, err
		}
		if err := os.WriteFile(hotPath, []byte(next), 0o644); err != nil {
			return added, updated, err
		}
	}
	return added, updated, nil
}

// importHot brings a hand-edited hot block into the corpus.
func (r *Reconciler) importHot(ctx context.Context, interior string, fileMod time.Time, corpus []memory.Entry) (int, int, error) {
	parsed := native.ParseUser(interior)
	if len(parsed) == 0 {
		if hotBodyKnown(corpus, interior) {
			return 0, 0, nil
		}
		n, u, err := r.upsertHot(ctx, corpus, ProfileTitle, strings.TrimSpace(interior), "", fileMod)
		return n, u, err
	}
	added, updated := 0, 0
	for _, e := range parsed {
		n, u, err := r.upsertHot(ctx, corpus, e.Title, e.Body, e.Reason, fileMod)
		if err != nil {
			return added, updated, err
		}
		added += n
		updated += u
		if n+u > 0 {
			var err error
			corpus, err = r.Vault.Entries(ctx)
			if err != nil {
				return added, updated, err
			}
		}
	}
	return added, updated, nil
}

// upsertHot creates or updates one hot entry. The corpus wins when it is
// newer than the file.
func (r *Reconciler) upsertHot(ctx context.Context, corpus []memory.Entry, title, body, reason string, fileMod time.Time) (int, int, error) {
	key := memory.NormTitle(title)
	for _, current := range corpus {
		if memory.NormTitle(current.Title) != key {
			continue
		}
		if !current.ModTime.IsZero() && !fileMod.After(current.ModTime) {
			return 0, 0, nil
		}
		if memory.NormBody(current.Body) == memory.NormBody(body) && (reason == "" || current.Reason == reason) {
			return 0, 0, nil
		}
		current.Body = body
		if reason != "" {
			current.Reason = reason
		}
		if current.Tier == "" {
			current.Tier = memory.TierHot
		}
		if err := r.Vault.Update(ctx, current); err != nil {
			return 0, 0, err
		}
		return 0, 1, nil
	}
	entry := memory.Entry{
		Title:      title,
		Type:       memory.TypeUser,
		Date:       time.Now().Format("2006-01-02"),
		Body:       body,
		Tier:       memory.TierHot,
		Source:     memory.DefaultSource,
		Native:     string(native.TargetMiniMaxUser),
		NativeType: "user",
		Reason:     reason,
	}
	if _, err := r.Vault.Create(ctx, entry); err != nil {
		return 0, 0, err
	}
	return 1, 0, nil
}

func hotBodyKnown(corpus []memory.Entry, body string) bool {
	want := memory.NormBody(body)
	for _, e := range corpus {
		if e.EffectiveTier() == memory.TierHot && memory.NormBody(e.Body) == want {
			return true
		}
	}
	return false
}

func renderAgent(corpus []memory.Entry) string {
	var chunks []string
	for _, e := range corpus {
		if e.Native == string(native.TargetMiniMaxAgent) && e.EffectiveTier() == memory.TierHot {
			chunks = append(chunks, strings.TrimRight(native.RenderAgent(e), "\n"))
		}
	}
	if len(chunks) == 0 {
		return ""
	}
	return strings.Join(chunks, "\n\n") + "\n"
}

// renderUserFile is the MiniMax user store: the hot block every harness
// shares, then the headings of the hot entries that target this store.
// The block already carries those headings, so they are not repeated.
func renderUserFile(hot string, _ []memory.Entry) string {
	return applyHotBlock("", hot)
}

func hotInterior(content string) string {
	m := profileBlock.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func applyHotBlock(content, block string) string {
	inner := native.HotProfileStart + "\n"
	if strings.TrimSpace(block) != "" {
		inner += strings.TrimSpace(block) + "\n"
	}
	inner += native.HotProfileEnd
	if loc := profileBlock.FindStringIndex(content); loc != nil {
		// Splice the bytes. ReplaceAllString would treat $ in a body as a backreference.
		return content[:loc[0]] + inner + content[loc[1]:]
	}
	if strings.TrimSpace(content) == "" {
		return "---\n" +
			"description: Shared memory — the profile and the permanent rules. Read before working.\n" +
			"type: index\n" +
			"alwaysApply: true\n" +
			"---\n\n" + inner + "\n"
	}
	return strings.TrimRight(content, "\n") + "\n\n" + inner + "\n"
}

// removeAbsent deletes projection files that the corpus no longer names.
func (r *Reconciler) removeAbsent(target native.Target, sub string, keep map[string]bool, rootOnly bool) (int, error) {
	files, err := r.Projections.List(target, sub)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, rel := range files {
		if keep[rel] {
			continue
		}
		if rootOnly && strings.Contains(rel, "/") {
			continue
		}
		if err := r.Projections.Delete(target, rel); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
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

var profileBlock = regexp.MustCompile(
	regexp.QuoteMeta(native.HotProfileStart) + `(?s)(.*?)` + regexp.QuoteMeta(native.HotProfileEnd))

// WriteHot writes the hot tier file of the local store.
func WriteHot(localRoot, content string) error {
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(localRoot, "MEMORY.md"), []byte(content), 0o644)
}
