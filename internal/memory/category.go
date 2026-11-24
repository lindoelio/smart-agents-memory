package memory

import "strings"

// TreeCategories is the semantic taxonomy used by the memory tree. It follows
// Qwen Code's AUTO_MEMORY_TREE_CATEGORIES; Uncategorized is the fallback.
var TreeCategories = []string{
	"basic_information",
	"hobbies",
	"communication_preference",
	"behavior_habit",
	"project_introduction",
	"tech_stack",
	"build_configuration",
	"dependency_configuration",
	"ide_configuration",
	"scm_configuration",
	"environment_configuration",
	"code_standard",
	"practice_standard",
	"testing_standard",
	"comment_standard",
	"tool_experience",
	"mcp_experience",
	"common_pitfall",
	"important_decision",
	"task_summary",
}

// Uncategorized is the fallback category.
const Uncategorized = "uncategorized"

// ValidCategory reports whether category is in the taxonomy.
func ValidCategory(category string) bool {
	if category == Uncategorized {
		return true
	}
	for _, known := range TreeCategories {
		if category == known {
			return true
		}
	}
	return false
}

// InferCategory returns the explicit valid category, or a deterministic
// default derived from the type, title, description and keywords.
func InferCategory(e Entry) string {
	if ValidCategory(e.Category) {
		return e.Category
	}
	text := strings.ToLower(strings.Join([]string{e.Title, e.Description, strings.Join(e.Keywords, " ")}, " "))
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(text, word) {
				return true
			}
		}
		return false
	}

	switch e.Type {
	case TypeUser:
		if has("prefer", "communication", "language", "portuguese", "english", "tone", "reply") {
			return "communication_preference"
		}
		if has("identity", "name", "role", "background", "profile") {
			return "basic_information"
		}
		if has("hobby", "hobbies") {
			return "hobbies"
		}
		return "behavior_habit"
	case TypeProject:
		if has("tech", "stack", "framework", "language", "library") {
			return "tech_stack"
		}
		if has("build", "compile", "makefile", "pipeline") {
			return "build_configuration"
		}
		if has("dependency", "dependencies", "package", "module") {
			return "dependency_configuration"
		}
		if has("ide", "editor", "vscode", "cursor") {
			return "ide_configuration"
		}
		if has("git", "branch", "scm", "worktree", "commit") {
			return "scm_configuration"
		}
		if has("environment", "deploy", "deployment", "runtime") {
			return "environment_configuration"
		}
		if has("introduction", "overview", "context") {
			return "project_introduction"
		}
		return "important_decision"
	case TypeLesson:
		if has("test", "testing", "flaky", "coverage") {
			return "testing_standard"
		}
		if has("comment", "documentation", "docstring") {
			return "comment_standard"
		}
		if has("mcp") {
			return "mcp_experience"
		}
		if has("tool", "cli", "command", "flag") {
			return "tool_experience"
		}
		if has("fail", "failure", "bug", "error", "pitfall", "trap", "regression") {
			return "common_pitfall"
		}
		if has("style", "standard", "convention", "lint", "format") {
			return "code_standard"
		}
		return "practice_standard"
	case TypeReference:
		if has("mcp") {
			return "mcp_experience"
		}
		return "tool_experience"
	}
	return Uncategorized
}
