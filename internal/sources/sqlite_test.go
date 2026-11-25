package sources

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openCodeDB(t *testing.T, rows [][4]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE session_message (
		session_id TEXT, seq INTEGER, type TEXT, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO session_message VALUES (?,?,?,?)`, row[0], row[1], row[2], row[3]); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestReadOpenCodeReturnsMessagesAndCursor(t *testing.T) {
	path := openCodeDB(t, [][4]string{
		{"s1", "1", "user", `{"text":"remember this"}`},
		{"s1", "2", "assistant", `{"text":"noted"}`},
		{"s1", "3", "tool", `{"text":"ignore"}`},
		{"s1", "4", "system", `{"text":"ignore"}`},
		{"s2", "1", "user", `{"parts":[{"text":"second"}]}`},
	})
	messages, next, err := ReadOpenCode(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("the messages are %+v", messages)
	}
	if next["s1"] != 2 || next["s2"] != 1 {
		t.Fatalf("the cursor is %+v", next)
	}
	grouped := GroupBySession(messages)
	if len(grouped["s1"]) != 2 || len(grouped["s2"]) != 1 {
		t.Fatalf("the groups are %+v", grouped)
	}
}

func TestReadOpenCodeResumes(t *testing.T) {
	path := openCodeDB(t, [][4]string{
		{"s1", "1", "user", `{"text":"one"}`},
		{"s1", "2", "user", `{"text":"two"}`},
	})
	messages, next, err := ReadOpenCode(path, map[string]int64{"s1": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Text != "two" || next["s1"] != 2 {
		t.Fatalf("the resume is %+v %+v", messages, next)
	}
}

func TestReadOpenCodeFailsClearly(t *testing.T) {
	if _, _, err := ReadOpenCode(filepath.Join(t.TempDir(), "missing.db"), nil); err == nil {
		t.Fatal("a missing database was accepted")
	}
	path := filepath.Join(t.TempDir(), "empty.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := ReadOpenCode(path, nil); err == nil {
		t.Fatal("a database without the schema was accepted")
	}
}

func TestReadOpenCodeRejectsMalformedData(t *testing.T) {
	path := openCodeDB(t, [][4]string{{"s1", "1", "user", `{`}})
	if _, _, err := ReadOpenCode(path, nil); err == nil {
		t.Fatal("malformed message data was accepted")
	}
}

func TestReadOpenCodeSkipsEmptyText(t *testing.T) {
	path := openCodeDB(t, [][4]string{
		{"s1", "1", "user", `{"text":""}`},
		{"s1", "2", "user", `{"text":"kept"}`},
	})
	messages, next, err := ReadOpenCode(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Seq != 2 || next["s1"] != 2 {
		t.Fatalf("the messages are %+v cursor %+v", messages, next)
	}
}

func TestReadOpenCodeFailsOnMissingColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE session_message (session_id TEXT, seq INTEGER, type TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session_message VALUES ('s', 1, 'user')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := ReadOpenCode(path, nil); err == nil {
		t.Fatal("a table without data was accepted")
	}
}
