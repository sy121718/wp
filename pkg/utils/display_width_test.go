package utils

import "testing"

func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"中文", 4},
		{"a中", 3},
		{"café", 4},
		{"日本語", 6},
		{"한국", 4},
	}
	for _, c := range cases {
		if got := DisplayWidth(c.in); got != c.want {
			t.Errorf("DisplayWidth(%q) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

func TestTruncateDisplayWidth(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"abc", 0, "abc"},
		{"abc", 5, "abc"},
		{"abcdef", 4, "abc…"},
		{"中文字符", 4, "中…"},
		{"中文字符", 6, "中文…"},
		{"ab", 1, "a…"},
		{"中", 1, "…"},
		{"", 3, ""},
	}
	for _, c := range cases {
		if got := TruncateDisplayWidth(c.in, c.max); got != c.want {
			t.Errorf("TruncateDisplayWidth(%q, %d) = %q，期望 %q", c.in, c.max, got, c.want)
		}
	}
}
