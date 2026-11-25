package sources

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileState is how far one transcript file has been processed.
type FileState struct {
	Size    int64     `json:"size"`
	MtimeNS int64     `json:"mtime_ns"`
	Offset  int64     `json:"offset"`
	Hash    string    `json:"hash,omitempty"`
	Last    time.Time `json:"last,omitempty"`
}

// SessionState is the cursor of one SQLite session.
type SessionState struct {
	LastSeq int64     `json:"last_seq"`
	Last    time.Time `json:"last,omitempty"`
}

// SourceState is the health summary of one source.
type SourceState struct {
	LastSync time.Time `json:"last_sync,omitempty"`
	Files    int       `json:"files,omitempty"`
	Errors   []string  `json:"errors,omitempty"`
}

// SyncState is the content of .sync-state.json.
type SyncState struct {
	Version int                     `json:"version"`
	Seeded  bool                    `json:"seeded"`
	Files   map[string]FileState    `json:"files"`
	Session map[string]SessionState `json:"sessions"`
	Seen    map[string]time.Time    `json:"seen,omitempty"`
	Sources map[string]SourceState  `json:"sources,omitempty"`
}

// NewState returns an empty state.
func NewState() *SyncState {
	return &SyncState{
		Version: 1,
		Files:   map[string]FileState{},
		Session: map[string]SessionState{},
		Seen:    map[string]time.Time{},
		Sources: map[string]SourceState{},
	}
}

// LoadState reads the state file. A missing file is an empty state.
func LoadState(path string) (*SyncState, error) {
	state := NewState()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return NewState(), fmt.Errorf("cannot read %s: %w", path, err)
	}
	normalizeState(state)
	return state, nil
}

func normalizeState(state *SyncState) {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Files == nil {
		state.Files = map[string]FileState{}
	}
	if state.Session == nil {
		state.Session = map[string]SessionState{}
	}
	if state.Seen == nil {
		state.Seen = map[string]time.Time{}
	}
	if state.Sources == nil {
		state.Sources = map[string]SourceState{}
	}
}

// SaveState writes the state atomically with mode 0600.
func SaveState(path string, state *SyncState) error {
	normalizeState(state)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sync-state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// PruneSeen removes dedupe hashes older than ttl.
func (s *SyncState) PruneSeen(now time.Time, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	for key, at := range s.Seen {
		if now.Sub(at) > ttl {
			delete(s.Seen, key)
		}
	}
}

// IsFresh reports whether a file has not changed since the state.
func (s FileState) IsFresh(info FileInfo) bool {
	return s.Size == info.Size && s.MtimeNS == info.MtimeNS
}

// FileInfo is the stat information the state needs.
type FileInfo struct {
	Size    int64
	MtimeNS int64
}

// FileInfoOf builds FileInfo from an os.FileInfo.
func FileInfoOf(size, mtime int64) FileInfo {
	return FileInfo{Size: size, MtimeNS: mtime}
}

// Lock is the exclusive sync lock.
type Lock struct {
	path string
	held bool
}

type lockRecord struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// AcquireLock creates an exclusive lock. A lock older than stale is removed.
func AcquireLock(path string, stale time.Duration) (*Lock, error) {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			data, _ := json.Marshal(lockRecord{PID: os.Getpid(), StartedAt: time.Now()})
			_, _ = f.Write(data)
			_ = f.Close()
			return &Lock{path: path, held: true}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		data, readErr := os.ReadFile(path)
		var record lockRecord
		if readErr == nil && json.Unmarshal(data, &record) == nil && time.Since(record.StartedAt) > stale {
			_ = os.Remove(path)
			continue
		}
		return nil, errors.New("sync is already running")
	}
	return nil, errors.New("sync is already running")
}

// Release removes the lock.
func (l *Lock) Release() {
	if l != nil && l.held {
		_ = os.Remove(l.path)
		l.held = false
	}
}
