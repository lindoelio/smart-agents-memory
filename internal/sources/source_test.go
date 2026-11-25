package sources

import "testing"

func TestBuildWithoutOverrides(t *testing.T) {
	reg := Build(nil, nil, 0, nil)
	if len(reg.Enabled()) != 7 {
		t.Fatalf("the default sources are %+v", reg.Enabled())
	}
	for _, source := range reg.Enabled() {
		if source.MaxBytes != DefaultMaxBytes {
			t.Fatalf("the max bytes is %d", source.MaxBytes)
		}
	}
}
