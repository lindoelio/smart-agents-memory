package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckReadsTheLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(`{"statistic":{"coverage":{"stmts":"91.0","lines":"92.5","branches":"83.0"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := check(path, 90)
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
	if _, err := check(path, 90); err == nil {
		t.Fatal("bad JSON was accepted")
	}
	if _, err := check(filepath.Join(t.TempDir(), "missing"), 90); err == nil {
		t.Fatal("a missing report was accepted")
	}
}

func TestCheckRejectsANonNumericLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(`{"statistic":{"coverage":{"lines":"n/a"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := check(path, 90); err == nil {
		t.Fatal("a non-numeric line was accepted")
	}
}
