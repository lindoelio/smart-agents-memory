// Package extract parses agent transcripts and keeps the extraction cursor.
// It knows nothing about a harness: every format is normalized to Role/Text
// turns, and the cursor survives harness compaction.
package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Turn is one normalized message.
type Turn struct {
	Role string
	Text string
}

// SessionCursor is the processed offset of one transcript.
type SessionCursor struct {
	SessionID       string    `json:"session_id"`
	TranscriptPath  string    `json:"transcript_path,omitempty"`
	ProcessedOffset int       `json:"processed_offset"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CursorStore is the content of .extract-cursor.json.
type CursorStore struct {
	Sessions map[string]SessionCursor `json:"sessions"`
}

// Load read the cursor file. A missing file is an empty store.
func Load(path string) (CursorStore, error) {
	store := CursorStore{Sessions: map[string]SessionCursor{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return store, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return CursorStore{Sessions: map[string]SessionCursor{}}, err
	}
	if store.Sessions == nil {
		store.Sessions = map[string]SessionCursor{}
	}
	return store, nil
}

// Save writes the cursor file atomically.
func Save(path string, store CursorStore) error {
	if store.Sessions == nil {
		store.Sessions = map[string]SessionCursor{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".extract-cursor-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// StartOffset returns the first turn to process. A different session starts
// at zero; a compacted transcript that is shorter than the cursor also
// restarts at zero, so post-compaction turns are not skipped forever.
func StartOffset(store CursorStore, sessionID string, turns []Turn) int {
	cursor, ok := store.Sessions[sessionID]
	if !ok || cursor.SessionID != sessionID || cursor.ProcessedOffset > len(turns) {
		return 0
	}
	return cursor.ProcessedOffset
}

// HasNewUserTurn reports whether the unprocessed slice has a non-empty user
// message.
func HasNewUserTurn(turns []Turn, offset int) bool {
	if offset < 0 || offset > len(turns) {
		return false
	}
	for _, turn := range turns[offset:] {
		if strings.EqualFold(turn.Role, "user") && strings.TrimSpace(turn.Text) != "" {
			return true
		}
	}
	return false
}

// Advance records the offset after a run.
func Advance(store *CursorStore, sessionID, transcriptPath string, offset int, now time.Time) {
	if store.Sessions == nil {
		store.Sessions = map[string]SessionCursor{}
	}
	store.Sessions[sessionID] = SessionCursor{
		SessionID:       sessionID,
		TranscriptPath:  transcriptPath,
		ProcessedOffset: offset,
		UpdatedAt:       now,
	}
}

// Parse accepts JSONL, a JSON array/object, or plain text.
//
// It is deliberately permissive: a hook or a manual call may send a payload
// this package does not recognize, and the model is a better judge of it
// than a strict schema. A directory scan must use ParseStrict instead.
func Parse(content []byte) ([]Turn, error) {
	return parse(content, false, true)
}

// ParseStrict parses a transcript and refuses content that is not one.
//
// A watch scans broad directories that also hold configuration, storage and
// notes. Valid JSON without a single recognizable turn yields zero turns
// instead of becoming one user message; plain text is accepted only when
// allowPlain is true, which the caller sets for extensions that are textual
// by definition.
func ParseStrict(content []byte, allowPlain bool) ([]Turn, error) {
	return parse(content, true, allowPlain)
}

func parse(content []byte, strict, allowPlain bool) ([]Turn, error) {
	text := strings.TrimSpace(string(content))
	if text == "" {
		return nil, nil
	}
	if turns, ok := parseJSONLines(text); ok {
		return turns, nil
	}
	if turns, ok := parseJSONDocument(text); ok {
		return turns, nil
	}
	if strict {
		if looksLikeJSON(text) || !allowPlain {
			return nil, nil
		}
	}
	return []Turn{{Role: "user", Text: text}}, nil
}

// looksLikeJSON reports whether the content is a JSON document or a stream
// of JSON lines without a recognizable conversation. In strict mode that is
// a file the scan must ignore, not a user message.
func looksLikeJSON(text string) bool {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err == nil {
		return true
	}
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return false
		}
		lines++
	}
	return lines > 0
}

func parseJSONLines(text string) ([]Turn, bool) {
	var turns []Turn
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return nil, false
		}
		turn, ok := turnOf(value)
		if ok {
			turns = append(turns, turn)
		}
	}
	return turns, len(turns) > 0
}

func parseJSONDocument(text string) ([]Turn, bool) {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil, false
	}
	switch typed := value.(type) {
	case []any:
		var turns []Turn
		for _, item := range typed {
			if turn, ok := turnOf(item); ok {
				turns = append(turns, turn)
			}
		}
		return turns, true
	case map[string]any:
		for _, key := range []string{"messages", "history", "turns"} {
			if raw, ok := typed[key]; ok {
				if list, ok := raw.([]any); ok {
					var turns []Turn
					for _, item := range list {
						if turn, ok := turnOf(item); ok {
							turns = append(turns, turn)
						}
					}
					return turns, true
				}
			}
		}
		if turn, ok := turnOf(typed); ok {
			return []Turn{turn}, true
		}
	}
	return nil, false
}

func turnOf(value any) (Turn, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return Turn{}, false
	}
	role := scalar(object["role"])
	if role == "" {
		role = scalar(object["type"])
	}
	if role == "" {
		role = scalar(object["from"])
	}
	role = strings.ToLower(role)
	switch role {
	case "human":
		role = "user"
	case "ai", "model", "bot":
		role = "assistant"
	}
	if role == "" {
		return Turn{}, false
	}
	text := textOf(firstNonNil(object["content"], object["message"], object["parts"], object["text"]))
	if strings.TrimSpace(text) == "" {
		return Turn{}, false
	}
	return Turn{Role: role, Text: strings.TrimSpace(text)}, true
}

func textOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			if text := textOf(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return textOf(firstNonNil(typed["text"], typed["content"], typed["message"], typed["parts"]))
	default:
		return ""
	}
}

func scalar(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// FormatTurns renders turns for the extraction prompt without inventing
// metadata.
func FormatTurns(turns []Turn) string {
	var b strings.Builder
	for _, turn := range turns {
		if strings.TrimSpace(turn.Text) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n\n", turn.Role, strings.TrimSpace(turn.Text))
	}
	return strings.TrimSpace(b.String())
}
