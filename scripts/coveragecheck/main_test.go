package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReadsTheLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(`{"statistic":{"coverage":{"stmts":"91.0","lines":"92.5","branches":"83.0"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := check(path)
	if err != nil {
		t.Fatal(err)
	}
	if value != 92.5 {
		t.Fatalf("the value is %v", value)
	}
}

func TestCheckRejectsBadJSONAndMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := check(path); err == nil {
		t.Fatal("bad JSON was accepted")
	}
	if _, err := check(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing report was accepted")
	}
}

func TestCheckRejectsANonNumericLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(`{"statistic":{"coverage":{"lines":"n/a"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := check(path); err == nil {
		t.Fatal("a non-numeric line was accepted")
	}
}

func TestRunPassesFailsAndValidates(t *testing.T) {
	dir := t.TempDir()
	pass := filepath.Join(dir, "pass.json")
	if err := os.WriteFile(pass, []byte(`{"statistic":{"coverage":{"lines":"92.5"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fail := filepath.Join(dir, "fail.json")
	if err := os.WriteFile(fail, []byte(`{"statistic":{"coverage":{"lines":"80"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-file", pass, "-min", "90"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("pass returned %d", code)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-file", fail, "-min", "90"}, &out, &errOut); code != 1 {
		t.Fatalf("fail returned %d", code)
	}
	if !strings.Contains(out.String(), "lines:") || errOut.Len() != 0 {
		t.Fatalf("fail output %q %q", out.String(), errOut.String())
	}
	if code := run(nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "-file") {
		t.Fatalf("missing file returned %d %q", code, errOut.String())
	}
	if code := run([]string{"-unknown"}, &out, &errOut); code != 2 {
		t.Fatalf("bad flag returned %d", code)
	}
	if code := run([]string{"-file", filepath.Join(dir, "missing")}, &out, &errOut); code != 2 {
		t.Fatalf("missing report returned %d", code)
	}
}
