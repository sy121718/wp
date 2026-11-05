package shell

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/safepath"
	"go_wp/pkg/view"
)

// Jump 描述一次写操作的结果事实。
//
// 字段保留是为了不打断现有调用点；终态语义已变：
// 成功走统一提示组件，失败走共享失败出口，不再渲染整页跳转提示，
// 也不再使用 meta refresh 倒计时自动跳转。
type Jump struct {
	OK bool

	MsgKey string

	Msg string

	MsgArgs []string

	// Back / BackText / Seconds 保留为业务事实：当前统一出口不再用它们，
	// 但调用点的回跳意图是真实业务信息，由发起模板的 data-write-nav 消费。
	Back string

	BackTextKey string

	BackText string

	Seconds int
}

// RenderJump 把写操作结果交给统一反馈出口。
//
// 成功：只输出一条提示（默认“操作成功”），刷新/关闭/导航由发起页面声明、共享前端基座执行。
// 失败：交共享失败出口，按真实业务状态码输出受控错误区。
func RenderJump(c *gin.Context, j Jump) {
	if c == nil {
		return
	}
	if j.OK {
		view.SuccessWith(c, view.SuccessFact{Key: j.MsgKey, Fallback: j.Msg, Args: cleanArgs(j.MsgArgs)})
		return
	}
	view.FailScene(c, sceneOfJump(j), jumpError(j))
}

func jumpError(j Jump) error {
	key := strings.TrimSpace(j.MsgKey)
	msg := strings.TrimSpace(j.Msg)
	if key == "" && msg == "" {
		return facing.NewMessage("uiInternalErr", "系统内部错误，请稍后重试")
	}
	if key == "" {
		return facing.NewMessage(msg, msg)
	}
	if len(j.MsgArgs) > 0 {
		key += "|" + strings.Join(cleanArgs(j.MsgArgs), "|")
	}
	return facing.NewMessage(key, msg)
}

func sceneOfJump(j Jump) string {
	if j.MsgKey != "" {
		if i := strings.IndexByte(j.MsgKey, '.'); i > 0 {
			return j.MsgKey[:i]
		}
	}
	return "shell"
}

func cleanArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, strings.TrimSpace(a))
	}
	return out
}

// SafeBack 保留安全回跳判定，供仍需回跳链接的业务视图使用。
func SafeBack(c *gin.Context, back, fallback string) string {
	if strings.TrimSpace(back) == "" || !safepath.IsSameOriginPath(back) {
		return fallback
	}
	return back
}
