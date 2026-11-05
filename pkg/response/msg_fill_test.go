package response

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
)

func TestMsgTextFillsArgsForKeylessMsg(t *testing.T) {
	c := errFactCtx(t)
	m := facing.Msg{Fallback: "共 %s 项", Args: []string{"3"}}
	if got := MsgText(c, m); got != "共 3 项" {
		t.Errorf("MsgText = %q，want %q —— Key 为空时跳过了参数填充，与模板 tf 分叉", got, "共 3 项")
	}

	if got := ArgsText(c, m.Key, m.Fallback, m.Args); got != MsgText(c, m) {
		t.Errorf("MsgText(%q) 与 ArgsText(%q) 不一致 —— 两条出口会给出不同的句子", MsgText(c, m), got)
	}
}

func TestMsgTextFillsArgsOnMissingEntry(t *testing.T) {
	c := errFactCtx(t)
	m := facing.Msg{Key: "admin.msg_test.absent_key", Fallback: "已处理 %s 条", Args: []string{"7"}}
	if got := MsgText(c, m); got != "已处理 7 条" {
		t.Errorf("MsgText = %q，want %q", got, "已处理 7 条")
	}
}

func TestMsgTextKeepsPlainTextWithoutArgs(t *testing.T) {
	c := errFactCtx(t)
	// 纯文本消息（没有词条、只有 Fallback）照样原样上屏 —— 这是「消息事实」的契约。
	// 文案里不能有裸百分号：不含 %s 占位符的句子一旦出现孤立 %，ArgsText 会判
	// 「参数填不进去」，为免 fmt 把 %!s(MISSING) 之类标记打上屏而退成归口文案。
	// 那条防护的定向用例是下面的 TestMsgTextFallsBackWhenTextIsNotFillable。
	m := facing.Msg{Key: "admin.msg_test.absent_key", Fallback: "折扣已生效"}
	if got := MsgText(c, m); got != "折扣已生效" {
		t.Errorf("MsgText = %q，want %q", got, "折扣已生效")
	}
}

func TestMsgTextFallsBackWhenTextIsNotFillable(t *testing.T) {
	c := errFactCtx(t)
	// 反向守卫：含孤立百分号的文案不能被原样交给 fmt.Sprintf（会打出 %!s(MISSING)
	// 这类标记），此时退成共享归口文案才是对的。
	m := facing.Msg{Key: "admin.msg_test.absent_key", Fallback: "折扣 100% 已生效"}
	got := MsgText(c, m)
	if strings.Contains(got, "%") {
		t.Fatalf("message 里残留了百分号形态的 fmt 标记：%q", got)
	}
	if got == "折扣 100% 已生效" {
		t.Fatalf("不可填写的文案不该原样上屏：%q", got)
	}
	if got != InternalFallback {
		t.Fatalf("应退成共享归口文案，实际 %q", got)
	}
}

func TestMsgEmpty(t *testing.T) {
	if !(facing.Msg{}).Empty() {
		t.Errorf("零值 facing.Msg 应是 Empty")
	}
	if (facing.Msg{Fallback: "有话说"}).Empty() {
		t.Errorf("只有 Fallback 的 facing.Msg 不是 Empty")
	}
	if (facing.Msg{Key: "a.b.c"}).Empty() {
		t.Errorf("只有 Key 的 facing.Msg 不是 Empty")
	}
}

func TestMsgTextFillsNamedArgs(t *testing.T) {
	c := errFactCtx(t)
	m := facing.Msg{Fallback: "已删除 {n} 个页面。", Named: map[string]string{"n": "3"}}
	if got := MsgText(c, m); got != "已删除 3 个页面。" {
		t.Errorf("MsgText = %q，want %q", got, "已删除 3 个页面。")
	}
}

func TestMsgTextFillsNamedArgsOnMissingEntry(t *testing.T) {
	c := errFactCtx(t)
	m := facing.Msg{Key: "admin.msg_test.absent_key", Fallback: "上架 {days} 天内", Named: map[string]string{"days": "7"}}
	if got := MsgText(c, m); got != "上架 7 天内" {
		t.Errorf("MsgText = %q，want %q", got, "上架 7 天内")
	}
}

func TestMsgTextPrefersNamedOverPositional(t *testing.T) {
	c := errFactCtx(t)
	m := facing.Msg{
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
	m := facing.Msg{Fallback: "已删除 {n} 个页面。", Named: map[string]string{"wrongName": "3"}}
	if got := MsgText(c, m); !strings.Contains(got, "{n}") {
		t.Errorf("MsgText = %q —— 参数名拼错时不再保留兜底原文，确认是有意改动", got)
	}
}

func errFactCtx(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/admin/pages/create", nil)
	return c
}
