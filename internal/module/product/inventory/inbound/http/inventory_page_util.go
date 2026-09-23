// inventory_page_util.go — 后台库存页共用的表单小工具（页面专属，不进 API 路径）。
package inventoryhttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// parseIntOr 解析十进制整数，失败返回兜底值（后台表单容错，不因一个脏字段 500）。
func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

// —— HTMX 写表单的「分档出口」（本包各写表单共用；本文件是这一档位的唯一实现处）——
//
// 背景：本包各页的写表单失败时是 `302 + ?err=` 回列表页（见 inventoryErrURL），
// **用户刚填的内容全丢**。改造方向是路径 A「渐进增强」：表单保留原生
// `method="post" action=…`，另加 `hx-post`（htmx 优先拦住 submit）；服务端按
// `HX-Request` **分档**：
//
//	· htmx 请求：失败返 **200 + 片段**（错误槽 + 回填后的表单）—— 抽屉 / 页面原地留住输入；
//	· 原生请求：维持既有 `302 + ?err=`（无 JS 环境的回退行为一个字不改）。
//
// 这是**另一个包**，不复用 producthttp 的同款 helper：两个页面包之间没有共享依赖，
// 为两个小函数引入跨模块 import 会把「商品页改了会不会影响库存页」变成新的耦合面。
// 两边语义刻意逐字一致，改动时请一起改（分档口径分叉会表现为同一种失败在商品页留输入、
// 在库存页丢输入）。
//
// 文案仍然走本包既有设施：失败片段里的错误槽用 **inventoryErrText**（业务错误取词 +
// 结构化日志，非业务错误给归口文案），不要另开一套错误文案 —— 分档只改「响应形状」，
// 不改「错误怎么变成人话」。

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 判据与 redirectWhere 同口径（大小写不敏感、去首尾空白）：两个出口必须认同同一个信号。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// redirectWhere 页面写动作的 PRG 出口。
//
// 原生表单走 302；HTMX 请求走 **HX-Redirect**：htmx 的 XHR 会自己跟随 302，最终响应里
// 已经读不到 Location，只有响应头上的 HX-Redirect 能让它整页跳转（否则会把整页 HTML
// 塞进抽屉里）。两条路的终点是同一个 URL，页面壳与提示位完全一致。
//
// 用法：把既有的 `c.Redirect(http.StatusFound, target)` 换成 `redirectWhere(c, target)` ——
// 原生行为逐字不变，htmx 那一档才有正确的跳转；**不要**给写表单配 `hx-swap="none"`
// 之外的 hx-* 目标（跳转由响应头决定，不由 swap 决定）。
func redirectWhere(c *gin.Context, target string) {
	if isHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, target)
}

// hxFragment 只在 htmx 请求下渲染片段（200 + HTML），并报告「是否已经响应」。
//
// 用法（写表单失败分档的唯一写法）：
//
//	if hxFragment(c, "admin/<模块>/xxx_form.html", data) {   // 模块专属片段跟模块走（见 templates/CLAUDE.md）
//		return
//	}
//	redirectWhere(c, inventoryErrURL(c, inventoryWarehousesPath, projectID, err))
//
// 片段模板必须**可独立渲染**（不带 extends layout，data 由调用点给齐），并且自带
// htmx 需要的结构（错误槽 + 回填后的表单）—— 模板由各批自己建，本 helper 只管分档。
func hxFragment(c *gin.Context, name string, data gin.H) bool {
	if !isHXRequest(c) {
		return false
	}
	c.HTML(http.StatusOK, name, data)
	return true
}

// formEchoMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
const formEchoMemory = 32 << 20

