package sources

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTripAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync-state.json")
	state := NewState()
	state.Seeded = true
	state.Files["cursor:/a"] = FileState{Size: 10, Offset: 10, MtimeNS: 1}
	state.Session["opencode:s"] = SessionState{LastSeq: 4}
	state.Seen["old"] = time.Now().Add(-10 * 24 * time.Hour)
	state.Seen["new"] = time.Now()
	state.Sources["cursor"] = SourceState{Files: 1, Errors: []string{"x"}}
	if err := SaveState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Seeded || loaded.Files["cursor:/a"].Size != 10 || loaded.Session["opencode:s"].LastSeq != 4 {
		t.Fatalf("the state is %+v", loaded)
	}
	loaded.PruneSeen(time.Now(), 7*24*time.Hour)
	if _, ok := loaded.Seen["old"]; ok {
		t.Fatal("the old hash survived")
	}
	if _, ok := loaded.Seen["new"]; !ok {
		t.Fatal("the new hash was pruned")
	}
}

func TestLoadStateMissingAndCorrupt(t *testing.T) {
	state, err := LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || state == nil || state.Files == nil {
		t.Fatalf("missing state: %+v %v", state, err)
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("a corrupt state was accepted")
	}
}

func TestSaveStateUsesPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync-state.json")
	if err := SaveState(path, NewState()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the mode is %o", info.Mode().Perm())
	}
}

func TestLockExcludesAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	first, err := AcquireLock(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(path, time.Minute); err == nil {
		t.Fatal("the second lock was acquired")
	}
	first.Release()
	second, err := AcquireLock(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the lock file survived release")
	}
}

func TestLockRecoversAStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	if err := os.WriteFile(path, []byte(`{"pid":1,"started_at":"2000-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireLock(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()
}

func TestFileStateIsFreshAndFileInfo(t *testing.T) {
	state := FileState{Size: 10, MtimeNS: 20, Offset: 10}
	if !state.IsFresh(FileInfoOf(10, 20)) {
		t.Fatal("an unchanged file is not fresh")
	}
	if state.IsFresh(FileInfoOf(11, 20)) {
		t.Fatal("a changed file is fresh")
	}
}

func TestLoadStateEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil || state == nil || state.Files == nil {
		t.Fatalf("empty state: %+v %v", state, err)
	}
}

func TestSaveStateFailsOnAFileParent(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(filepath.Join(blocker, "state.json"), NewState()); err == nil {
		t.Fatal("save under a file succeeded")
	}
}

func TestLockRejectsAMalformedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(path, time.Minute); err == nil {
		t.Fatal("a malformed lock was taken over")
	}
}

func TestLoadStateNormalizesAnEmptyObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil || state.Version != 1 || state.Files == nil || state.Seen == nil || state.Sources == nil {
		t.Fatalf("state: %+v %v", state, err)
	}
}

func TestLoadStateNormalizesZeroVersionAndNilMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":0,"files":null,"sessions":null,"seen":null,"sources":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil || state.Version != 1 || state.Files == nil || state.Session == nil || state.Seen == nil || state.Sources == nil {
		t.Fatalf("state: %+v %v", state, err)
	}
	state.PruneSeen(time.Now(), 0)
}
