package blockhttp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesBoundaries(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"上限为 0 时不截断", "abcdef", 0, "abcdef"},
		{"上限为负时不截断", "abcdef", -1, "abcdef"},
		{"长度恰好等于上限时不截断", "abcde", 5, "abcde"},
		{"ASCII 超限截到上限并补省略号", strings.Repeat("x", 20), 8, strings.Repeat("x", 8) + "…"},
		{"多字节字符不被切断", strings.Repeat("测", 5), 11, strings.Repeat("测", 3) + "…"},
		{"上限落在字符中间时退到边界", strings.Repeat("测", 5), 10, strings.Repeat("测", 3) + "…"},
		{"多字节内容恰好不超限时原样", strings.Repeat("测", 3), 9, strings.Repeat("测", 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateRunes(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("truncateRunes(len=%d, max=%d)=%q，want %q", len(tc.in), tc.max, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("截断结果不是合法 UTF-8：%q", got)
			}
		})
	}
}

func TestBlockRefTextCapMatchesFormerShellLimit(t *testing.T) {
	const formerShellNoticeMaxBytes = 512
	if blockRefTextMaxBytes != formerShellNoticeMaxBytes {
		t.Fatalf("blockRefTextMaxBytes=%d，want %d（旧壳层回执上限）",
			blockRefTextMaxBytes, formerShellNoticeMaxBytes)
	}
	got := truncateRunes(strings.Repeat("x", formerShellNoticeMaxBytes+100), blockRefTextMaxBytes-1)
	content := strings.TrimSuffix(got, "…")
	if len(content) != formerShellNoticeMaxBytes-1 {
		t.Fatalf("内容上限应为 %d 字节（旧上限 −1），实际 %d",
			formerShellNoticeMaxBytes-1, len(content))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("超限时必须留下截断标记，实际 %q", got)
	}
}
