// Package secretguard scans memory content for high-confidence credentials.
//
// The rules are a curated subset of gitleaks (MIT): distinctive prefixes and
// near-zero false positives. The scanner never returns or logs the matched
// value — only the rule that fired.
package secretguard

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Match is one rule that fired. The matched text is deliberately absent.
type Match struct {
	RuleID string
	Label  string
}

type rule struct {
	id      string
	pattern string
}

// The rules are kept in the order of the Qwen Code secret-scanner. A rule
// with no distinctive prefix does not belong here: a false positive would
// block a legitimate memory.
var rules = []rule{
	{id: "aws-access-token", pattern: `\b((?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16})\b`},
	{id: "alibaba-cloud-access-key", pattern: `\b(LTAI[a-zA-Z0-9]{12,20})\b`},
	{id: "gcp-api-key", pattern: `AIza[\w-]{35}`},
	{id: "gcp-oauth-client-secret", pattern: `GOCSPX-[\w-]{24}`},
	{id: "digitalocean-pat", pattern: `dop_v1_[a-f0-9]{64}`},
	{id: "anthropic-api-key", pattern: `sk-ant-[a-zA-Z0-9_-]{20,}`},
	{id: "openai-api-key", pattern: `sk-(?:proj|svcacct|admin)-[a-zA-Z0-9_-]{20,}|sk-[a-zA-Z0-9_-]{20,512}T3BlbkFJ[a-zA-Z0-9_-]{20,512}|sk-[a-zA-Z0-9]{48}`},
	{id: "huggingface-access-token", pattern: `hf_[a-zA-Z0-9]{34,}`},
	{id: "github-pat", pattern: `ghp_[0-9a-zA-Z]{36}`},
	{id: "github-fine-grained-pat", pattern: `github_pat_\w{82}`},
	{id: "github-app-token", pattern: `(?:ghu|ghs)_[0-9a-zA-Z]{36}`},
	{id: "github-oauth", pattern: `gho_[0-9a-zA-Z]{36}`},
	{id: "gitlab-pat", pattern: `glpat-[\w-]{20}`},
	{id: "slack-bot-token", pattern: `xox[b]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*`},
	{id: "slack-app-token", pattern: `(?i)xapp-\d-[A-Z0-9]+-\d+-[a-z0-9]+`},
	{id: "sendgrid-api-token", pattern: `SG\.[\w-]{22}\.[\w-]{43}`},
	{id: "npm-access-token", pattern: `npm_[a-zA-Z0-9]{36}`},
	{id: "stripe-access-token", pattern: `(?:sk|rk)_(?:test|live|prod)_[a-zA-Z0-9]{10,99}`},
	{id: "private-key", pattern: `(?is)-----BEGIN[ A-Z0-9_-]{0,100}PRIVATE KEY(?: BLOCK)?-----[\s\S-]{64,}?-----END[ A-Z0-9_-]{0,100}PRIVATE KEY(?: BLOCK)?-----`},
}

type compiledRule struct {
	id      string
	label   string
	pattern *regexp.Regexp
}

var (
	compiled []compiledRule
	once     sync.Once
)

func compile() {
	once.Do(func() {
		compiled = make([]compiledRule, 0, len(rules))
		for _, r := range rules {
			compiled = append(compiled, compiledRule{
				id:      r.id,
				label:   labelOf(r.id),
				pattern: regexp.MustCompile(r.pattern),
			})
		}
	})
}

// Scan returns one match per rule that fired. The matched value is never
// returned, so a caller cannot accidentally log the secret.
func Scan(text string) []Match {
	if text == "" {
		return nil
	}
	compile()
	var matches []Match
	for _, rule := range compiled {
		if rule.pattern.MatchString(text) {
			matches = append(matches, Match{RuleID: rule.id, Label: rule.label})
		}
	}
	return matches
}

// Check returns an error naming the matched rules when content contains a
// credential. The error never contains the secret itself.
func Check(title, body string) error {
	matches := Scan(title + "\n" + body)
	if len(matches) == 0 {
		return nil
	}
	labels := make([]string, 0, len(matches))
	for _, match := range matches {
		labels = append(labels, match.Label)
	}
	return fmt.Errorf("content contains potential secrets (%s); credentials cannot be stored in memory", strings.Join(labels, ", "))
}

var labelSpecial = map[string]string{
	"aws":          "AWS",
	"gcp":          "GCP",
	"api":          "API",
	"pat":          "PAT",
	"oauth":        "OAuth",
	"npm":          "NPM",
	"github":       "GitHub",
	"gitlab":       "GitLab",
	"openai":       "OpenAI",
	"digitalocean": "DigitalOcean",
	"huggingface":  "HuggingFace",
	"alibaba":      "Alibaba",
	"sendgrid":     "SendGrid",
}

func labelOf(id string) string {
	parts := strings.Split(id, "-")
	for i, part := range parts {
		if special, ok := labelSpecial[part]; ok {
			parts[i] = special
			continue
		}
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

// Redact replaces every high-confidence credential in text with the literal
// "[redacted]" and returns the number of replacements. The matched value is
// never returned or logged.
func Redact(text string) (string, int) {
	if text == "" {
		return text, 0
	}
	compile()
	out := text
	total := 0
	for _, rule := range compiled {
		matches := rule.pattern.FindAllStringIndex(out, -1)
		if len(matches) == 0 {
			continue
		}
		total += len(matches)
		out = rule.pattern.ReplaceAllString(out, "[redacted]")
	}
	return out, total
}
