package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lindoelio/smart-agents-memory/internal/sources"
)

func TestReadFromOnlyCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte("a\nb\npartial"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, end, err := readFrom(path, 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nb\n" || end != 4 {
		t.Fatalf("readFrom returned %q end=%d", data, end)
	}
	data, end, err = readFrom(path, end, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 || end != 4 {
		t.Fatalf("the partial line was consumed: %q %d", data, end)
	}
}

func TestReadFromMissingFile(t *testing.T) {
	if _, _, err := readFrom(filepath.Join(t.TempDir(), "nope"), 0, 10); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestReadFromZeroChunk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	data, end, err := readFrom(path, 0, 10)
	if err != nil || len(data) != 0 || end != 0 {
		t.Fatalf("readFrom returned %q %d %v", data, end, err)
	}
}

func TestResetForBackfill(t *testing.T) {
	state := sources.NewState()
	state.Files["x"] = sources.FileState{Offset: 10}
	state.Session["s"] = sources.SessionState{LastSeq: 3}
	resetForBackfill(state)
	if state.Files["x"].Offset != 0 || state.Session["s"].LastSeq != 0 {
		t.Fatalf("the state was not reset: %+v", state)
	}
}

func TestConfigureSchedulerWritesAndRemoves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := &App{}
	result, err := a.ConfigureScheduler(SchedulerOptions{Interval: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Paths) == 0 || result.DryRun {
		t.Fatalf("the result is %+v", result)
	}
	for _, path := range result.Paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the scheduler file %s: %v", path, err)
		}
	}
	removed, err := a.ConfigureScheduler(SchedulerOptions{Remove: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range removed.Paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the scheduler file %s survived", path)
		}
	}
}

func TestConfigureSchedulerDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a := &App{}
	result, err := a.ConfigureScheduler(SchedulerOptions{Interval: time.Minute, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Content == "" {
		t.Fatalf("the dry run is %+v", result)
	}
	for _, path := range result.Paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the dry run wrote %s", path)
		}
	}
}

func TestSchedulerContent(t *testing.T) {
	plist := launchdPlist("/tmp/smart-agents-memory", "/tmp/sync.log", time.Minute)
	if !strings.Contains(plist, "com.smart-agents-memory.sync") || !strings.Contains(plist, "<integer>60</integer>") {
		t.Fatalf("the plist is %s", plist)
	}
	service, timer := systemdUnits("/tmp/smart-agents-memory", time.Minute)
	if !strings.Contains(service, "ExecStart=/tmp/smart-agents-memory sync --quiet") {
		t.Fatalf("the service is %s", service)
	}
	if !strings.Contains(timer, "OnUnitActiveSec=1m0s") {
		t.Fatalf("the timer is %s", timer)
	}
	if runtime.GOOS == "windows" {
		t.Skip("no scheduler on Windows")
	}
}
