package adminhttp

// admin_page_err_text_test.go — 页面提示参数（?err= / ?errored=）受控出口的单测。
//
// 为什么单独钉这一层：这 8 个 admin 页面此前把 c.Query("err") 原样塞进模板。值现在都由服务端
// 构造，但**页面不是可信边界** —— `?err=任意文本` 谁都能手写，渲染出来就是一条顶着「系统提示」
// 样式的伪造消息（Jet 已做 HTML 转义，所以不是 XSS；问题是「看起来像系统说的话」），
// 而超长参数会把提示条撑破页面。
//
// 出口分两层，本文件把第一层钉细（名字与实现一一对应）：
//
//   · adminPageErrParamClean —— 第一道**形状**清洗（控制字符 / 首尾空白 / 200 字节截断），
//     不含任何文案白名单：它只回答「这个串能不能安全进模板」（本文件的主体）；
//   · adminPageErrText(c, raw) —— 清洗 + adminErrTexts(c) 白名单**整体**匹配，未命中落空串：
//     它回答「这句话是不是系统真的说过的」。
//
// 第二层的双向对账（真实串透出 / 伪造串落空 / 候选不超限 / 写侧来源）在 admin_err_texts_test.go。

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAdminPageErrParamCleanKeepsNormalCopy 第一道清洗不改写任何形状合法的文本 —— 它不是归口。
func TestAdminPageErrParamCleanKeepsNormalCopy(t *testing.T) {
	cases := []string{
		"角色编码已存在",
		"已删除 12 个角色，3 个未能删除（受保护或被引用）",
		"Entry key is required",
		"a.b · zh-CN",
	}
	for _, want := range cases {
		if got := adminPageErrParamClean(want); got != want {
			t.Errorf("正常文案不应被改写：got %q want %q", got, want)
		}
	}
}

// TestAdminPageErrParamCleanStripsControlChars 控制字符一律去掉：它们能把一行提示拆成多行、
// 伪造出第二条「系统消息」的观感，也是终端 / 日志注入的常见载体。
func TestAdminPageErrParamCleanStripsControlChars(t *testing.T) {
	got := adminPageErrParamClean("\n\t系统提示：已保存\r \x00\x1b[31mred\x7f")
	if strings.ContainsAny(got, "\n\t\r\x00\x1b\x7f") {
		t.Fatalf("控制字符应被全部去掉，got %q", got)
	}
	if got != "系统提示：已保存 [31mred" {
		t.Fatalf("清洗后应只留下可见文本（并 trim），got %q", got)
	}
}

// TestAdminPageErrParamCleanTrimsBlank 首尾空白（含全角空格）被去掉；纯空白退化成空串。
func TestAdminPageErrParamCleanTrimsBlank(t *testing.T) {
	if got := adminPageErrParamClean("   角色编码已存在\n"); got != "角色编码已存在" {
		t.Fatalf("应 trim 首尾空白与换行，got %q", got)
	}
	if got := adminPageErrParamClean("   \t\n  "); got != "" {
		t.Fatalf("纯空白应退化成空串，got %q", got)
	}
}

// TestAdminPageErrParamCleanTruncatesLongASCII 超长 ASCII 截断到 200 字节并补省略号。
//
// 这是第一道（形状）的边界，不是最终出口：截断后的串必然对不上任何候选，
// 所以 adminPageErrText 对超长参数的结果同样是空串（见同文件 TestAdminPageErrTextRejectsOversized）。
func TestAdminPageErrParamCleanTruncatesLongASCII(t *testing.T) {
	got := adminPageErrParamClean(strings.Repeat("A", 5000))
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("截断后应补省略号，got len=%d", len(got))
	}
	if len(got) != adminPageErrParamMaxBytes+len("…") {
		t.Fatalf("截断长度应是 %d 字节 + 省略号，got %d", adminPageErrParamMaxBytes, len(got))
	}
}

// TestAdminPageErrParamCleanTruncatesOnRuneBoundary 按字节截断不能劈开多字节字符。
//
// 「已删除 12 个角色…」这类服务端文案本身是中文：200 字节不是 3 的整数倍，
// 直接切 slice 会把第 67 个汉字切成两半，页面上就会多出一个替换字符（）。
func TestAdminPageErrParamCleanTruncatesOnRuneBoundary(t *testing.T) {
	got := adminPageErrParamClean(strings.Repeat("汉", 500))
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果必须是合法 UTF-8，got %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("截断后应补省略号，got %q", got)
	}
	body := strings.TrimSuffix(got, "…")
	if len(body) > adminPageErrParamMaxBytes || len(body)%3 != 0 {
		t.Fatalf("截断点应回退到最近的字符边界，body len=%d", len(body))
	}
}

