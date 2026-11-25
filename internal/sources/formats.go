package sources

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/lindoelio/smart-agents-memory/internal/extract"
)

const maxTurnChars = 16000

var (
	systemReminder = regexp.MustCompile(`(?is)<system-reminder>.*?</system-reminder>`)
	additionalMeta = regexp.MustCompile(`(?is)<ADDITIONAL_METADATA>.*?</ADDITIONAL_METADATA>`)
	settingsChange = regexp.MustCompile(`(?is)<USER_SETTINGS_CHANGE>.*?</USER_SETTINGS_CHANGE>`)
	timestampTag   = regexp.MustCompile(`(?is)<timestamp>.*?</timestamp>`)
	userQueryTag   = regexp.MustCompile(`(?is)</?user_query>`)
	userRequestTag = regexp.MustCompile(`(?is)</?USER_REQUEST>`)
	agentsBlock    = regexp.MustCompile(`(?is)<INSTRUCTIONS>.*?</INSTRUCTIONS>`)
)

// Parse normalizes one transcript in the given format.
func Parse(format Format, data []byte) ([]extract.Turn, error) {
	switch format {
	case FormatMessagesJSONL:
		return parseMessages(data)
	case FormatACPJSONL:
		return parseACP(data)
	case FormatStepJSONL:
		return parseSteps(data)
	case FormatResponseItemJSONL:
		return parseResponseItems(data)
	default:
		return nil, fmt.Errorf("unsupported transcript format %q", format)
	}
}

func parseMessages(data []byte) ([]extract.Turn, error) {
	var turns []extract.Turn
	err := scanJSONLines(data, func(obj map[string]any) {
		if reason, _ := obj["synthetic_reason"].(string); strings.TrimSpace(reason) != "" {
			return
		}
		role := normalizeRole(roleOf(obj))
		if role == "" {
			return
		}
		text := textOf(firstNonNil(obj["content"], obj["message"], obj["parts"], obj["text"]))
		clean := cleanText(text)
		if clean == "" || isAgentsInstructions(clean) {
			return
		}
		turns = append(turns, extract.Turn{Role: role, Text: clip(clean)})
	})
	return turns, err
}

func parseACP(data []byte) ([]extract.Turn, error) {
	var turns []extract.Turn
	err := scanJSONLines(data, func(obj map[string]any) {
		method, _ := obj["method"].(string)
		if method != "session/update" {
			return
		}
		params, _ := obj["params"].(map[string]any)
		update, _ := params["update"].(map[string]any)
		kind, _ := update["sessionUpdate"].(string)
		var role string
		switch kind {
		case "user_message_chunk":
			role = "user"
		case "agent_message_chunk":
			role = "assistant"
		default:
			return
		}
		text := cleanText(textOf(update["content"]))
		if text == "" {
			return
		}
		turns = append(turns, extract.Turn{Role: role, Text: clip(text)})
	})
	return turns, err
}

func parseSteps(data []byte) ([]extract.Turn, error) {
	var turns []extract.Turn
	err := scanJSONLines(data, func(obj map[string]any) {
		kind, _ := obj["type"].(string)
		var role string
		switch kind {
		case "USER_INPUT":
			role = "user"
		case "PLANNER_RESPONSE":
			role = "assistant"
		default:
			return
		}
		text := cleanText(textOf(obj["content"]))
		if text == "" || isAgentsInstructions(text) {
			return
		}
		turns = append(turns, extract.Turn{Role: role, Text: clip(text)})
	})
	return turns, err
}

func parseResponseItems(data []byte) ([]extract.Turn, error) {
	var turns []extract.Turn
	err := scanJSONLines(data, func(obj map[string]any) {
		if kind, _ := obj["type"].(string); kind != "response_item" {
			return
		}
		payload, _ := obj["payload"].(map[string]any)
		if kind, _ := payload["type"].(string); kind != "message" {
			return
		}
		role := normalizeRole(stringValue(payload["role"]))
		if role == "" {
			return
		}
		text := cleanText(textOf(payload["content"]))
		if text == "" || isAgentsInstructions(text) || strings.HasPrefix(text, "# AGENTS.md instructions") {
			return
		}
		turns = append(turns, extract.Turn{Role: role, Text: clip(text)})
	})
	return turns, err
}

func scanJSONLines(data []byte, fn func(map[string]any)) error {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			return fmt.Errorf("line %d: %w", i+1, err)
		}
		fn(obj)
	}
	return nil
}

func roleOf(obj map[string]any) string {
	for _, key := range []string{"role", "type", "sender"} {
		if value := stringValue(obj[key]); value != "" {
			return value
		}
	}
	if message, ok := obj["message"].(map[string]any); ok {
		if value := stringValue(message["role"]); value != "" {
			return value
		}
	}
	return ""
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human", "user_input":
		return "user"
	case "assistant", "ai", "model", "planner_response":
		return "assistant"
	default:
		return ""
	}
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
		kind := strings.ToLower(stringValue(typed["type"]))
		switch kind {
		case "tool_result", "tool_use", "thinking", "reasoning":
			return ""
		case "text", "input_text", "output_text":
			return stringValue(typed["text"])
		}
		return textOf(firstNonNil(typed["text"], typed["content"], typed["message"], typed["parts"]))
	default:
		return ""
	}
}

func stringValue(value any) string {
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

func cleanText(text string) string {
	text = systemReminder.ReplaceAllString(text, "")
	text = additionalMeta.ReplaceAllString(text, "")
	text = settingsChange.ReplaceAllString(text, "")
	text = timestampTag.ReplaceAllString(text, "")
	text = userQueryTag.ReplaceAllString(text, "")
	text = userRequestTag.ReplaceAllString(text, "")
	text = agentsBlock.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func isAgentsInstructions(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "# AGENTS.md instructions")
}

func clip(text string) string {
	runes := []rune(text)
	if len(runes) <= maxTurnChars {
		return text
	}
	return string(runes[:maxTurnChars]) + "\n[truncated]"
}
