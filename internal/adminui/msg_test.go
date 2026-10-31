package adminui

import (
	"strings"
	"testing"
)

func TestMsgTextFillsArgsForKeylessMsg(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Fallback: "共 %s 项", Args: []string{"3"}}
	if got := MsgText(c, m); got != "共 3 项" {
		t.Errorf("MsgText = %q，want %q —— Key 为空时跳过了参数填充，与模板 tf 分叉", got, "共 3 项")
	}

	if got := TranslateArgs(c, m.Key, m.Fallback, m.Args); got != MsgText(c, m) {
		t.Errorf("MsgText(%q) 与 TranslateArgs(%q) 不一致 —— 两条出口会给出不同的句子", MsgText(c, m), got)
	}
}

func TestMsgTextFillsArgsOnMissingEntry(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Key: "admin.msg_test.absent_key", Fallback: "已处理 %s 条", Args: []string{"7"}}
	if got := MsgText(c, m); got != "已处理 7 条" {
		t.Errorf("MsgText = %q，want %q", got, "已处理 7 条")
	}
}

func TestMsgTextKeepsPlainTextWithoutArgs(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Key: "admin.msg_test.absent_key", Fallback: "折扣 100% 已生效"}
	if got := MsgText(c, m); got != "折扣 100% 已生效" {
		t.Errorf("MsgText = %q，want %q", got, "折扣 100% 已生效")
	}
}

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

func TestMsgTextFillsNamedArgs(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Fallback: "已删除 {n} 个页面。", Named: map[string]string{"n": "3"}}
	if got := MsgText(c, m); got != "已删除 3 个页面。" {
		t.Errorf("MsgText = %q，want %q", got, "已删除 3 个页面。")
	}
}

func TestMsgTextFillsNamedArgsOnMissingEntry(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Key: "admin.msg_test.absent_key", Fallback: "上架 {days} 天内", Named: map[string]string{"days": "7"}}
	if got := MsgText(c, m); got != "上架 7 天内" {
		t.Errorf("MsgText = %q，want %q", got, "上架 7 天内")
	}
}

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

func TestMsgTextNamedArgWithWrongParamName(t *testing.T) {
	c := errFactCtx(t)
	m := Msg{Fallback: "已删除 {n} 个页面。", Named: map[string]string{"wrongName": "3"}}
	if got := MsgText(c, m); !strings.Contains(got, "{n}") {
		t.Errorf("MsgText = %q —— 参数名拼错时不再保留兜底原文，确认是有意改动", got)
	}
}
