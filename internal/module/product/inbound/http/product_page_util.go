// product_page_util.go — 商品后台页的表单取值小工具。
package producthttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
)

// parseFloat 解析表单里的金额字段（非法值返回错误，由调用方忽略）。
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// productListPageSize 商品列表每页条数（服务端分页）。
//
// 20 与后台其它列表页同一量级：列表行要逐个取商品详情组装视图，
// 一页取太多等于把「翻页」换成「一屏卡顿」。
const productListPageSize = 20

// productPageNumber 解析页码：空值 / 非数字 / 越界一律退回第 1 页。
//
// 查询串不是可信边界：page=abc、page=-3、page=999999 都会走到这里，
// 服务端自行归一，绝不把非法页码透给取数层（负 offset 在 PG 上是错误）。
func productPageNumber(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// productListFilterURL 列表页的筛选基地址（**不含** page/limit：分页组件自己拼）。
//
// 分页链接由 shell.BuildPagination 在这个基地址后追加 page/limit —— 把筛选条件
// 拼进基地址，翻页时关键词与状态才不会丢；反过来说，若基地址里塞了 page，
// 就会出现两个 page 参数，浏览器取第一个，翻页看起来「点了没反应」。
func productListFilterURL(projectID, keyword, status string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if k := strings.TrimSpace(keyword); k != "" {
		q.Set("keyword", k)
	}
	if s := strings.TrimSpace(status); s != "" {
		q.Set("status", s)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/products?" + enc
	}
	return "/admin/products"
}

// productStatusOptions 列表筛选的状态下拉项（含「全部状态」，按当前语言取词）。
//
// 状态维度**只有这一种入口**：不另配状态徽章筛选 —— 同一维度两套控件时，
// 用户会怀疑它们是否等价（admin-ui-logic §2）。
func productStatusOptions(c *gin.Context, current string) []gin.H {
	tr := shell.TranslateFor(c)
	opts := []struct{ value, key, fallback string }{
		{"", productenums.ProductsStatusAll, "全部状态"},
		{productenums.StatusDraft, productenums.ProductsStatusDraft, "草稿"},
		{productenums.StatusPublished, productenums.ProductsStatusPublished, "已上架"},
		{productenums.StatusArchived, productenums.ProductsStatusArchived, "已下架"},
	}
	out := make([]gin.H, 0, len(opts))
	for _, o := range opts {
		out = append(out, gin.H{
			"Value":    o.value,
			"Label":    tr(o.key, o.fallback),
			"Selected": o.value == strings.TrimSpace(current),
		})
	}
	return out
}

// productEditLocation 商品编辑页的回跳地址（PRG）——商品级写操作的**唯一**落点。
//
// 商品详情页已改为**只读**（详情页回答「这个商品由什么组成」，编辑页回答「怎么改它」）：
// 变体 / 评分 / 属性引用 / 分类与品牌 / 手工标签 / 预设回滚这些写表单都在编辑页，
// 提交后必须留在编辑页 —— 弹回详情页会让用户以为改动没生效（那里没有表单了），
// 弹回列表页则等于让他重新找一遍那个商品。
// errMsg 非空时作为 ?err= 回显（编辑页有错误提示位）；只有「商品本身被删掉」是例外
// （那时编辑页没有意义，见 ProductsDelete）。
func productEditLocation(projectID, productID, errMsg string) string {
	q := url.Values{}
	q.Set("project", strings.TrimSpace(projectID))
	q.Set("product", strings.TrimSpace(productID))
	if e := strings.TrimSpace(errMsg); e != "" {
		q.Set("err", e)
	}
	return "/admin/products/edit?" + q.Encode()
}

// productEditLocationWith 编辑页写操作的完整回跳地址：?err=（警告）与 ?done=（信息）两个渲染位。
//
// 变体清单的保存是**部分成功**语义（清单外要删的行可能因有库存 / 被引用被跳过），
// 所以「成功但有跳过」必须能同时看见两个信号 —— 只给 err 会让用户以为整批没保存，
// 只给 done 又会让跳过原因被淹没。
func productEditLocationWith(projectID, productID, errMsg, doneMsg string) string {
	loc := productEditLocation(projectID, productID, errMsg)
	if d := strings.TrimSpace(doneMsg); d != "" {
		sep := "?"
		if strings.Contains(loc, "?") {
			sep = "&"
		}
		loc += sep + "done=" + url.QueryEscape(d)
	}
	return loc
}

// formProductID 取表单里的商品 id：优先 productId，再回落 id。
//
// 两个名字都是**既有字段名**，不下令重命名：详情页的商品级表单（属性引用 / 分类与品牌 /
// 手工标签）用的隐藏域是 id（其值就是商品 id），子资源表单（变体 / 评分）用的是 productId。
// 只适用于这两者都指向商品本身的地方 —— 变体删除 / 评分删除的 id 是**子资源 id**，
// 那两个 handler 必须读 productId，绝不能走这里的回落。
func formProductID(c *gin.Context) string {
	if id := strings.TrimSpace(c.PostForm("productId")); id != "" {
		return id
	}
	return strings.TrimSpace(c.PostForm("id"))
}

// —— HTMX 写表单的「分档出口」（各写表单共用；本文件是这一档位的唯一实现处）——
//
// 背景：本模块的写表单失败时是 `302 + ?err=` 回列表页，**用户刚填的内容全丢**。
// 改造方向是路径 A「渐进增强」：表单保留原生 `method="post" action=…`，另加 `hx-post`
// （htmx 优先拦住 submit）；服务端按 `HX-Request` **分档**：
//
//	· htmx 请求：失败返 **200 + 片段**（错误槽 + 回填后的表单）—— 抽屉 / 页面原地留住输入；
//	· 原生请求：维持既有 `302 + ?err=`（无 JS 环境的回退行为一个字不改）。
//
// 新增写表单时**不要再各写一份头判断或自己拼回填 map** —— 分档口径必须只有一份，
// 否则会出现「跳转按 htmx 走、片段按原生走」这种自相矛盾的档位。

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 判据与 redirectWhere 同口径（大小写不敏感、去首尾空白）：两个出口必须认同同一个信号。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// redirectWhere 页面写动作的 PRG 出口（从 product_page_handle.go 提升到本文件共享）。
//
// 原生表单（列表页的批量删除、新建抽屉）走 302；HTMX 请求（批量改价抽屉里的 hx-post）
// 走 **HX-Redirect**：htmx 的 XHR 会自己跟随 302，最终响应里已经读不到 Location，
// 只有响应头上的 HX-Redirect 能让它整页跳转（否则会把整页 HTML 塞进抽屉里）。
// 两条路的终点是同一个 URL，页面壳与提示位完全一致。
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
//	redirectWhere(c, fallbackURL)   // 原生回退：既有 302 + ?err= 不变
//
// 片段模板必须**可独立渲染**（不带 extends layout，data 由调用点给齐），并且自带
// htmx 需要的结构（错误槽 + 回填后的表单）—— 模板由各批自己建，本 helper 只管分档。
// 模板渲染失败时 jet 渲染器会走它的 renderError（500 + 文案），不会把半个片段当成功返回。
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
// （新建路径上后者根本不存在）。
type formEcho struct {
	// values 归一化后的提交值：去首尾空白、丢空项（与 shell.FieldValue 同口径）。
	// 多值字段保留**提交顺序** —— 顺序在本域有语义（「按本表顺序第一个勾中的仓是认领仓」），
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
//	"FormEcho"        gin.H{字段名: string}   文本 / 下拉 / 隐藏域 → value="{{ .FormEcho.sku }}"
//	"FormEchoChecked" gin.H{字段名: bool}     复选框 → {{if .FormEchoChecked.trackQuantity}}checked{{end}}
//	"FormEchoMulti"   gin.H{字段名: []string} 多值字段的完整提交值（保序）
//
// **键名必须带 FormEcho 前缀**：片段与页面共用同一份渲染 data，而页面自己的键空间里
// 早已有同名键 —— 商品列表页的 `Form` 是**批量改价的 pricingForm 结构体**
// （product_page_handle.go 的 `"Form": defaultPricingForm(...)`）。沿用裸 `Form` 会让片段里的
// `{{.Form.name}}` 命中结构体，Jet 报 `can't use name as field name in struct type` 并
// **从那一行截断整页**（HTTP 仍 200）—— 而且只在列表页那条渲染路径上炸，新建整页看不出来。
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
