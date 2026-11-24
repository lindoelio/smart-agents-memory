package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/extract"
	"github.com/lindoelio/smart-agents-memory/internal/gitscope"
	"github.com/lindoelio/smart-agents-memory/internal/llm"
	"github.com/lindoelio/smart-agents-memory/internal/memory"
	"github.com/lindoelio/smart-agents-memory/internal/secretguard"
)

const (
	extractCursorFile  = ".extract-cursor.json"
	defaultDreamDirty  = 10
	extractSource      = "smart-agents-memory-extract"
	extractMaxManifest = 25000
)

// ExtractOptions selects one post-turn extraction.
type ExtractOptions struct {
	// Transcript is the raw stdin or hook text.
	Transcript string
	// TranscriptPath is a file to read when Transcript is empty.
	TranscriptPath string
	// SessionID keys the cursor. It defaults to the path or "default".
	SessionID string
	// OneShot treats the payload as a complete unit: the cursor is neither
	// read nor written, so every stdin payload is processed. It is what a
	// post-turn hook needs, because each call carries one finished turn.
	OneShot bool
	// CWD is where a project candidate resolves its scope.
	CWD string
	// DryRun reports without writing memories or moving the cursor.
	DryRun bool
	// AutoDream runs Dream when the dirty threshold is reached.
	AutoDream bool
	// DreamThreshold overrides the default of 10.
	DreamThreshold int
}

// ExtractReport is the outcome of one extraction.
type ExtractReport struct {
	Candidates    int
	Added         int
	Skipped       []string
	SkippedReason string
	Cursor        extract.SessionCursor
	Dream         *DreamReport
}

// candidateOutcome separates a rejected candidate from a failed write. A
// rejection is normal and does not stop the run; a failed write is an
// operational error, and the cursor must not move past it.
type candidateOutcome struct {
	Reason string
	Added  bool
	Err    error
}

