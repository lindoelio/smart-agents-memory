package sources

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// DBMessage is one user or assistant message from a session database.
type DBMessage struct {
	Session string
	Seq     int64
	Role    string
	Text    string
}

// ReadOpenCode reads the OpenCode session_message table read-only.
//
// after is the last seq per session. The returned map is the new cursor.
func ReadOpenCode(path string, after map[string]int64) ([]DBMessage, map[string]int64, error) {
	next := map[string]int64{}
	for k, v := range after {
		next[k] = v
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, next, err
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, next, err
	}
	rows, err := db.Query(`SELECT DISTINCT session_id FROM session_message WHERE type IN ('user','assistant')`)
	if err != nil {
		return nil, next, fmt.Errorf("opencode session_message: %w", err)
	}
	var sessions []string
	for rows.Next() {
		var session string
		if err := rows.Scan(&session); err != nil {
			rows.Close()
			return nil, next, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, next, err
	}
	rows.Close()

	var out []DBMessage
	for _, session := range sessions {
		upper := next[session]
		msgRows, err := db.Query(`SELECT seq, type, data FROM session_message
			WHERE session_id = ? AND type IN ('user','assistant') AND seq > ?
			ORDER BY seq`, session, upper)
		if err != nil {
			return nil, next, err
		}
		for msgRows.Next() {
			var seq int64
			var kind, data string
			if err := msgRows.Scan(&seq, &kind, &data); err != nil {
				msgRows.Close()
				return nil, next, err
			}
			role := normalizeRole(kind)
			if role == "" {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				msgRows.Close()
				return nil, next, fmt.Errorf("opencode message %s/%d: %w", session, seq, err)
			}
			text := cleanText(textOf(firstNonNil(payload["text"], payload["content"], payload["parts"])))
			if text == "" {
				if seq > next[session] {
					next[session] = seq
				}
				continue
			}
			out = append(out, DBMessage{Session: session, Seq: seq, Role: role, Text: clip(text)})
			if seq > next[session] {
				next[session] = seq
			}
		}
		if err := msgRows.Err(); err != nil {
			msgRows.Close()
			return nil, next, err
		}
		msgRows.Close()
	}
	return out, next, nil
}

// GroupBySession groups messages preserving sequence order.
func GroupBySession(messages []DBMessage) map[string][]DBMessage {
	out := map[string][]DBMessage{}
	for _, msg := range messages {
		out[msg.Session] = append(out[msg.Session], msg)
	}
	return out
}
