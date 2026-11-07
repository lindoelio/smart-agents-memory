package memory

import (
	"strings"
	"testing"
)

func TestParseAndFormatRoundTrip(t *testing.T) {
	source := `---
title: "A durable lesson"
type: lesson
date: 2026-11-07
source: "smart-agents-memory-cli"
native: minimax-agent
tier: cold
native_type: reference
---

## A durable lesson

The rule. Why it matters.
`
	entry, ok := Parse(source)
	if !ok {
		t.Fatal("the note was not parsed")
	}
	if entry.Title != "A durable lesson" {
		t.Fatalf("title = %q", entry.Title)
	}
	if entry.Type != TypeLesson {
		t.Fatalf("type = %q", entry.Type)
	}
	if entry.Tier != TierCold {
		t.Fatalf("tier = %q", entry.Tier)
	}
	if entry.Body != "The rule. Why it matters." {
		t.Fatalf("body = %q", entry.Body)
	}
	if entry.Date != "2026-11-07" {
		t.Fatalf("date = %q", entry.Date)
	}

	again, ok := Parse(Format(entry))
	if !ok {
		t.Fatal("the formatted note was not parsed")
	}
	if again.Body != entry.Body || again.Title != entry.Title || again.Type != entry.Type {
		t.Fatalf("round trip changed the entry: %+v", again)
	}
}

func TestParseKeepsUnknownKeys(t *testing.T) {
	source := `---
title: "Keeps what it does not know"
type: lesson
date: 2026-11-07
alwaysApply: true
custom: "kept"
---

Body.
`
	entry, ok := Parse(source)
	if !ok {
		t.Fatal("the note was not parsed")
	}
	if entry.Extra["alwaysApply"] != "true" {
		t.Fatalf("alwaysApply = %q", entry.Extra["alwaysApply"])
	}
	if entry.Extra["custom"] != "kept" {
		t.Fatalf("custom = %q", entry.Extra["custom"])
	}
	formatted := Format(entry)
	if !strings.Contains(formatted, "alwaysApply: true") {
		t.Fatalf("the extra key was dropped:\n%s", formatted)
	}
}

func TestParseRejectsANoteWithoutTitle(t *testing.T) {
	if _, ok := Parse("---\ntype: lesson\n---\n\nBody.\n"); ok {
		t.Fatal("a note without a title was accepted")
	}
	if _, ok := Parse("no frontmatter at all"); ok {
		t.Fatal("a note without frontmatter was accepted")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"CI is not the test loop":    "ci-is-not-the-test-loop",
		"  Trimmed  ":                "trimmed",
		"pipefail aborts the script": "pipefail-aborts-the-script",
		"":                           "entry",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlugifyShortensLongTitles(t *testing.T) {
	long := strings.Repeat("very long title ", 12)
	slug := Slugify(long)
	if len(slug) > 80 {
		t.Fatalf("the slug is %d characters long", len(slug))
	}
	if !strings.Contains(slug, "-") {
		t.Fatalf("the slug has no hash: %q", slug)
	}
	if Slugify(long) != slug {
		t.Fatal("the slug is not deterministic")
	}
}

func TestASCIISlug(t *testing.T) {
	cases := map[string]string{
		"Reconciliação do corpus":               "reconciliacao-do-corpus",
		"Reconciliação do corpus: mtime decide": "reconciliacao-do-corpus-mtime-decide",
		"---leading": "leading",
	}
	for input, want := range cases {
		got := ASCIISlug(input)
		if got != want {
			t.Errorf("ASCIISlug(%q) = %q, want %q", input, got, want)
		}
	}
	long := ASCIISlug(strings.Repeat("abc ", 40))
	if len(long) > 64 {
		t.Fatalf("the topic name is %d characters long", len(long))
	}
	for i, r := range long {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !valid {
			t.Fatalf("the topic name has an invalid character at %d: %q", i, long)
		}
	}
}

func TestNormTitle(t *testing.T) {
	if NormTitle("CI is not the test loop") != NormTitle("ci is not the test loop") {
		t.Fatal("the comparison is case sensitive")
	}
	if NormTitle("`code` title") != "code title" {
		t.Fatalf("NormTitle = %q", NormTitle("`code` title"))
	}
}

func TestTypeMapping(t *testing.T) {
	if TypeUser.NativeTarget() != "minimax-user" {
		t.Fatal("a user entry does not target the user store")
	}
	if TypeLesson.NativeTarget() != "minimax-agent" {
		t.Fatal("a lesson does not target the agent store")
	}
	if TypeUser.NativeType() != "user" {
		t.Fatal("a user entry is not a user native type")
	}
	if TypeLesson.NativeType() != "reference" {
		t.Fatal("a lesson is not a reference native type")
	}
	for _, typ := range Types {
		if !typ.Valid() {
			t.Fatalf("%q is not valid", typ)
		}
	}
	if Type("unknown").Valid() {
		t.Fatal("an unknown type is valid")
	}
}

func TestEffectiveTier(t *testing.T) {
	if (Entry{}).EffectiveTier() != TierCold {
		t.Fatal("an entry without a tier is not cold")
	}
	if (Entry{Tier: TierHot}).EffectiveTier() != TierHot {
		t.Fatal("a hot entry is not hot")
	}
}

func TestIsEnglish(t *testing.T) {
	if !IsEnglish("The rule is that you always write the why, because a rule without a why is not reusable at all.") {
		t.Fatal("English was not recognized")
	}
	if IsEnglish("A regra e que voce sempre escreve o porque, porque uma regra sem motivo nao serve para nada aqui.") {
		t.Fatal("Portuguese was not recognized")
	}
	if !IsEnglish("short text") {
		t.Fatal("a short text warns")
	}
}
