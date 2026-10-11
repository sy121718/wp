package blockhttp

// block_err_length_test.go — 引用明细整串的展示封顶（区块侧自持常量）。
//
// 背景（task-67）：整串「受控文案 + ：+ 引用明细」原先按壳层读侧回执协议的上限封顶
// （`shell.NoticeMaxBytes-1`）。那族协议已整族下线，只留这一处活引用，于是把常量搬到
// 使用方：`blockRefTextMaxBytes = 512`，调用点仍然 −1（= 511 内容字节），行为逐字节不变。
//
// 这里钉三件事，前两件失效时都是**静默**的：
//
//  1. `truncateRunes` 的边界 —— 按字节上限截断必须停在 UTF-8 字符边界上（切断一个多字节
//     字符会产出非法 UTF-8，浏览器把它显示成替换符），且超长时以「…」收尾：截断要看得见，
//     否则运营看到一条「完整」但其实被砍掉后半段的定位信息，会去错地方找引用。
//  2. 上限常量与旧口径同值 —— 512 内容字节、用时 −1（旧的 `shell.NoticeMaxBytes-1` = 511），
//     这样「换成局部常量」这件事在测试里是可判定的，而不只是 diff 里看着像。
//  3. through-handler 的整串形状 —— 长引用明细走完真实出口后：前缀（受控文案）仍在、
//     明细被截断且有截断标记、整串合法 UTF-8 且不超过 511 内容字节。
//
// 为什么通过端点而不是直接调 `blockRefErrText`：入参是 gin.Context（要 i18n 取词），
// 而 `deleteResp` 已经把上下文与桩服务都组装好了（block_bulk_delete_test.go）。
// 前缀不写死中文字面量：先跑一次短明细取出「：」前的部分，再拿它当下一次的期望 ——
// 文案随词条演进时这条用例不会跟着变红，而它要守的是形状不是措辞。

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	blockcontract "go_wp/internal/module/block/contract"
)

// TestTruncateRunesBoundaries 截断函数的边界：非正值与未超限原样返回，
// 超限时截到**字符边界**并补「…」。
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

// TestBlockRefTextCapMatchesFormerShellLimit 上限常量与旧 `shell.NoticeMaxBytes` 同值，
// 且调用点的 −1 语义没变（内容 511 字节 + 截断标记）。
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

// TestBlockRefErrorCapsLongUsageDetail through-handler：长引用明细的整串形状。
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

// TestBlockRefTextCallSiteKeepsOffset 上限常量的**使用方式**也是协议的一部分。
//
// 为什么用形状判据而不是端到端：明细自己先被 `blockRefDetailMaxBytes`（420）封顶，
// 端到端整串最长只有 439 字节（实测：前缀 12 字节 + ：+ 420 内容 + 「…」），够不到 511 ——
// 把调用点的 `-1` 删掉，端到端断言照样全绿。而那个 `-1` 是「给截断标记留 1 字节」的语义：
// 改掉它就等于悄悄换了展示上限。契约在这里是**源码里那一个表达式**，于是直接判它。
// （同仓先例：internal/shell/shell_migration_test.go 用 AST 判导出形状而不是抄引用清单。）
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

// deleteRefMessage 用一条「被引用拒绝」的错误跑一次单条删除端点，取回响应里的 message。
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