// ExtractCandidate is one durable fact proposed by the model.
type ExtractCandidate struct {
	Scope          string   `json:"scope"`
	Title          string   `json:"title"`
	Body           string   `json:"body"`
	Description    string   `json:"description,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
	UsageScenarios []string `json:"usage_scenarios,omitempty"`
	Category       string   `json:"category,omitempty"`
	Project        string   `json:"project,omitempty"`
	Worktree       string   `json:"worktree,omitempty"`
}

func (a *App) extractCursorPath() string { return filepath.Join(a.LocalRoot, extractCursorFile) }

// saveExtractCursor persists the offset unless the run cannot own a cursor:
// a dry run writes nothing, and a one-shot payload has no session to resume.
func (a *App) saveExtractCursor(store *extract.CursorStore, sessionID, transcriptPath string, offset int, opts ExtractOptions) error {
	if opts.DryRun || opts.OneShot {
		return nil
	}
	extract.Advance(store, sessionID, transcriptPath, offset, time.Now())
	return extract.Save(a.extractCursorPath(), *store)
}

// Extract parses a transcript, asks the configured model for durable
// knowledge, and persists only candidates that pass every safety gate.
func (a *App) Extract(ctx context.Context, opts ExtractOptions) (ExtractReport, error) {
	report := ExtractReport{}
	content := opts.Transcript
	if content == "" && opts.TranscriptPath != "" {
		data, err := os.ReadFile(opts.TranscriptPath)
		if err != nil {
			return report, err
		}
		content = string(data)
	}
	if strings.TrimSpace(content) == "" {
		return report, fmt.Errorf("empty transcript")
	}
	sessionID := strings.TrimSpace(opts.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(opts.TranscriptPath)
	}
	if sessionID == "" {
		sessionID = "default"
	}
	turns, err := extract.Parse([]byte(content))
	if err != nil {
		return report, err
	}
	store := extract.CursorStore{Sessions: map[string]extract.SessionCursor{}}
	start := 0
	if !opts.OneShot {
		store, err = extract.Load(a.extractCursorPath())
		if err != nil {
			return report, err
		}
		start = extract.StartOffset(store, sessionID, turns)
	}
	if !extract.HasNewUserTurn(turns, start) {
		report.SkippedReason = "no_new_user_turns"
		report.Cursor = extract.SessionCursor{
			SessionID: sessionID, TranscriptPath: opts.TranscriptPath,
			ProcessedOffset: len(turns), UpdatedAt: time.Now(),
		}
		if err := a.saveExtractCursor(&store, sessionID, opts.TranscriptPath, len(turns), opts); err != nil {
			return report, err
		}
		return report, nil
	}
	if a.LLM == nil {
		return report, fmt.Errorf("extraction needs an LLM client")
	}

	manifest := a.extractManifest(ctx)
	request := a.extractPrompt(turns[start:], manifest)
	var response struct {
		Candidates []ExtractCandidate `json:"candidates"`
	}
	if err := a.LLM.CompleteJSON(ctx, request, &response); err != nil {
		return report, err
	}

	existing := a.extractExisting(ctx)
	for _, candidate := range response.Candidates {
		report.Candidates++
		outcome := a.extractCandidate(ctx, candidate, existing, opts)
		if outcome.Err != nil {
			// A failed write is not a rejection: the candidate is still
			// owed, so the cursor stays where it was for the next run.
			return report, fmt.Errorf("cannot persist %q: %w", candidate.Title, outcome.Err)
		}
		if outcome.Reason != "" {
			report.Skipped = append(report.Skipped, candidate.Title+": "+outcome.Reason)
			continue
		}
		if outcome.Added {
			report.Added++
		}
	}

	report.Cursor = extract.SessionCursor{
		SessionID: sessionID, TranscriptPath: opts.TranscriptPath,
		ProcessedOffset: len(turns), UpdatedAt: time.Now(),
	}
	if err := a.saveExtractCursor(&store, sessionID, opts.TranscriptPath, len(turns), opts); err != nil {
		return report, err
	}

	if opts.AutoDream && !opts.DryRun {
		threshold := opts.DreamThreshold
		if threshold <= 0 {
			threshold = defaultDreamDirty
		}
		state := a.loadDreamState()
		if state.DirtyMutations >= threshold {
			dream, err := a.Dream(ctx, DreamOptions{})
			if err != nil {
				return report, err
			}
			report.Dream = &dream
		}
	}
	return report, nil
}

func (a *App) extractManifest(ctx context.Context) string {
	var entries []memory.Entry
	if a.Vault != nil && a.Vault.Ready(ctx) == nil {
		entries, _ = a.Vault.Entries(ctx)
	}
	var b strings.Builder
	for _, entry := range entries {
		if strings.TrimSpace(entry.Title) == "" {
			continue
		}
		line := "- " + entry.Title
		if entry.Description != "" {
			line += ": " + entry.Description
		}
		if b.Len()+len(line)+1 > extractMaxManifest {
			break
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

func (a *App) extractPrompt(turns []extract.Turn, manifest string) llm.Request {
	system := "Extract only durable knowledge from an agent conversation. " +
		"Return JSON {\"candidates\":[...]} where each candidate has scope (user, lesson, project, reference), title, body, description, keywords, usage_scenarios, category, project and worktree. " +
		"Every body must contain the rule/fact and its why. Write in English. " +
		"Do not extract task status, file listings, diffs, transient state, or anything already covered by the manifest. " +
		"Do not invent credentials or reference secret values."
	user := "Existing memory:\n" + manifest + "\n\nConversation:\n" + extract.FormatTurns(turns)
	return llm.Request{SystemPrompt: system, UserPrompt: user, Temperature: 0}
}

func (a *App) extractExisting(ctx context.Context) map[string]memory.Entry {
	out := map[string]memory.Entry{}
	if a.Vault != nil && a.Vault.Ready(ctx) == nil {
		entries, _ := a.Vault.Entries(ctx)
		for _, entry := range entries {
			out[memory.NormTitle(entry.Title)] = entry
		}
	}
	return out
}

// extractCandidate applies every safety gate. A non-empty Reason means the
// candidate was rejected; Err means the write itself failed and the caller
// must stop before moving the cursor.
func (a *App) extractCandidate(ctx context.Context, candidate ExtractCandidate, existing map[string]memory.Entry, opts ExtractOptions) candidateOutcome {
	scope := memory.Type(strings.TrimSpace(candidate.Scope))
	if !scope.Valid() {
		return candidateOutcome{Reason: "invalid scope"}
	}
	title := strings.TrimSpace(candidate.Title)
	body := strings.TrimSpace(candidate.Body)
	if title == "" || body == "" {
		return candidateOutcome{Reason: "empty title or body"}
	}
	if !memory.IsEnglish(body) {
		return candidateOutcome{Reason: "body is not English"}
	}
	if err := secretguard.Check(title, body); err != nil {
		return candidateOutcome{Reason: "secret detected"}
	}
	if current, ok := existing[memory.NormTitle(title)]; ok {
		if current.Pinned || current.EffectiveTier() == memory.TierHot {
			return candidateOutcome{Reason: "pinned or hot entry exists"}
		}
	}
	project := candidate.Project
	worktree := candidate.Worktree
	if scope == memory.TypeProject && project == "" {
		cwd := opts.CWD
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		if info, ok := gitscope.Resolve(cwd); ok {
			project = info.RepoSlug
			if worktree == "" {
				worktree = info.WorktreeID
			}
		}
	}
	category := candidate.Category
	if category != "" && !memory.ValidCategory(category) {
		category = ""
	}
	if opts.DryRun {
		return candidateOutcome{Added: true}
	}
	message, err := a.Add(ctx, AddOptions{
		Scope: string(scope), Title: title, Body: body, Source: extractSource,
		Tier: string(memory.TierCold), Pinned: false, Project: project, Worktree: worktree,
		Description: candidate.Description, Tags: candidate.Keywords,
		Category: category, UsageScenarios: candidate.UsageScenarios,
		CWD: opts.CWD, AllowOtherLanguage: true,
	})
	if err != nil {
		return candidateOutcome{Err: err}
	}
	return candidateOutcome{
		Added: strings.HasPrefix(message, "recorded") || strings.HasPrefix(message, "appended"),
	}
}
