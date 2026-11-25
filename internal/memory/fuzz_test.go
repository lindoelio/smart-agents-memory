package memory

import "testing"

func FuzzParse(f *testing.F) {
	f.Add("---\ntitle: A lesson\ntype: lesson\n---\n\n## A lesson\n\nThe rule.\n")
	f.Add("---\ntitle: \"quoted\"\ntype: project\nproject: app\n---\n\nbody\n")
	f.Add("not frontmatter")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = Parse(text)
	})
}