// TestAdminPageErrParamCleanCleansBeforeTruncating 先清洗后截断：
// 否则前 200 字节全是控制字符时，清洗完剩下的内容反而比该有的少。
func TestAdminPageErrParamCleanCleansBeforeTruncating(t *testing.T) {
	got := adminPageErrParamClean(strings.Repeat("\n", 300) + strings.Repeat("B", 300))
	if strings.ContainsAny(got, "\n") {
		t.Fatalf("控制字符应被去掉，got %q", got)
	}
	if !strings.HasPrefix(got, "B") || !strings.HasSuffix(got, "…") {
		t.Fatalf("应先清洗再截断，got %q", got)
	}
}

// —— 第二层：白名单整体匹配（adminPageErrText） ——
//
// 真实串透出与伪造串落空是一对：只钉一侧都会漏（理由见 admin_err_texts_test.go 的文件头）。
// 素材一律从写侧的构造函数取，不手抄文案 —— 手抄的第二份真相不会跟着写侧改措辞。

// TestAdminPageErrTextAcceptsBulkPartial 列表页「部分成功」的结论必须原样透出。
func TestAdminPageErrTextAcceptsBulkPartial(t *testing.T) {
	c := newPageErrContext(t)
	for _, noun := range adminBulkNouns {
		loc := adminBulkResultURL("/admin/x", noun, 3, 2)
		parsed, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("写侧回跳地址无法解析：%v", err)
		}
		raw := parsed.Query().Get("err")
		if raw == "" {
			t.Fatalf("有跳过时应走 ?err=（noun=%q）：%s", noun, loc)
		}
		if got := adminPageErrText(c, raw); got != raw {
			t.Errorf("写侧的批量结论应原样透出，got %q（raw=%q）", got, raw)
		}
	}
}

// TestAdminPageErrTextRejectsForged 手拼的文案一律落空串。
//
// 不落归口文案是刻意的：那会给手拼参数凭空造出一条「系统提示」，等于替攻击者的话背书。
// 与 ?done= / ?saved= 的取舍一致 —— 系统文案必须是系统真的说过的话。
func TestAdminPageErrTextRejectsForged(t *testing.T) {
	c := newPageErrContext(t)
	for _, raw := range []string{
		"",
		"   ",
		"系统维护中，请稍后重试",
		"<script>alert(1)</script>",
		"已删除 3 个用户", // 名词不在 adminBulkNouns 里
		"已删除 3 个角色，2 个未能删除（受保护）",      // 少半句
		"已删除 3 个角色，2 个未能删除（受保护或被引用）。", // 多一个句号
	} {
		if got := adminPageErrText(c, raw); got != "" {
			t.Errorf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
		}
	}
}

// TestAdminPageErrTextRequiresWholeMatch 必须整体相等，不许用 Contains 绕过白名单。
//
// 形态 3（受控文案 + 「：」+ 定位信息）是 shell.FacingNotice 的既有判据 —— 服务端会在业务文案
// 后补一句定位信息，所以「…（受保护或被引用）」后面跟「：」是合法的；但**前缀 / 后缀夹带**
// 必须不命中，否则手拼 URL 就能变成「夹一段已知文案 + 任意内容」。
func TestAdminPageErrTextRequiresWholeMatch(t *testing.T) {
	c := newPageErrContext(t)
	for _, noun := range adminBulkNouns {
		msg := fmt.Sprintf(adminBulkPartialTemplate, 3, noun, 2)
		for _, forged := range []string{
			"<script>alert(1)</script>" + msg,
			msg + "<script>alert(1)</script>",
			"伪造前缀 " + msg,
		} {
			if got := adminPageErrText(c, forged); got != "" {
				t.Errorf("夹带内容不该命中（必须整体相等），实际 %q（noun=%q）", got, noun)
			}
		}
		if got := adminPageErrText(c, msg+"：外部编码 xyz"); got != msg+"：外部编码 xyz" {
			t.Errorf("「受控文案 + ：+ 定位信息」是 shell 的形态 3，应当放行，实际 %q", got)
		}
	}
}

// TestAdminPageErrTextRejectsOversized 超长参数落空串。
//
// 第一道清洗把它截断到 200 字节 + 省略号，截断后的串对不上任何候选 —— 两层合起来的结果是空串。
// 这条同时是「请求方不能决定我们扫多长」的边界回归（adminPageErrParamMaxBytes 的用例）。
func TestAdminPageErrTextRejectsOversized(t *testing.T) {
	c := newPageErrContext(t)
	for _, raw := range []string{
		strings.Repeat("A", 5000),
		strings.Repeat("已删除 3 个角色", 200),
		strings.Repeat("汉", 500),
	} {
		if got := adminPageErrText(c, raw); got != "" {
			t.Errorf("超长参数应落空串，实际 len=%d（raw len=%d）", len(got), len(raw))
		}
	}
}
