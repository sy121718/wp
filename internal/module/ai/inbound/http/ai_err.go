// ai_err.go — ai 模块页面侧的错误文案归口。
//
// 与 sysconfig 的 sysconfig_err.go 同口径：**只有登记在 enums.FacingMessages 里的
// key** 才会被显示，未登记的一律归口到 ErrInternal（原文只进日志）—— 否则底层错误串
// （SQL、网络库、文件路径）会被原样渲染到界面上。
//
// 为什么不直接把中文写进 service：文案与语言相关，service 层只产出 key（见 enums 头注释）；
// 页面这一层负责「key → 当前语言的文案」的翻译与兜底。
//
// PRG 回执（?err= / ?done=）里传的是 **key 本身**而不是译文 —— 见 facingQuery。
package aihttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/logger"
)

// aiErrKey 把 service 返回的错误归口成**已登记的白名单 key**（未登记 → ErrInternal）。
//
// 页面侧一律用 key 流转（HTMX 片段渲染时再翻译、PRG 回执把 key 写进 query）：
// 若这里返回译文，非 zh-CN 语言下写进 query 的译文在渲染侧对不上白名单 —— 提示会静默消失。
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

// facingQuery 读取 PRG 的一次性提示（`?err=` / `?done=`）。
//
// 与旧实现的关键差别：query 值是 **key**（不是译文），因此
//
//	· 白名单比对按 key 做 —— 用户构造的任意文本（哪怕是某个 key 的中文值）都无法命中，
//	  注入面比「按译文比对」更小；
//	· 语言无关 —— 非 zh-CN 语言下也不会因为译文对不上而把提示丢掉；
//	· 支持随行的计数参数（`?doneN=` / `?doneM=`），文案里的 `{n}` / `{m}` 在这里填充。
func facingQuery(c *gin.Context, slot string) string {
	key := strings.TrimSpace(c.Query(slot))
	if key == "" {
		return ""
	}
	if _, ok := aienums.FacingText(key); !ok {
		return ""
	}
	return facingTextParams(c, key, queryFacingParams(c, slot))
}

// queryFacingParams 读取随 PRG 提示一起回传的计数参数（`{slot}N` / `{slot}M`）。
func queryFacingParams(c *gin.Context, slot string) map[string]string {
	params := make(map[string]string, 2)
	for _, name := range []string{"n", "m"} {
		if v := strings.TrimSpace(c.Query(slot + upperFirst(name))); v != "" {
			params[name] = v
		}
	}
	return params
}
