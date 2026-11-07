package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// Keep letters, digits, underscore and dash; drop the rest. The
	// letter classes are Unicode: an accented word keeps its accents in
	// the address of the note.
	slugStrip = regexp.MustCompile(`[^\p{L}\p{N}_\s-]`)
	slugSpace = regexp.MustCompile(`[\s_]+`)
	slugDash  = regexp.MustCompile(`-{2,}`)

	// The MiniMax runtime only recognizes topic files whose name matches
	// ^[a-z0-9][a-z0-9-]{0,63}$ — an accent breaks recognition.
	asciiNonWord = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slugify is the corpus address of an entry.
//
// The title is not truncated: the name is only an address, and the full
// title lives in the frontmatter. Only the extreme is shortened, with a
// short hash so it does not collide.
func Slugify(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = slugStrip.ReplaceAllString(text, "")
	text = slugSpace.ReplaceAllString(text, "-")
	text = slugDash.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	if len(text) > 80 {
		digest := sha1.Sum([]byte(text))
		text = strings.TrimRight(text[:73], "-") + "-" + hex.EncodeToString(digest[:])[:6]
	}
	if text == "" {
		return "entry"
	}
	return text
}

// ASCIISlug is the name of a topic file: lowercase ASCII, dash separated,
// at most 64 characters, always starting with a letter or a digit.
func ASCIISlug(text string) string {
	// Decompose, then drop the combining marks: an accent becomes the
	// plain letter, and a cedilla becomes a c.
	decomposed := norm.NFKD.String(strings.ToLower(text))
	var b strings.Builder
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	slug := strings.ReplaceAll(b.String(), "~", "-")
	slug = asciiNonWord.ReplaceAllString(slug, "-")
	slug = slugDash.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	slug = strings.Trim(slug[:min(len(slug), 64)], "-")
	if slug == "" {
		return "topic"
	}
	if !isAlnum(rune(slug[0])) {
		slug = strings.Trim(("t-" + slug)[:64], "-")
	}
	if slug == "" {
		return "topic"
	}
	return slug
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
