package smartagentsmemory

import (
	"io/fs"
	"testing"
)

func TestSkillFSIsEmbedded(t *testing.T) {
	entries, err := fs.ReadDir(SkillFS, "skills")
	if err != nil || len(entries) == 0 {
		t.Fatalf("the embedded skill tree is %v %v", entries, err)
	}
}
