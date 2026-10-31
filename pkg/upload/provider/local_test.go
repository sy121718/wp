package uploadprovider

import "testing"

func TestSanitizeFilenameWindowsReservedNames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"CON", "_CON"},
		{"con.txt", "_con.txt"},
		{"NUL", "_NUL"},
		{"COM1", "_COM1"},
		{"com9.log", "_com9.log"},
		{"LPT5", "_LPT5"},
		{"AUX", "_AUX"},
		{"PRN", "_PRN"},
		{"CON.txt", "_CON.txt"},

		{"COM10", "COM10"},
		{"LPT10", "LPT10"},

		{"report.txt", "report.txt"},
		{"report", "report"},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeFilenameTrailingDotsAndSpaces(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report.txt. ", "report.txt"},
		{"name. ", "name"},
		{"name .", "name"},
		{"CON.txt. ", "_CON.txt"},
		{"  spaced  ", "spaced"},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeFilenamePathCharsAndTraversal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/", "_"},
		{"a/b", "a_b"},
		{`a\b`, "a_b"},
		{"a:b", "a_b"},
		{"a*b", "a_b"},
		{"a?b", "a_b"},
		{`a"b`, "a_b"},
		{"a<b", "a_b"},
		{"a>b", "a_b"},
		{"a|b", "a_b"},
		{"..", ""},
		{".", ""},
		{"", ""},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.in); got != tc.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
