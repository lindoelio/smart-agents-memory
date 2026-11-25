package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/extract"
	"github.com/lindoelio/smart-agents-memory/internal/sources"
)

const (
	syncStateFile = ".sync-state.json"
	syncLockFile  = ".sync.lock"
	syncSeenTTL   = 7 * 24 * time.Hour
	syncStale     = 15 * time.Minute
)

// SyncOptions selects one capture run.
type SyncOptions struct {
	DryRun    bool
	Backfill  bool
	Since     time.Duration
	SourceID  string
	MaxBytes  int64
	CWD       string
	AutoDream bool
}

// SyncReport is the outcome of one capture run.
type SyncReport struct {
	Seeded     bool     `json:"seeded"`
	Sources    int      `json:"sources"`
	Files      int      `json:"files"`
	Changed    int      `json:"changed"`
	Turns      int      `json:"turns"`
	Candidates int      `json:"candidates"`
	Accepted   int      `json:"accepted"`
	Added      int      `json:"added"`
	Skipped    int      `json:"skipped"`
	Ignored    int      `json:"ignored"`
	Redacted   int      `json:"redacted,omitempty"`
	Migrated   int      `json:"migrated,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

func (a *App) syncStatePath() string { return filepath.Join(a.LocalRoot, syncStateFile) }
func (a *App) syncLockPath() string  { return filepath.Join(a.LocalRoot, syncLockFile) }

// Sync reads the transcript artifacts that changed since the last run and
// extracts durable memories from them.
//
// The first run seeds the state at the end of every file and extracts
// nothing. Backfill is explicit: it is the only way to process history.
func (a *App) Sync(ctx context.Context, opts SyncOptions) (SyncReport, error) {
	report := SyncReport{}
	if a == nil || a.Vault == nil {
		return report, fmt.Errorf("sync needs a store")
	}
	if err := a.Vault.Ready(ctx); err != nil {
		return report, err
	}
	legacy := a.legacyCursors()
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = a.Cfg.SyncMaxBytes
	}
	if maxBytes <= 0 {
		maxBytes = sources.DefaultMaxBytes
	}
	reg := sources.Build(a.Cfg.SyncRoots, a.Cfg.SyncDisabled, maxBytes, a.Cfg.WatchDirs)
	allSources := reg.Enabled()
	if opts.SourceID != "" {
		source, ok := reg.ByID(opts.SourceID)
		if !ok {
			return report, fmt.Errorf("unknown source %q", opts.SourceID)
		}
		reg.Sources = []sources.Source{source}
	}
	enabled := reg.Enabled()
	report.Sources = len(enabled)

	state, err := sources.LoadState(a.syncStatePath())
	if err != nil {
		return report, err
	}
	var release func()
	if !opts.DryRun {
		lock, err := sources.AcquireLock(a.syncLockPath(), syncStale)
		if err != nil {
			return report, err
		}
		release = lock.Release
	}
	if release != nil {
		defer release()
	}
	now := time.Now()
	state.PruneSeen(now, syncSeenTTL)
	if opts.Backfill {
		resetForBackfill(state)
	}

	for _, source := range enabled {
		beforeFiles := report.Files
		sourceSaved := state.Sources[source.ID]
		if state.Seeded && !sourceSaved.Seeded {
			// A state written before per-source seeding already covered the
			// sources that existed then. New sources are seeded from here.
			sourceSaved.Seeded = true
		}
		seeding := !opts.Backfill && !sourceSaved.Seeded && !state.Seeded
		report.Seeded = report.Seeded || seeding
		reportSource := sources.SourceState{LastSync: now, Seeded: sourceSaved.Seeded}
		var sourceErr error
		switch source.Kind {
		case sources.KindSQLite:
			sourceErr = a.syncSQLite(ctx, source, state, opts, seeding, &report)
		default:
			sourceErr = a.syncFiles(ctx, source, state, opts, seeding, legacy, &report)
		}
		if sourceErr != nil {
			reportSource.Errors = append(reportSource.Errors, sourceErr.Error())
			report.Errors = append(report.Errors, source.ID+": "+sourceErr.Error())
		}
		if seeding || opts.Backfill {
			reportSource.Seeded = true
		}
		reportSource.Files = report.Files - beforeFiles
		state.Sources[source.ID] = reportSource
	}

	allSeeded := true
	for _, source := range allSources {
		if !state.Sources[source.ID].Seeded {
			allSeeded = false
			break
		}
	}
	if opts.Backfill && !opts.DryRun && len(report.Errors) == 0 {
		state.Backfilled = true
	}
	if !opts.DryRun {
		state.Seeded = allSeeded
		if err := sources.SaveState(a.syncStatePath(), state); err != nil {
			return report, err
		}
	}
	if opts.AutoDream && !opts.DryRun && len(report.Errors) == 0 {
		threshold := defaultDreamDirty
		dreamState := a.loadDreamState()
		if dreamState.DirtyMutations >= threshold {
			if _, err := a.Dream(ctx, DreamOptions{}); err != nil {
				report.Errors = append(report.Errors, "dream: "+err.Error())
			}
		}
	}
	return report, nil
}

func (a *App) legacyCursors() map[string]extract.SessionCursor {
	store, err := extract.Load(a.extractCursorPath())
	if err != nil {
		return nil
	}
	out := map[string]extract.SessionCursor{}
	for id, cursor := range store.Sessions {
		path := cursor.TranscriptPath
		if path == "" {
			path = id
		}
		if abs, err := filepath.Abs(path); err == nil {
			out[abs] = cursor
		}
	}
	return out
}

func absolutePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func resetForBackfill(state *sources.SyncState) {
	for key, file := range state.Files {
		file.Offset = 0
		state.Files[key] = file
	}
	for key, session := range state.Session {
		session.LastSeq = 0
		state.Session[key] = session
	}
}

func (a *App) syncFiles(ctx context.Context, source sources.Source, state *sources.SyncState, opts SyncOptions, seeding bool, legacy map[string]extract.SessionCursor, report *SyncReport) error {
	for _, candidate := range sources.Discover(sources.Registry{Sources: []sources.Source{source}}) {
		report.Files++
		info, err := os.Stat(candidate.Path)
		if err != nil {
			report.Errors = append(report.Errors, candidate.Path+": "+err.Error())
			continue
		}
		key := source.ID + ":" + candidate.Path
		previous := state.Files[key]
		if seeding {
			offset := info.Size()
			if cursor, ok := legacy[absolutePath(candidate.Path)]; ok && cursor.ProcessedOffset > 0 {
				// The old watch already extracted this file. Seed it at the
				// end instead of sending its history again.
				report.Migrated++
				offset = info.Size()
			}
			state.Files[key] = sources.FileState{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), Offset: offset, Last: time.Now()}
			continue
		}
		if previous.Size == info.Size() && previous.MtimeNS == info.ModTime().UnixNano() && previous.Offset >= info.Size() {
			continue
		}
		if opts.Since > 0 && time.Since(info.ModTime()) > opts.Since {
			// Seeded history stays skipped; backfill can still process it.
			state.Files[key] = sources.FileState{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), Offset: info.Size(), Last: time.Now()}
			continue
		}
		offset := previous.Offset
		if info.Size() < offset {
			offset = 0
		}
		data, end, err := readFrom(candidate.Path, offset, source.MaxBytes)
		if err != nil {
			report.Errors = append(report.Errors, candidate.Path+": "+err.Error())
			continue
		}
		if len(data) == 0 {
			state.Files[key] = sources.FileState{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), Offset: end, Last: time.Now()}
			continue
		}
		report.Changed++
		turns, err := sources.Parse(candidate.Source.Format, data)
		if err != nil {
			report.Errors = append(report.Errors, candidate.Path+": "+err.Error())
			continue
		}
		newTurns, changed := a.newTurns(source.ID, key, turns, state)
		report.Turns += len(newTurns)
		if !changed {
			report.Ignored++
			state.Files[key] = sources.FileState{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), Offset: end, Last: time.Now()}
			continue
		}
		if opts.DryRun {
			state.Files[key] = sources.FileState{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), Offset: end, Last: time.Now()}
			continue
		}
		result, err := a.ExtractTurns(ctx, newTurns, ExtractOptions{CWD: opts.CWD})
		if err != nil {
			report.Errors = append(report.Errors, candidate.Path+": "+err.Error())
			continue
		}
		report.Candidates += result.Candidates
		report.Accepted += result.Accepted
		report.Added += result.Added
		report.Skipped += len(result.Skipped)
		report.Redacted += result.Redacted
		state.Files[key] = sources.FileState{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), Offset: end, Last: time.Now()}
		markSeen(state, source.ID, key, newTurns)
		if !opts.DryRun {
			if err := sources.SaveState(a.syncStatePath(), state); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) syncSQLite(ctx context.Context, source sources.Source, state *sources.SyncState, opts SyncOptions, seeding bool, report *SyncReport) error {
	for _, root := range source.Roots {
		path := sources.ExpandPath(root)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		after := map[string]int64{}
		for key, session := range state.Session {
			if strings.HasPrefix(key, source.ID+":") {
				after[strings.TrimPrefix(key, source.ID+":")] = session.LastSeq
			}
		}
		messages, next, err := sources.ReadOpenCode(path, after)
		if err != nil {
			return err
		}
		if seeding {
			for session, seq := range next {
				state.Session[source.ID+":"+session] = sources.SessionState{LastSeq: seq, Last: time.Now()}
			}
			continue
		}
		grouped := sources.GroupBySession(messages)
		if len(grouped) == 0 {
			continue
		}
		report.Changed += len(grouped)
		for session, group := range grouped {
			report.Files++
			var turns []extract.Turn
			for _, message := range group {
				turns = append(turns, extract.Turn{Role: message.Role, Text: message.Text})
			}
			report.Turns += len(turns)
			if opts.DryRun {
				state.Session[source.ID+":"+session] = sources.SessionState{LastSeq: next[session], Last: time.Now()}
				continue
			}
			result, err := a.ExtractTurns(ctx, turns, ExtractOptions{CWD: opts.CWD})
			if err != nil {
				report.Errors = append(report.Errors, path+" "+session+": "+err.Error())
				continue
			}
			report.Candidates += result.Candidates
			report.Accepted += result.Accepted
			report.Added += result.Added
			report.Skipped += len(result.Skipped)
			report.Redacted += result.Redacted
			state.Session[source.ID+":"+session] = sources.SessionState{LastSeq: next[session], Last: time.Now()}
			markSeen(state, source.ID, source.ID+":"+session, turns)
			if err := sources.SaveState(a.syncStatePath(), state); err != nil {
				return err
			}
		}
	}
	return nil
}

// newTurns keeps only turns that were not delivered before.
func (a *App) newTurns(sourceID, key string, turns []extract.Turn, state *sources.SyncState) ([]extract.Turn, bool) {
	var out []extract.Turn
	hasUser := false
	for _, turn := range turns {
		hash := turnHash(sourceID, key, turn)
		if turn.Role == "user" {
			if _, ok := state.Seen[hash]; ok {
				continue
			}
			hasUser = true
		}
		out = append(out, turn)
	}
	if !hasUser {
		return nil, false
	}
	return out, true
}

func markSeen(state *sources.SyncState, sourceID, key string, turns []extract.Turn) {
	for _, turn := range turns {
		state.Seen[turnHash(sourceID, key, turn)] = time.Now()
	}
}

func turnHash(sourceID, key string, turn extract.Turn) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(sourceID+"\x00"+key+"\x00"+turn.Role+"\x00"+turn.Text)))
}

// readFrom reads the complete lines after offset. The returned end is the
// byte offset of the last complete line; a partial final line stays for the
// next run.
func readFrom(path string, offset, maxBytes int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if maxBytes <= 0 {
		maxBytes = sources.DefaultMaxBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	data := make([]byte, maxBytes)
	n, err := f.Read(data)
	if err != nil && err != io.EOF {
		return nil, offset, err
	}
	data = data[:n]
	if len(data) == 0 {
		return nil, offset, nil
	}
	last := -1
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			last = i
			break
		}
	}
	if last < 0 {
		return nil, offset, nil
	}
	return data[:last+1], offset + int64(last+1), nil
}

// SourceStatus is one line of the doctor matrix.
type SourceStatus struct {
	ID         string
	Kind       sources.Kind
	Enabled    bool
	Exists     bool
	Seeded     bool
	Backfilled bool
	LastSync   time.Time
	Errors     []string
}

// SourceStatuses reports the configured sources without reading transcripts.
func (a *App) SourceStatuses() []SourceStatus {
	reg := sources.Build(a.Cfg.SyncRoots, a.Cfg.SyncDisabled, a.Cfg.SyncMaxBytes, a.Cfg.WatchDirs)
	state, _ := sources.LoadState(a.syncStatePath())
	statuses := make([]SourceStatus, 0, len(reg.Sources))
	for _, source := range reg.Sources {
		status := SourceStatus{ID: source.ID, Kind: source.Kind, Enabled: source.Enabled}
		for _, root := range source.Roots {
			if _, err := os.Stat(sources.ExpandPath(root)); err == nil {
				status.Exists = true
				break
			}
		}
		if state != nil {
			status.Backfilled = state.Backfilled
			if state.Seeded {
				status.Seeded = true
			}
			if saved, ok := state.Sources[source.ID]; ok {
				status.LastSync = saved.LastSync
				status.Errors = saved.Errors
				status.Seeded = status.Seeded || saved.Seeded
			}
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}
