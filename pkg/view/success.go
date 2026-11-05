package view

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
)

// SuccessFact 是写操作成功的唯一事实：一条可取自 i18n 的消息。
//
// 它明确不携带模板名、业务 data、实体 ID、HTML、URL、DOM 选择器或导航配置。
// 需要更新列表、展示实体链接等，走对应业务视图，不混进通用提示协议。
type SuccessFact struct {
	Key      string
	Fallback string
	Args     []string
}

// SuccessWriter 把成功事实交给已装配的统一成功出口（共享 Jet 提示组件）。
type SuccessWriter func(c *gin.Context, fact SuccessFact)

const (
	ctxSuccessWriter  = "view.success.writer"
	ctxSuccessWritten = "view.success.written"
)

// UseSuccess 在路由装配处注册唯一的成功出口。
func UseSuccess(write SuccessWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if write != nil {
			c.Set(ctxSuccessWriter, write)
		}
		c.Next()
	}
}

// Success 输出默认“操作成功”提示。
func Success(c *gin.Context) {
	SuccessWith(c, SuccessFact{})
}

// SuccessMessage 输出一条明确的业务消息；非空 Key 由统一提示组件取词。
func SuccessMessage(c *gin.Context, key, fallback string, args ...string) {
	SuccessWith(c, SuccessFact{Key: key, Fallback: fallback, Args: args})
}

// SuccessWith 把成功事实交给统一出口；未装配时按失败处理，不静默成功。
func SuccessWith(c *gin.Context, fact SuccessFact) {
	if c == nil {
		return
	}
	if c.Writer.Written() || c.GetBool(ctxSuccessWritten) {
		return
	}
	raw, ok := c.Get(ctxSuccessWriter)
	if ok {
		if write, ok := raw.(SuccessWriter); ok && write != nil {
			c.Set(ctxSuccessWritten, true)
			c.Header("Cache-Control", "no-store")
			write(c, fact)
			return
		}
	}
	FailScene(c, "view", errors.New("成功出口未装配：路由缺 SuccessWriter"))
}

// FailMessage 输出一条明确的业务消息（失败出口），按真实业务状态码渲染。
//
// 它对应 SuccessMessage 的失败侧：调用方只交消息事实，
// 不直接操作状态码与模板；scene 只进日志与失败事件的归类。
func FailMessage(c *gin.Context, scene, key, fallback string, args ...string) {
	key = strings.TrimSpace(key)
	fallback = strings.TrimSpace(fallback)
	// 没有登记 key 时用兜底文案本身作 key：它仍是明确的业务消息，
	// 走 4xx 业务失败语义，而不是被当成未知内部错误落到 500。
	if key == "" {
		if fallback == "" {
			FailScene(c, scene, facing.NewMessage("uiInternalErr", "系统内部错误，请稍后重试"))
			return
		}
		FailScene(c, scene, facing.NewMessage(fallback, fallback))
		return
	}
	msg := key
	if len(args) > 0 {
		msg += "|" + strings.Join(args, "|")
	}
	FailScene(c, scene, facing.NewMessage(msg, fallback))
}
