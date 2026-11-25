package native

import "testing"

func FuzzParseTopic(f *testing.F) {
	f.Add("---\ntitle: A topic\ncategory: testing_standard\ndescription: A topic\n---\n\n## A topic\n\nThe rule.\n", "a-topic.md")
	f.Add("not a topic", "x.md")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, content, name string) {
		_, _ = ParseTopic(content, name)
	})
}
