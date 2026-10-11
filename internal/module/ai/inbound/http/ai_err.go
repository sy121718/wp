// ai_err.go — ai 模块页面侧的错误文案归口。
//
// **判据只有一条**：`adminui.BusinessErrFact`（内部是 pkg/response.IsBusinessError 的形态判定：
// `模块.类别.语义` 的 key，或 `Err*` / `Msg*` 常量名）。本模块原先拿 `aienums.FacingMessages`
// （一张 key → 中文兜底的表）**兼任**了「可透出性判据」—— 那是**第二份真相**：service 换了 key、
// 或某个 key 改由别的模块提供，白名单就静默失配，两种症状都不报错（把底层错误串当文案透出、
// 或把业务文案吞成归口）。本批把判据交给共享实现，FacingMessages 只留「词条缺失时页面上
// 显示什么」这一件事（与 page 模块的 pageenums.ErrDetailFallbacks 同一性质）。
//
// 为什么不直接把中文写进 service：文案与语言相关，service 层只产出 key（见 enums 头注释）；
// 页面这一层负责「key → 当前语言的文案」的翻译与兜底。
//
// 控制器**不再取词**：给模板 / 提示页的是**事实**（adminui.Msg：key + 中文兜底 + 参数），
// 取词由模板注入的 t / tf 在渲染期做（见 adminui.Prepare）；只有片段槽位还是文本的那些地方
// 才在 Go 侧取成文本，并逐处注明「还没取词化」。
//
// 页面写动作的结论由 shell.RenderJump 渲染（见 ai_page.go 的 pageNotice / sessionNotice），
// 文案是事实、渲染在提示页里，不再经 ?err= / ?done= 回带。
package aihttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/logger"
)

// aiErrScene 结构化日志的场景名（ai 模块的页面错误都记在这里）。
const aiErrScene = "ai"

// aiErrKey 把 service 返回的错误归口成**可透出的业务 key**（未命中 → ErrInternal）。
//
// 页面侧一律用 key 流转，渲染时再按请求语言取词（片段与提示页共用这一条）。
// 未命中的原文只进日志：它可能带表名、SQLSTATE 与网络库原文，而响应（含提示页）不是可信边界。
func aiErrKey(err error) string {
	if err == nil {
		return ""
	}
	if fact, ok := adminui.BusinessErrFact(err); ok {
		return fact.Key
	}
	logger.Scene(aiErrScene).Error(err, "ai 模块页面错误")
	return aienums.ErrInternal
}

// aiErrFallback key → 中文兜底（FacingMessages 现在是**兜底表**，不再是可透出性判据）。
//
// 表里没有的 key 回归口文案而不是裸 key：`ai.err.xxx` 摆在页面上与「系统坏了」没有区别，
// 而归口文案至少是一句人话 —— 缺的是词条，不是判据。
func aiErrFallback(key string) string {
	if text, ok := aienums.FacingText(key); ok {
		return text
	}
	if text, ok := aienums.FacingText(aienums.ErrInternal); ok {
		return text
	}
	return "操作失败，请稍后重试"
}

// aiMsgText 一条文案**事实** → 当前语言的文本（key 缺失时回落中文兜底）。
//
// 只有「槽位契约还是字符串」的调用点才用它：控制器不再直接调 shell.TranslateFor，
// 取词一律经由共享的 adminui.MsgText（与模板注入的 tf 是同一实现）。
func aiMsgText(c *gin.Context, key, fallback string) string {
	return adminui.MsgText(c, adminui.Msg{Key: key, Fallback: fallback})
}

// aiErrText 把 service 返回的错误翻成**当前语言、可直接显示**的文案。
func aiErrText(c *gin.Context, err error) string {
	key := aiErrKey(err)
	if key == "" {
		return ""
	}
	return aiMsgText(c, key, aiErrFallback(key))
}

// internalFallback 归口文案（未命中的错误统一显示它）。
func internalFallback(c *gin.Context) string { return facingText(c, aienums.ErrInternal) }
