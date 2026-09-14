package database

import "testing"

func TestEscapeLikePattern(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"50%", "50\\%"},
		{"a_b", "a\\_b"},
		{`back\slash`, `back\\slash`},
	}
	for _, tc := range cases {
		if got := EscapeLikePattern(tc.in); got != tc.want {
			t.Errorf("EscapeLikePattern(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
