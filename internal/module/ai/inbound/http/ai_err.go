// ai_err.go — ai 模块页面侧的错误文案归口。
//
// 与 sysconfig 的 sysconfig_err.go 同口径：**只有登记在 enums.FacingMessages 里的
// key** 才会被显示，未登记的一律归口到 ErrInternal（原文只进日志）—— 否则底层错误串
// （SQL、网络库、文件路径）会被原样渲染到界面上。
//
// 为什么不直接把中文写进 service：文案与语言相关，service 层只产出 key（见 enums 头注释）；
// 页面这一层负责「key → 当前语言的文案」的翻译与兜底。
//
// 页面写动作的结论由 shell.RenderJump 渲染（见 ai_page.go 的 pageNotice / sessionNotice），
// 文案在这里翻成当前语言后再交给提示页，不再经 ?err= / ?done= 回带。
package aihttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/logger"
)

// aiErrKey 把 service 返回的错误归口成**已登记的白名单 key**（未登记 → ErrInternal）。
//
// 页面侧一律用 key 流转，渲染时再按请求语言取词（片段与提示页共用这一条）。
func aiErrKey(err error) string {
	if err == nil {
		return ""
	}
	key := strings.TrimSpace(err.Error())
	if _, ok := aienums.FacingText(key); ok {
		return key
	}
	logger.Scene("ai").Error(err, "ai 模块页面错误")
	return aienums.ErrInternal
}

// aiErrText 把 service 返回的错误翻成**当前语言、可直接显示**的文案。
func aiErrText(c *gin.Context, err error) string {
	key := aiErrKey(err)
	if key == "" {
		return ""
	}
	return facingText(c, key)
}

// internalFallback 归口文案（未登记的错误统一显示它）。
func internalFallback(c *gin.Context) string { return facingText(c, aienums.ErrInternal) }
