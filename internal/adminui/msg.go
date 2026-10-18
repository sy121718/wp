package adminui

// msg.go — 文案事实在**后台页面出口**这一侧的名字与取词。
//
// 形状本身（key + 中文兜底 + `%s` 位置参数）定义在 **pkg/facing**：它是跨出口的约定 ——
// JSON 接口、后台页面、访客页判的是同一件事，把它放进本包等于某一条出口独占了一份公共契约
// （第二条出口就会再留一份自己的说法）。本文件只剩两件事：
//
//	① 别名 —— 迁移期兼容：存量 100+ 处写的是 adminui.Msg；**新代码直接用 facing.Msg**，
//	   名字要说明它是什么（一句文案的事实），而不是它被谁先用上；
//	② 取词 —— MsgText / TranslateArgs 需要**请求语言**，那是出口才有的东西（facing 零依赖，
//	   拿不到 gin.Context，也不该拿到）。
//
// 契约与三段判据（Key 空 = Fallback 即成品串、Fallback 不给空串、Args 是已 strconv 的字符串）
// 见 pkg/facing/msg.go，本文件不重复。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
	"go_wp/pkg/facing"
)

// Msg 一句可展示文案的事实（定义与契约见 pkg/facing）。
type Msg = facing.Msg

// MsgText 把事实取成当前语言文本（局部槽位 / **JSON 出口**这两类「模板不再取词」的地方用）。
//
// 实现与模板注入的 `tf` **同一份**（TranslateArgs）：这样「Go 侧取词」与「渲染期取词」
// 对同一个事实给出同一个字符串。曾经这里对 Key 为空的事实直接返回 Fallback（跳过 `%s` 填充），
// 于是同一个 Msg 在 JSON 出口不填参数、在页面上填 —— 两条出口给出不同的句子，而都不报错。
// 参数按**填了哪一种**分派：有具名参数走 TranslateFill（语序归词条），否则走 TranslateArgs
// （`%s` 位置，语序归调用方）。分派不按 Key 是否为空判 —— 那是本文件开头记的那次分叉。
func MsgText(c *gin.Context, m Msg) string {
	return text.MsgText(c, m)
}

// InternalMsg 归口文案的事实（拿不到具体 err 的失败分支用）：页面不能显示空串。
func InternalMsg() Msg {
	f := InternalErrorFact()
	return Msg{Key: f.Key, Fallback: f.Fallback}
}
