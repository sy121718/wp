package i18n

import "testing"

func TestCountPlaceholders(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},
		{"无占位符", 0},
		{"%s %d", 2},
		{"%[1]s 和 %[2]s", 2},
		{"100%% 完成", 0},
		{"%.2f", 1},
		{"%+v", 1},
		{"%#v", 1},
		{"%5d", 1},
		{"%s%t%x", 3},
		{"不是%占位", 0},
	}

	for _, tc := range cases {
		if got := CountPlaceholders(tc.text); got != tc.want {
			t.Fatalf("CountPlaceholders(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestHasStringPlaceholdersOnly(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"纯文本", true},
		{"%s", true},
		{"%[1]s %[2]s", true},
		{"100%% 完成", true},
		{"%d", false},
		{"%.2f", false},
		{"%s%d", false},
	}

	for _, tc := range cases {
		if got := HasStringPlaceholdersOnly(tc.text); got != tc.want {
			t.Fatalf("HasStringPlaceholdersOnly(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
