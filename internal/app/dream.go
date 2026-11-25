package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/llm"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/secretguard"
	"github.com/lindoelio/smart-agents-memory/internal/vault"
)

const (
	dreamStateFile = ".smart-agents-memory-dream.json"
	dreamLockFile  = ".consolidation.lock"
)

// DreamState is the persistent state of consolidation.
type DreamState struct {
	Version        int       `json:"version"`
	LastDreamAt    time.Time `json:"last_dream_at,omitempty"`
	DirtyMutations int       `json:"dirty_mutations"`
	LastStatus     string    `json:"last_status,omitempty"`
}

// DreamOptions selects a consolidation run.
type DreamOptions struct {
	DryRun            bool
	DeterministicOnly bool
	MinDirty          int
}

// DreamReport is the outcome of consolidation.
type DreamReport struct {
	Merged     int
	Updated    int
	Deleted    int
	Backfilled int
	DryRun     bool
}

// DreamOperation is one validated LLM consolidation operation.
type DreamOperation struct {
	Action         string   `json:"action"`
	TargetTitle    string   `json:"target_title"`
	SourceTitles   []string `json:"source_titles,omitempty"`
	NewTitle       string   `json:"new_title,omitempty"`
	NewBody        string   `json:"new_body"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	Keywords       []string `json:"keywords"`
	UsageScenarios []string `json:"usage_scenarios,omitempty"`
}

func (a *App) dreamStatePath() string { return filepath.Join(a.LocalRoot, dreamStateFile) }
func (a *App) dreamLockPath() string  { return filepath.Join(a.LocalRoot, dreamLockFile) }

func (a *App) loadDreamState() DreamState {
	state := DreamState{Version: 1, LastStatus: "idle"}
	data, err := os.ReadFile(a.dreamStatePath())
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return DreamState{Version: 1, LastStatus: "idle"}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.LastStatus == "" {
		state.LastStatus = "idle"
	}
	return state
}

func (a *App) saveDreamState(state DreamState) error {
	state.Version = 1
	if err := os.MkdirAll(a.LocalRoot, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.dreamStatePath(), append(data, '\n'), 0o644)
}

// bumpDirty records one mutation for the next Dream.
func (a *App) bumpDirty() { a.bumpDirtyBy(1) }

// bumpDirtyBy records n mutations. The native import counts the entries it
// added or updated, so the dirty count and the number of memories agree.
// It is best-effort: a state-file failure must not lose the memory the
// caller just wrote.
func (a *App) bumpDirtyBy(n int) {
	if n <= 0 {
		return
	}
	state := a.loadDreamState()
	state.DirtyMutations += n
	if err := a.saveDreamState(state); err != nil && a.Err != nil {
		fmt.Fprintln(a.Err, "warning: cannot update dream state: "+err.Error())
	}
}

type consolidationLock struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

func (a *App) acquireDreamLock() (func(), error) {
	if err := os.MkdirAll(a.LocalRoot, 0o755); err != nil {
		return nil, err
	}
	path := a.dreamLockPath()
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			data, _ := json.Marshal(consolidationLock{PID: os.Getpid(), StartedAt: time.Now()})
			_, _ = file.Write(append(data, '\n'))
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		data, _ := os.ReadFile(path)
		var lock consolidationLock
		if json.Unmarshal(data, &lock) != nil || time.Since(lock.StartedAt) > 15*time.Minute {
			_ = os.Remove(path)
			continue
		}
		return nil, fmt.Errorf("consolidation is already running")
	}
	return nil, fmt.Errorf("consolidation is already running")
}

// Dream runs the deterministic pass and, when configured, the LLM pass.
func (a *App) Dream(ctx context.Context, opts DreamOptions) (report DreamReport, err error) {
	report.DryRun = opts.DryRun
	state := a.loadDreamState()
	if opts.MinDirty > 0 && state.DirtyMutations < opts.MinDirty {
		return report, nil
	}
	release, err := a.acquireDreamLock()
	if err != nil {
		return report, err
	}
	defer release()

	// A dry run reports; it never writes a memory or the state file.
	if !opts.DryRun {
		state.LastStatus = "running"
		if saveErr := a.saveDreamState(state); saveErr != nil {
			return report, saveErr
		}
		defer func() {
			if err != nil {
				state.LastStatus = "failed"
				_ = a.saveDreamState(state)
			}
		}()
	}

	original, err := a.Vault.Entries(ctx)
	if err != nil {
		return report, err
	}
	merged, deleted, survivors, err := a.dreamDedupe(ctx, original, opts.DryRun)
	if err != nil {
		return report, err
	}
	report.Merged = merged
	report.Deleted = deleted
	backfilled, err := a.dreamBackfill(ctx, survivors, opts.DryRun)
	if err != nil {
		return report, err
	}
	report.Backfilled = backfilled

	if a.LLM != nil && !opts.DeterministicOnly {
		operations, planErr := a.dreamPlan(ctx, survivors)
		if planErr != nil {
			return report, planErr
		}
		updated, merged2, deleted2, applyErr := a.dreamApply(ctx, original, operations, opts.DryRun)
		if applyErr != nil {
			return report, applyErr
		}
		report.Updated += updated
		report.Merged += merged2
		report.Deleted += deleted2
	}

	if opts.DryRun {
		return report, nil
	}
	if _, err := a.index(ctx, indexOptions{skipNativeImport: true}); err != nil {
		return report, err
	}
	state.DirtyMutations = 0
	state.LastDreamAt = time.Now()
	if report.Merged+report.Updated+report.Deleted+report.Backfilled == 0 {
		state.LastStatus = "noop"
	} else {
		state.LastStatus = "updated"
	}
	if err := a.saveDreamState(state); err != nil {
		return report, err
	}
	line := fmt.Sprintf("dream | merged=%d updated=%d deleted=%d backfilled=%d", report.Merged, report.Updated, report.Deleted, report.Backfilled)
	if err := a.Vault.AppendLog(ctx, line); err != nil {
		return report, err
	}
	return report, nil
}

func (a *App) dreamDedupe(ctx context.Context, corpus []memory.Entry, dry bool) (int, int, []memory.Entry, error) {
	active := append([]memory.Entry(nil), corpus...)
	removed := map[int]bool{}
	merged, deleted := 0, 0
	remove := func(index int) error {
		if removed[index] {
			return nil
		}
		removed[index] = true
		deleted++
		if dry {
			return nil
		}
		remover, ok := a.Vault.(vault.Remover)
		if !ok {
			return fmt.Errorf("the %s gateway cannot remove entries", a.Vault.Kind())
		}
		return remover.Remove(ctx, active[index])
	}
	merge := func(survivor int, others []int) error {
		for _, other := range others {
			active[survivor].Keywords = mergeUnique(active[survivor].Keywords, active[other].Keywords)
			active[survivor].UsageScenarios = mergeUnique(active[survivor].UsageScenarios, active[other].UsageScenarios)
			merged++
			if err := remove(other); err != nil {
				return err
			}
		}
		if !dry {
			return a.Vault.Update(ctx, active[survivor])
		}
		return nil
	}

	for {
		groups := map[string][]int{}
		for i := range active {
			if removed[i] || protectedForDream(active[i]) {
				continue
			}
			key := string(active[i].Type) + "\x00" + active[i].Project + "\x00" + memory.NormTitle(active[i].Title)
			groups[key] = append(groups[key], i)
		}
		changed := false
		for _, group := range groups {
			if len(group) < 2 {
				continue
			}
			survivor := newestEntry(active, group)
			var others []int
			for _, index := range group {
				if index != survivor {
					others = append(others, index)
				}
			}
			if err := merge(survivor, others); err != nil {
				return merged, deleted, nil, err
			}
			changed = true
		}
		if !changed {
			break
		}
	}

	for {
		groups := map[string][]int{}
		for i := range active {
			if removed[i] || protectedForDream(active[i]) {
				continue
			}
			key := string(active[i].Type) + "\x00" + active[i].Project + "\x00" + memory.NormBody(active[i].Body)
			groups[key] = append(groups[key], i)
		}
		changed := false
		for _, group := range groups {
			if len(group) < 2 {
				continue
			}
			survivor := newestEntry(active, group)
			var others []int
			for _, index := range group {
				if index != survivor {
					others = append(others, index)
				}
			}
			if err := merge(survivor, others); err != nil {
				return merged, deleted, nil, err
			}
			changed = true
		}
		if !changed {
			break
		}
	}
	return merged, deleted, dreamSurvivors(active, removed), nil
}

// dreamSurvivors returns the entries the run keeps: the active set without
// the indices that were merged away. A dry run uses it to report the same
// counts a real run would produce.
func dreamSurvivors(active []memory.Entry, removed map[int]bool) []memory.Entry {
	out := make([]memory.Entry, 0, len(active))
	for i := range active {
		if !removed[i] {
			out = append(out, active[i])
		}
	}
	return out
}

func protectedForDream(e memory.Entry) bool {
	return e.Pinned || e.EffectiveTier() == memory.TierHot
}

func newestEntry(entries []memory.Entry, group []int) int {
	survivor := group[0]
	for _, index := range group[1:] {
		if entries[index].ModTime.After(entries[survivor].ModTime) {
			survivor = index
			continue
		}
		if entries[index].ModTime.Equal(entries[survivor].ModTime) && entries[index].Date > entries[survivor].Date {
			survivor = index
		}
	}
	return survivor
}

func (a *App) dreamBackfill(ctx context.Context, entries []memory.Entry, dry bool) (int, error) {
	backfilled := 0
	for i := range entries {
		entry := entries[i]
		if protectedForDream(entry) {
			continue
		}
		changed := false
		if entry.Category == "" {
			entry.Category = memory.InferCategory(entry)
			changed = true
		}
		if entry.Description == "" {
			entry.Description = dreamDescription(entry)
			changed = true
		}
		if !changed {
			continue
		}
		// The caller keeps the set for the LLM stage, so the new
		// metadata is visible to it in a dry run and in a real one.
		entries[i] = entry
		if !dry {
			if err := a.Vault.Update(ctx, entry); err != nil {
				return backfilled, err
			}
		}
		backfilled++
	}
	return backfilled, nil
}

func dreamDescription(e memory.Entry) string {
	for _, line := range strings.Split(e.Body, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#-* "))
		if line == "" {
			continue
		}
		if len(line) > 120 {
			line = line[:117]
			if space := strings.LastIndex(line, " "); space > 0 {
				line = line[:space]
			}
			line += "..."
		}
		return line
	}
	return e.Title
}

func (a *App) dreamPlan(ctx context.Context, entries []memory.Entry) ([]DreamOperation, error) {
	groups := map[string][]memory.Entry{}
	for _, entry := range entries {
		if protectedForDream(entry) {
			continue
		}
		key := string(entry.Type) + "\x00" + entry.Project
		groups[key] = append(groups[key], entry)
	}
	var operations []DreamOperation
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		manifest := make([]map[string]any, 0, len(group))
		for _, entry := range group {
			manifest = append(manifest, map[string]any{
				"title": entry.Title, "body": entry.Body, "description": entry.Description,
				"category": memory.InferCategory(entry), "keywords": entry.Keywords,
				"usage_scenarios": entry.UsageScenarios,
			})
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		var response struct {
			Operations []DreamOperation `json:"operations"`
		}
		if err := a.LLM.CompleteJSON(ctx, llm.Request{
			SystemPrompt: "Consolidate durable memories. Return JSON {\"operations\":[...]} with action merge or update. Never touch pinned or hot memories. Every new_body must be non-empty English without credentials.",
			UserPrompt:   "Memory group:\n" + string(data),
			Temperature:  0,
		}, &response); err != nil {
			return nil, err
		}
		operations = append(operations, response.Operations...)
	}
	return operations, nil
}

func (a *App) dreamApply(ctx context.Context, original []memory.Entry, operations []DreamOperation, dry bool) (int, int, int, error) {
	originalByTitle := map[string]memory.Entry{}
	for _, entry := range original {
		originalByTitle[memory.NormTitle(entry.Title)] = entry
	}
	current, err := a.Vault.Entries(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	currentByTitle := map[string]memory.Entry{}
	for _, entry := range current {
		currentByTitle[memory.NormTitle(entry.Title)] = entry
	}
	updated, merged, deleted := 0, 0, 0
	for _, op := range operations {
		targetKey := memory.NormTitle(op.TargetTitle)
		target, targetOK := currentByTitle[targetKey]
		originalTarget, originalOK := originalByTitle[targetKey]
		if !targetOK || !originalOK || protectedForDream(originalTarget) || protectedForDream(target) {
			continue
		}
		sources := make([]memory.Entry, 0, len(op.SourceTitles))
		if op.Action == "merge" {
			valid := true
			for _, title := range op.SourceTitles {
				key := memory.NormTitle(title)
				source, ok := currentByTitle[key]
				originalSource, originalSourceOK := originalByTitle[key]
				if !ok || !originalSourceOK || key == targetKey || protectedForDream(source) || protectedForDream(originalSource) {
					valid = false
					break
				}
				sources = append(sources, source)
			}
			if !valid {
				continue
			}
		} else if op.Action != "update" {
			continue
		}
		if strings.TrimSpace(op.NewBody) == "" || !memory.IsEnglishStrict(op.NewBody) {
			continue
		}
		if err := secretguard.Check(op.NewTitle, op.NewBody); err != nil {
			continue
		}
		title := strings.TrimSpace(op.NewTitle)
		if title == "" {
			title = target.Title
		}
		if newKey := memory.NormTitle(title); newKey != targetKey {
			if _, exists := currentByTitle[newKey]; exists {
				continue
			}
			if _, exists := originalByTitle[newKey]; exists {
				continue
			}
		}
		target.Title = title
		target.Body = strings.TrimSpace(op.NewBody)
		if op.Description != "" {
			target.Description = op.Description
		}
		if op.Category != "" {
			if !memory.ValidCategory(op.Category) {
				continue
			}
			target.Category = op.Category
		}
		if len(op.Keywords) > 0 {
			target.Keywords = mergeUnique(target.Keywords, op.Keywords)
		}
		if len(op.UsageScenarios) > 0 {
			target.UsageScenarios = mergeUnique(target.UsageScenarios, op.UsageScenarios)
		}
		if !dry {
			if err := a.Vault.Update(ctx, target); err != nil {
				return updated, merged, deleted, err
			}
			if op.Action == "merge" {
				remover, ok := a.Vault.(vault.Remover)
				if !ok {
					return updated, merged, deleted, fmt.Errorf("the %s gateway cannot remove entries", a.Vault.Kind())
				}
				for _, source := range sources {
					if err := remover.Remove(ctx, source); err != nil {
						return updated, merged, deleted, err
					}
					deleted++
				}
			}
		} else if op.Action == "merge" {
			deleted += len(sources)
		}
		if op.Action == "merge" {
			merged++
			for _, source := range sources {
				delete(currentByTitle, memory.NormTitle(source.Title))
			}
		} else {
			updated++
		}
		delete(currentByTitle, targetKey)
		currentByTitle[memory.NormTitle(title)] = target
	}
	return updated, merged, deleted, nil
}