// formEcho 这次请求的表单快照（失败片段回填用）。
//
// 值只来自**这次提交**，不回查数据库：回填要的是「用户刚打的字」，而不是「库里的旧值」
// （新建 / 行内编辑路径上后者会把人刚改的内容顶掉，比不回填更糟）。
type formEcho struct {
	// values 归一化后的提交值：去首尾空白、丢空项（与 shell.FieldValue 同口径）。
	// 多值字段保留**提交顺序** —— 顺序在本域有语义（多仓勾选按表内顺序取认领仓），
	// 排序或去重都会改掉它。
	values url.Values
}

// formEchoFrom 取这次请求的表单快照。
func formEchoFrom(c *gin.Context) formEcho {
	vals := url.Values{}
	if c == nil || c.Request == nil {
		return formEcho{values: vals}
	}
	// 走标准库的解析入口（gin 的 PostForm 也走这里）：urlencoded 与 multipart 两条路径
	// 都会把字段填进 req.PostForm。解析错误一律忽略 —— 解析失败就当「没提交」，
	// 回填退化成空表单；提交本身合法与否由各 handler 的业务校验回答，不在这里变成 500。
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	for key, list := range c.Request.PostForm {
		for _, raw := range list {
			if v := strings.TrimSpace(raw); v != "" {
				vals.Add(key, v)
			}
		}
	}
	return formEcho{values: vals}
}

// value 取单值字段的回填值（缺失返回空串）：文本 / 下拉 / 隐藏域用。
func (e formEcho) value(name string) string {
	if list := e.values[name]; len(list) > 0 {
		return list[0]
	}
	return ""
}

// list 取多值字段的完整回填值（复选框组 / 同名多次提交），保留提交顺序。
func (e formEcho) list(name string) []string {
	return e.values[name]
}

// checked 该字段这次是否被提交过（复选框的回填判据）。
//
// 判据是「字段在提交里存在」：复选框只有被勾选才会提交 —— 所以勾选态用 checked，
// 文本值用 value，两者不要混用（文本框的值可能是空串，那不代表「没填过」）。
func (e formEcho) checked(name string) bool {
	return len(e.values[name]) > 0
}

// formEchoData 把回填值摊成片段 data 可直接并入的三个键。
//
//	"FormEcho"        gin.H{字段名: string}   文本 / 下拉 / 隐藏域 → value="{{ .FormEcho.code }}"
//	"FormEchoChecked" gin.H{字段名: bool}     复选框 → {{if .FormEchoChecked.isDefault}}checked{{end}}
//	"FormEchoMulti"   gin.H{字段名: []string} 多值字段的完整提交值（保序）
//
// **键名带 FormEcho 前缀，不用裸 Form**：回填片段与页面共用同一份渲染 data，而页面
// 自己的键空间里可能早已有 `Form`（商品列表页的 `Form` 是批量改价的 pricingForm **结构体**）。
// 撞名时片段里的 `{{.Form.code}}` 会命中结构体，Jet 报 `can't use code as field name in
// struct type` 并从那一行截断整页（HTTP 仍 200），且只在带那个键的渲染路径上炸。
//
// **字段名要逐个列出**：Jet 读 map 里缺失的键会抛运行时错误、整个片段渲染失败 ——
// 渲染器先渲到 buffer，失败走 `http.Error(500, …)`（buffer 里的半截内容被丢弃），
// 而 htmx 默认把 5xx 判成 `swap:false`：用户点了保存**什么都看不到**。
// 所以列出的字段都被补成零值。模板里访问**未列出**的字段仍要用 isset 包裹
// （见 internal/templates/CLAUDE.md 的「可选数据键必须用 isset 判断」）。
func formEchoData(c *gin.Context, fields ...string) gin.H {
	echo := formEchoFrom(c)
	form := make(gin.H, len(fields))
	checked := make(gin.H, len(fields))
	multi := make(gin.H, len(fields))
	for _, name := range fields {
		form[name] = echo.value(name)
		checked[name] = echo.checked(name)
		multi[name] = echo.list(name)
	}
	return gin.H{"FormEcho": form, "FormEchoChecked": checked, "FormEchoMulti": multi}
}
