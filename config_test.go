package ethindex

import "testing"

func TestConfigApplyDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "zero values become defaults",
			in:   Config{},
			want: Config{FinalityDepth: 64, MaxBlockRange: 10_000, MaxConcurrency: 16},
		},
		{
			name: "explicit values preserved",
			in:   Config{FinalityDepth: 32, MaxBlockRange: 500, MaxConcurrency: 4},
			want: Config{FinalityDepth: 32, MaxBlockRange: 500, MaxConcurrency: 4},
		},
		{
			name: "partial overrides",
			in:   Config{FinalityDepth: 128},
			want: Config{FinalityDepth: 128, MaxBlockRange: 10_000, MaxConcurrency: 16},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			got.applyDefaults()
			if got != tc.want {
				t.Errorf("applyDefaults() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
