package tokens

import "testing"

func TestEstimateText(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1}, // 4 * 0.25
		{"abc", 1},  // 0.75 -> ceil
		{"中文", 2},   // 2 * 1
		{"ab中", 2},  // 0.5 + 1 = 1.5 -> ceil
	}
	for _, tc := range cases {
		if got := EstimateText(tc.in); got != tc.want {
			t.Errorf("EstimateText(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestEstimateMarshals(t *testing.T) {
	// {"k":"abcd"} is 11 ASCII runes: ceil(11 * 0.25) = 3.
	if got := Estimate(map[string]any{"k": "abcd"}); got != 3 {
		t.Fatalf("Estimate = %d, want 3", got)
	}
}
