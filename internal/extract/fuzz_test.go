package extract

import "testing"

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"role":"user","content":"a question"}`))
	f.Add([]byte(`{"messages":[{"type":"user","message":{"parts":[{"text":"hi"}]}}]}`))
	f.Add([]byte("plain text from a shell"))
	f.Add([]byte(`[{"role":"human","content":"a"}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
		_, _ = ParseStrict(data, true)
		_, _ = ParseStrict(data, false)
	})
}
