package ethindex

import (
	"reflect"
	"testing"
)

func TestChunkBlockRange(t *testing.T) {
	tests := []struct {
		name     string
		from, to uint64
		size     uint64
		want     []blockRange
	}{
		{
			name: "single block",
			from: 10, to: 10, size: 100,
			want: []blockRange{{10, 10}},
		},
		{
			name: "exact divisible",
			from: 0, to: 199, size: 100,
			want: []blockRange{{0, 99}, {100, 199}},
		},
		{
			name: "partial last chunk",
			from: 0, to: 150, size: 100,
			want: []blockRange{{0, 99}, {100, 150}},
		},
		{
			name: "size of one",
			from: 5, to: 7, size: 1,
			want: []blockRange{{5, 5}, {6, 6}, {7, 7}},
		},
		{
			name: "size larger than range",
			from: 100, to: 105, size: 1000,
			want: []blockRange{{100, 105}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chunkBlockRange(tc.from, tc.to, tc.size)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("chunkBlockRange(%d, %d, %d) = %v, want %v",
					tc.from, tc.to, tc.size, got, tc.want)
			}
		})
	}
}
