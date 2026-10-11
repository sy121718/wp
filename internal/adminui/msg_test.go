package adminui

// msg_test.go — 「Go 侧取词」与「渲染期取词」必须是同一个字符串。
//
// 同一个事实（facing.Msg）会在两条路上被渲染：
//   - 模板：注入的 `tf`（internal/templates 的 tl 函数）+ `tr`；
//   - Go 侧：MsgText —— 给 JSON 出口、拼串 HTML、以及还没有模板槽位的片段用。
//
// 两条路曾经分叉：MsgText 在 Key 为空时**直接返回 Fallback**（跳过 `%s` 填充），
// 而 tf 无论 Key 是否为空都走 TranslateArgs（先取词、再按 `%s` 填充）。
// 后果是同一个 Msg 在接口响应里是「共 %s 项」、在页面上是「共 3 项」——
// 两条出口给出不同的句子，而两边都不报错、也不进日志。
//
// 这几条是会变红的判据：把 MsgText 改回「Key 为空就 return m.Fallback」，
// 第 ① 条立刻红。

import (
	"strings"
	"testing"
)

// ① Key 为空（成品串档）+ 有参数：`%s` 必须被填上，与模板 tf 同口径。
func TestMsgTextFillsArgsForKeylessMsg(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Fallback: "共 %s 项", Args: []string{"3"}}
	if got := MsgText(c, m); got != "共 3 项" {
		t.Errorf("MsgText = %q，want %q —— Key 为空时跳过了参数填充，与模板 tf 分叉", got, "共 3 项")
	}
	// 同一实现（TranslateArgs）也服务模板的 tf，所以这里断言它就是同口径的证据。
	if got := TranslateArgs(c, m.Key, m.Fallback, m.Args); got != MsgText(c, m) {
		t.Errorf("MsgText(%q) 与 TranslateArgs(%q) 不一致 —— 两条出口会给出不同的句子", MsgText(c, m), got)
	}
}

// ② Key 非空但词条缺席：回落 Fallback 后同样要填参数。
func TestMsgTextFillsArgsOnMissingEntry(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Key: "admin.msg_test.absent_key", Fallback: "已处理 %s 条", Args: []string{"7"}}
	if got := MsgText(c, m); got != "已处理 7 条" {
		t.Errorf("MsgText = %q，want %q", got, "已处理 7 条")
	}
}

// ③ 没有参数时原样输出（不要因为走了 Sprintf 而在含 `%` 的成品串上出岔子）。
func TestMsgTextKeepsPlainTextWithoutArgs(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Key: "admin.msg_test.absent_key", Fallback: "折扣 100% 已生效"}
	if got := MsgText(c, m); got != "折扣 100% 已生效" {
		t.Errorf("MsgText = %q，want %q", got, "折扣 100% 已生效")
	}
}

// ④ Empty 的两半都要为空 —— 失败分支靠它决定要不要给归口文案
// （只判 Key 会把「成品串档」的空事实当成「有内容」）。
func TestMsgEmpty(t *testing.T) {
	if !(Msg{}).Empty() {
		t.Errorf("零值 Msg 应是 Empty")
	}
	if (Msg{Fallback: "有话说"}).Empty() {
		t.Errorf("只有 Fallback 的 Msg 不是 Empty")
	}
	if (Msg{Key: "a.b.c"}).Empty() {
		t.Errorf("只有 Key 的 Msg 不是 Empty")
	}
}

// ⑤ 具名参数：`{n}` 按名字填，语序由词条决定（不是调用方）。
func TestMsgTextFillsNamedArgs(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Fallback: "已删除 {n} 个页面。", Named: map[string]string{"n": "3"}}
	if got := MsgText(c, m); got != "已删除 3 个页面。" {
		t.Errorf("MsgText = %q，want %q", got, "已删除 3 个页面。")
	}
}

// ⑥ 词条缺席 + 具名参数：回落兜底原文后仍要填上（与位置参数同口径）。
func TestMsgTextFillsNamedArgsOnMissingEntry(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Key: "admin.msg_test.absent_key", Fallback: "上架 {days} 天内", Named: map[string]string{"days": "7"}}
	if got := MsgText(c, m); got != "上架 7 天内" {
		t.Errorf("MsgText = %q，want %q", got, "上架 7 天内")
	}
}

// ⑦ 两种参数都给时**用具名**：判据是「语序归谁」，不是「哪个先填」。
func TestMsgTextPrefersNamedOverPositional(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{
		Fallback: "共 {n} 项",
		Args:     []string{"9"},
		Named:    map[string]string{"n": "3"},
	}
	if got := MsgText(c, m); got != "共 3 项" {
		t.Errorf("MsgText = %q，want %q —— 具名参数应优先", got, "共 3 项")
	}
}

// ⑧ 参数名拼错（词条与兜底两处都填不干净）时返回**尽力填过的兜底原文** —— 带 `{n}`
// 字面量，但比空串好（空串会让失败在页面上完全不可见）。
//
// 写成测试是让「参数名拼错」有一个会变红的地方，不是说这个行为是设计目标：设计目标
// 那条是「词条被改坏 → 回落兜底原文，而那里能填干净」，由 pkg/i18n 的
// TestFillTranslateFallsBackOnBrokenEntry 钉住。
func TestMsgTextNamedArgWithWrongParamName(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Fallback: "已删除 {n} 个页面。", Named: map[string]string{"wrongName": "3"}}
	if got := MsgText(c, m); !strings.Contains(got, "{n}") {
		t.Errorf("MsgText = %q —— 参数名拼错时不再保留兜底原文，确认是有意改动", got)
	}
}
