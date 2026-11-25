package secretguard

import (
	"strings"
	"testing"
)

func TestEveryRuleFires(t *testing.T) {
	cases := []struct {
		ruleID  string
		content string
	}{
		{"aws-access-token", "key AKIA1234567890ABCDEF here"},
		{"alibaba-cloud-access-key", "LTAI123456789012"},
		{"gcp-api-key", "AIza" + strings.Repeat("a", 35)},
		{"gcp-oauth-client-secret", "GOCSPX-" + strings.Repeat("a", 24)},
		{"digitalocean-pat", "dop_v1_" + strings.Repeat("a", 64)},
		{"anthropic-api-key", "sk-ant-" + strings.Repeat("a", 20)},
		{"openai-api-key", "sk-proj-" + strings.Repeat("a", 20)},
		{"huggingface-access-token", "hf_" + strings.Repeat("a", 34)},
		{"github-pat", "ghp_" + strings.Repeat("a", 36)},
		{"github-fine-grained-pat", "github_pat_" + strings.Repeat("a", 82)},
		{"github-app-token", "ghs_" + strings.Repeat("a", 36)},
		{"github-oauth", "gho_" + strings.Repeat("a", 36)},
		{"gitlab-pat", "glpat-" + strings.Repeat("a", 20)},
		{"slack-bot-token", "xoxb-1234567890-1234567890-abc"},
		{"slack-app-token", "xapp-1-ABC123-123-abc"},
		{"sendgrid-api-token", "SG." + strings.Repeat("a", 22) + "." + strings.Repeat("b", 43)},
		{"npm-access-token", "npm_" + strings.Repeat("a", 36)},
		{"stripe-access-token", "sk_test_" + strings.Repeat("a", 10)},
		{"private-key", "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("a", 64) + "\n-----END RSA PRIVATE KEY-----"},
	}

	for _, tc := range cases {
		t.Run(tc.ruleID, func(t *testing.T) {
			matches := Scan(tc.content)
			found := false
			for _, match := range matches {
				if match.RuleID == tc.ruleID {
					found = true
				}
			}
			if !found {
				t.Fatalf("rule %q did not fire; got %+v", tc.ruleID, matches)
			}
			err := Check("a title", tc.content)
			if err == nil {
				t.Fatal("Check accepted the secret")
			}
			if strings.Contains(err.Error(), tc.content) {
				t.Fatalf("the error echoed the secret: %v", err)
			}
		})
	}
}

func TestOrdinaryTextIsClean(t *testing.T) {
	cases := []string{
		"Rule: verify the migration locally. Why: CI is not the test loop.",
		"id 550e8400-e29b-41d4-a716-446655440000",
		"sha256 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		"func main() { fmt.Println(\"hello\") }",
		"Authorization: Bearer <redacted>",
	}
	for _, content := range cases {
		if matches := Scan(content); len(matches) != 0 {
			t.Errorf("ordinary text matched %+v: %q", matches, content)
		}
		if err := Check("title", content); err != nil {
			t.Errorf("Check rejected ordinary text: %v", err)
		}
	}
}

func TestATitleSecretIsCaught(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 36)
	err := Check(secret, "an ordinary body")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("the title scan is wrong: %v", err)
	}
}

func TestLabelsAreReadable(t *testing.T) {
	labels := map[string]string{}
	for _, rule := range rules {
		labels[rule.id] = labelOf(rule.id)
	}
	if labels["github-pat"] != "GitHub PAT" {
		t.Fatalf("github-pat label is %q", labels["github-pat"])
	}
	if labels["aws-access-token"] != "AWS Access Token" {
		t.Fatalf("aws label is %q", labels["aws-access-token"])
	}
	if labels["private-key"] != "Private Key" {
		t.Fatalf("private-key label is %q", labels["private-key"])
	}
}

func TestScanReturnsOneMatchPerRule(t *testing.T) {
	content := "ghp_" + strings.Repeat("a", 36) + " ghp_" + strings.Repeat("b", 36)
	matches := Scan(content)
	count := 0
	for _, match := range matches {
		if match.RuleID == "github-pat" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("github-pat fired %d times", count)
	}
}

func TestRedactReplacesHighConfidenceSecrets(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	input := "the token is " + token + " and the other is " + token
	got, count := Redact(input)
	if count != 2 {
		t.Fatalf("the count is %d", count)
	}
	if strings.Contains(got, token) || !strings.Contains(got, "[redacted]") {
		t.Fatalf("the redacted text is %q", got)
	}
}

func TestRedactLeavesOrdinaryTextAndEmptyInput(t *testing.T) {
	for _, input := range []string{"", "no credentials here", "sk-short"} {
		got, count := Redact(input)
		if got != input || count != 0 {
			t.Fatalf("Redact(%q) = %q %d", input, got, count)
		}
	}
}
