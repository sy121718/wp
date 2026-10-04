// ai_page_util.go — 后台页面的 HTMX 分档助手与视图装配。
//
// 形状照 internal/module/inventory/inbound/http/inventory_page_util.go（模块各写一份，
// 不跨模块共用）：写操作在同一个 handler 里按 HX-Request 分档 —— HTMX 请求回片段或
// HX-Redirect，原生表单回 303/302 重定向。这样表单**保留原生的 method/action**，
// 关掉 JS 也是完整可用的后台。
package aihttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/internal/web/shell"
)

// isHXRequest 判断是不是 htmx 发出的请求。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// redirectWhere 按请求类型选择跳转方式：HTMX 用 HX-Redirect（浏览器整页跳转），
// 原生表单用 302（PRG 的 P 部分）。
func redirectWhere(c *gin.Context, target string) {
	if isHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	// 303：POST 之后换成 GET 去看结果（PRG）。302 在部分客户端会被当成「重放原方法」，
	// 于是「提交一次」变成「提交两次」——表单页尤其明显。
	c.Redirect(http.StatusSeeOther, target)
}

// hxFragment 渲染一个可独立渲染的片段；非 HTMX 请求返回 false（调用方走整页重定向）。
func hxFragment(c *gin.Context, name string, data gin.H) bool {
	if !isHXRequest(c) {
		return false
	}
	c.HTML(http.StatusOK, name, data)
	return true
}

// parseInt64 宽容解析整数（空串 / 非法值回 0）。
func parseInt64(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// statusPtr 可选的「启用 / 停用」表单值：空串回 nil（= 让 service 用默认的启用），
// 有值才表态。非空但解析不出来按停用处理（fail-closed），不静默变启用。
func statusPtr(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v := aienums.StatusDisabled
	if parseInt64(raw) == int64(aienums.StatusEnabled) {
		v = aienums.StatusEnabled
	}
	return &v
}

// userID 当前登录用户 ID（写操作的 update_by / create_by）。
func userID(c *gin.Context) int64 {
	return int64(shell.CurrentUserID(c))
}

// modelRows 把模型目录转成模板用的行视图。
//
// 转换放在 Go 侧而不是模板里：模板只做「取字段 + isset 判断」，
// 「哪些输入类型勾上了」这类判断不该由模板表达式承担（错了是整页 500）。
func modelRows(models []aidto.ModelEntry) []gin.H {
	rows := make([]gin.H, 0, len(models))
	for i := range models {
		hasText, hasImage := false, false
		for _, t := range models[i].InputTypes {
			switch t {
			case aienums.InputTypeText:
				hasText = true
			case aienums.InputTypeImage:
				hasImage = true
			}
		}
		rows = append(rows, gin.H{
			"Index":           i,
			"ID":              models[i].ID,
			"DisplayName":     models[i].DisplayName,
			"ContextWindow":   models[i].ContextWindow,
			"MaxOutputTokens": models[i].MaxOutputTokens,
			"TextChecked":     hasText,
			"ImageChecked":    hasImage,
		})
	}
	return rows
}

// cardData 一个供应商卡片（含模型目录区）的模板数据。
//
// csrf_token / t / PermSet 从页面基座 map 里借（卡片是 include 进整页的，
// 作用域是这张卡片自己的 map，拿不到整页的键）。
func cardData(base gin.H, provider aidto.Provider, notice string, isErr bool, defaultBaseURL string) gin.H {
	return gin.H{
		"Provider":        provider,
		"Rows":            modelRows(provider.Models),
		"ProtocolOptions": aienums.ProtocolOptions,
		"DefaultBaseURL":  defaultBaseURL,
		"BuiltinKeys":     builtinProviderKeys(),
		"ModelsNotice":    notice,
		"ModelsNoticeErr": isErr,
		"csrf_token":      base["csrf_token"],
		"t":               base["t"],
		"PermSet":         base["PermSet"],
	}
}

// parseModelRows 从表单解析模型目录（表单字段同名多值，按出现顺序与行对齐）。
//
// 输入类型用**行序号后缀**（input_text_0 / input_image_1）：checkbox 只在勾选时提交，
// 靠「同名多值」的数组下标对齐会在「中间某行没勾」时整体错位。行序号由服务端渲染时
// 重新编号（每次增删行都整体重渲染），所以序号始终连续。
func parseModelRows(c *gin.Context) []aidto.ModelEntry {
	ids := c.PostFormArray("modelId")
	names := c.PostFormArray("modelDisplayName")
	windows := c.PostFormArray("modelContextWindow")
	maxOutputs := c.PostFormArray("modelMaxOutputTokens")
	rows := make([]aidto.ModelEntry, 0, len(ids))
	for i := range ids {
		types := make([]string, 0, 2)
		if c.PostForm("input_text_"+strconv.Itoa(i)) != "" {
			types = append(types, aienums.InputTypeText)
		}
		if c.PostForm("input_image_"+strconv.Itoa(i)) != "" {
			types = append(types, aienums.InputTypeImage)
		}
		rows = append(rows, aidto.ModelEntry{
			ID:              ids[i],
			DisplayName:     pick(names, i),
			ContextWindow:   parseInt64(pick(windows, i)),
			MaxOutputTokens: parseInt64(pick(maxOutputs, i)),
			InputTypes:      types,
		})
	}
	return rows
}

// pick 取数组第 i 项（越界回空串）。
func pick(items []string, i int) string {
	if i < 0 || i >= len(items) {
		return ""
	}
	return items[i]
}
