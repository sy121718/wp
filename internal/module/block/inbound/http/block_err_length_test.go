package blockhttp

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	blockcontract "go_wp/internal/module/block/contract"
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

func TestBlockRefErrorCapsLongUsageDetail(t *testing.T) {
	short := deleteRefMessage(t, "page-42")
	prefix, _, ok := strings.Cut(short, "：")
	if !ok {
		t.Fatalf("带明细的提示应形如「受控文案 ：明细」，实际 %q", short)
	}

	longDetail := "/" + strings.Repeat("x", 900)
	long := deleteRefMessage(t, longDetail)

	if !strings.HasPrefix(long, prefix+"：") {
		t.Fatalf("长明细不应改动受控文案前缀：\n got %q\nwant 前缀 %q", long, prefix)
	}
	body := strings.TrimSuffix(long, "…")
	if len(body) > blockRefTextMaxBytes-1 {
		t.Fatalf("整串内容超过 %d 字节：%d（%q）", blockRefTextMaxBytes-1, len(body), long)
	}
	if !strings.HasSuffix(long, "…") {
		t.Fatalf("900 字符的定位串必须被截断并留下标记，实际 %q", long)
	}
	if !utf8.ValidString(long) {
		t.Fatalf("整串不是合法 UTF-8：%q", long)
	}
	if strings.Contains(long, longDetail) {
		t.Fatalf("900 字符的定位串不该原样出现在提示里")
	}
}

func TestBlockRefTextCallSiteKeepsOffset(t *testing.T) {
	src, err := os.ReadFile("block_err.go")
	if err != nil {
		t.Fatalf("读取 block_err.go 失败：%v", err)
	}
	const want = `truncateRunes(msg+"："+detail, blockRefTextMaxBytes-1)`
	if !strings.Contains(string(src), want) {
		t.Fatalf("整串封顶的调用点变了：期望出现 %s（−1 = 给截断标记留 1 字节）", want)
	}
}

func deleteRefMessage(t *testing.T, label string) string {
	t.Helper()
	svc := &bulkDeleteStubService{
		fail: map[string]error{
			"b1": blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
				{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: label, Detail: "main"},
			}),
		},
	}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusConflict {
		t.Fatalf("被引用应回 409，实际 %d（body=%v）", status, parsed)
	}
	msg, _ := parsed["message"].(string)
	if strings.TrimSpace(msg) == "" {
		t.Fatalf("409 响应必须带可读提示，实际 %v", parsed)
	}
	return msg
}
