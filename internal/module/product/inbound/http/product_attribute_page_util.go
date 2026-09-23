// product_attribute_page_util.go — 商品属性页（/admin/product-attributes）写表单的
// 「提交失败原地留住输入」出口（路径 A 渐进增强）。
//
// 形状与口径**照批 1 的商品建表单**（product_new_page.go + partials/product_create_form.html），
// 分档判据只有一份（product_page_util.go 的 isHXRequest / redirectWhere / hxFragment / formEchoData）：
//
//	· htmx 请求：失败返 200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：维持既有 302 + ?err=（无 JS 环境的回退行为一个字不改）；
//	· 成功出口也必须分档：htmx 的 XHR 会自己跟随 302，最终响应里读不到 Location，
//	  整页 HTML 会被塞进抽屉里 —— 唯一能让它整页跳转的是 redirectWhere 发的 HX-Redirect。
//
// 为什么单独一个文件：属性页有三个抽屉表单（新建属性组 / 编辑属性组 / 保存属性值），
// 每个抽屉的数据既要在**页面首屏**渲染（初值 = 库里当前值），又要在**提交失败**时由
// handler 单独重渲染（值 = 用户刚打的字）。两处各拼一份 data 必然分叉，而分叉不会编译
// 失败 —— 只会让失败重渲染缺一个键，于是 Jet 在那一行渲染失败、**整块响应被丢弃（500）**，
// 而 htmx 把 5xx 判成不 swap：用户点「保存」后页面上毫无反应（比白页更难查）。
// 因此组装只有 attrGroupFormData / attrValuesFormData 两个入口，首屏与失败共用。
package producthttp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/web/shell"
)

// —— 表单形态与地址 ——

// 属性组表单的两种形态：新建（页头 / 空态打开的抽屉 tpl-attr-create）
// 与编辑（列表每行一个抽屉 tpl-attr-edit-<id>）。
const (
	attrGroupModeCreate = "create"
	attrGroupModeEdit   = "edit"
)

// 属性页抽屉表单的模板名（片段自带 host，可独立渲染，不带 layout）。
const (
	attrGroupFormTemplate  = "admin/product/product_attribute_group_form.html"
	attrValuesFormTemplate = "admin/product/product_attribute_values_form.html"
)

// 三个抽屉表单的提交地址（原生 action 与 hx-post 恒为同一个值）。
const (
	attrGroupCreateAction = "/admin/product-attributes/create"
	attrGroupUpdateAction = "/admin/product-attributes/update"
	attrValuesAction      = "/admin/product-attributes/set-values"
)

// attrGroupFormAction 属性组表单按形态取提交地址。
func attrGroupFormAction(mode string) string {
	if mode == attrGroupModeEdit {
		return attrGroupUpdateAction
	}
	return attrGroupCreateAction
}

// —— 回填字段清单（片段与 handler 之间的协议）——

// attrGroupFormFields 属性组表单的回填字段清单。
//
// 改片段里的 `name=` 必须同步改这里：helper 按这份清单给每个字段补零值键，漏列的字段
// 在失败片段里读不到 —— Jet 读缺失的 map 键会在那一行渲染失败，整块响应被丢弃（500），
// htmx 判 5xx 为不 swap：用户看到的是「点了保存什么也没发生」，页面上的输入还是旧的。
//
// isVariation 只能按 FormEchoMulti 的**值**回填、不能用 FormEchoChecked：它是
// 「同名隐藏域打底 + 复选框」的形态，隐藏域让它**每次提交都存在** —— 详见 formValueHas。
var attrGroupFormFields = []string{"projectId", "id", "name", "key", "sort", "isVariation"}

// attrValuesFormFields 属性值表单的回填字段清单。
//
// values[n].* 不在这里：属性值行的回填走 RowsCtx（由 attrRowsFromForm 按提交顺序重建），
// 不是单值回填 —— 塞进 FormEcho 只会得到一串没人读的字符串。
var attrValuesFormFields = []string{"projectId", "id", "groupId"}

// productAttributeListURL 属性页的回跳地址（PRG）：保留工程上下文并带上一条结论文案。
//
// 与商品列表页的 productListURL 同一形状（工程与文案都做 URL 编码）；原生档的既有落点
// /admin/product-attributes 一个字不改。
func productAttributeListURL(projectID, errMsg string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if e := strings.TrimSpace(errMsg); e != "" {
		q.Set(listErrMark, e)
	}
	if enc := q.Encode(); enc != "" {
		return "/admin/product-attributes?" + enc
	}
	return "/admin/product-attributes"
}

// —— 数据组装（首屏与失败重渲染共用）——

// attrGroupFormOpts 属性组抽屉表单的数据入参。
//
// 用结构体而不是位置参数：这里连着两个 id（工程 / 属性组）与一个模式串，位置写反
// （工程 id 塞进属性组槽）不会编译失败 —— 只会在失败片段里把工程 id 渲染进隐藏域，
// 提交后回跳到一个不存在的工程，且只在失败路径上出现。
type attrGroupFormOpts struct {
	Csrf      string
	ProjectID string
	Tr        func(key, fallback string) string
	Mode      string
	GroupID   string
	// Name / Key / Sort 是**首屏**的初值（编辑抽屉取该组的当前值，新建给零值）；
	// 失败重渲染时它们由片段的 else 分支让位给 FormEcho.*（模板里一眼可见）。
	Name      string
	Key       string
	Sort      int
	Variation bool
	Rows      attrRowsCtx
}

// attrGroupFormData 属性组抽屉表单的渲染数据（**首屏与失败重渲染共用**）。
//
// 键分两类：
//   - 结构性键（Csrf / Project / t / Mode / Action / IsCreate / GroupID / VariationChecked /
//     RowsCtx）—— 恒定给齐，片段在**任何**渲染路径下都能独立成立；
//   - 回填三键（FormEcho / FormEchoChecked / FormEchoMulti）—— 只在失败重渲染时并入
//     （见 attrGroupFormFail），片段以 `gfFill := isset(.FormEcho)` 区分首屏与回填。
//
// t 是取词函数：片段是**带参数 include** 的（数据是每个抽屉自己的数据类，不是页面 data），
// 拿不到 shell.Prepare 注入的页面键 —— 缺了它片段里的 `.["t"](...)` 会在那一行报错中断整页。
//
// InDrawer 恒为 true：属性页的表单**只**存在于抽屉里（页面上没有行内属性表单）。
// 失败重渲染时若表单没申报抽屉形态，它会由 attrGroupFormFail 删掉。
func attrGroupFormData(o attrGroupFormOpts) gin.H {
	return gin.H{
		"Csrf": o.Csrf, "Project": o.ProjectID, "t": o.Tr,
		"Mode": o.Mode, "Action": attrGroupFormAction(o.Mode),
		"GroupID": o.GroupID, "IsCreate": o.Mode == attrGroupModeCreate,
		// 首屏初值（三个键恒在：片段的 else 分支直接读它们，缺键会让 Jet 从那行截断）。
		"Name": o.Name, "Key": o.Key, "Sort": o.Sort,
		// 首屏勾选态（新建默认勾选、编辑取该组的当前值）；失败重渲染时模板走
		// FormEchoMulti 的按值命中分支，这个键只是结构上必须在场（模板 else 分支要读它）。
		"VariationChecked": o.Variation,
		"RowsCtx":          o.Rows,
		"InDrawer":         true,
	}
}

// attrValuesFormOpts 属性值抽屉表单的数据入参。
type attrValuesFormOpts struct {
	Csrf      string
	ProjectID string
	Tr        func(key, fallback string) string
	GroupID   string
	Rows      attrRowsCtx
}

// attrValuesFormData 属性值抽屉表单的渲染数据（首屏与失败重渲染共用）。
//
// 回填的**主体是 RowsCtx**（用户刚编辑的那几行，由 attrRowsFromForm 保序重建）；
// FormEcho* 三键只承载 projectId / id / groupId 这几个隐藏域 —— 值表单的可见字段
// 全是 values[n].*，没有单值字段可回填。
func attrValuesFormData(o attrValuesFormOpts) gin.H {
	return gin.H{
		"Csrf": o.Csrf, "Project": o.ProjectID, "t": o.Tr,
		"GroupID": o.GroupID, "Action": attrValuesAction,
		"RowsCtx": o.Rows, "InDrawer": true,
	}
}

// attrCreateDrawerForm 「新建属性组」抽屉的表单数据（页头与空态两个入口共用同一份）。
func attrCreateDrawerForm(csrf, projectID string, tr func(key, fallback string) string) gin.H {
	return attrGroupFormData(attrGroupFormOpts{
		Csrf: csrf, ProjectID: projectID, Tr: tr,
		// 新建时没有可继承的组：勾选态沿用既有默认（勾上「参与变体」）；
		// 属性值行**首屏为空**（Rows: nil）：编辑器渲染 0 行 + 「还没有属性值…」的引导文案，
		// 用户点「+ 添加值」后由 ProductAttributesValueRows 兜底保证至少一行。两者口径**不同**，
		// 别按「同口径」推 —— 这里若补一行空输入，抽屉一打开就是一行空行配着引导文案，更费解。
		Mode: attrGroupModeCreate, GroupID: "new", Variation: true,
		Rows: attrRowsCtx{GroupID: "new", Rows: nil},
	})
}

// attrRowDrawerForms 属性组列表里**一行**对应的两个抽屉表单数据（页面首屏）。
//
// 两个抽屉的初值都从同一行数据来：编辑抽屉取该组的名称 / 标识 / 排序 / 参与变体，
// 属性值抽屉取该组的属性值列表。它们与失败重渲染时的数据形状**逐键一致**
// （差别只在 FormEcho* 三键与 SubmitErr 的有无），片段因此只有一套渲染分支。
func attrRowDrawerForms(csrf, projectID string, tr func(key, fallback string) string,
	a *productdto.AttributeResp) (gin.H, gin.H) {

	group := attrGroupFormData(attrGroupFormOpts{
		Csrf: csrf, ProjectID: projectID, Tr: tr,
		Mode: attrGroupModeEdit, GroupID: a.ID,
		Name: a.Name, Key: a.Key, Sort: a.Sort, Variation: a.IsVariation,
	})
	values := attrValuesFormData(attrValuesFormOpts{
		Csrf: csrf, ProjectID: projectID, Tr: tr, GroupID: a.ID,
		Rows: attrRowsCtx{GroupID: a.ID, Rows: a.Values},
	})
	return group, values
}

// —— 失败出口（分档口径唯一，别在调用点各写一份头判断）——

// attrGroupFormFail 属性组表单（新建 / 编辑）的失败出口。
//
//	· htmx 请求：200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：302 + ?err=（既有行为一个字不改）。
//
// **成功路径也必须分档**（调用点用 redirectWhere）：见本文件顶部注释。
func (h *productPageHandle) attrGroupFormFail(c *gin.Context, mode, projectID, groupID, msg string) {
	if !isHXRequest(c) {
		c.Redirect(http.StatusFound, productAttributeListURL(projectID, msg))
		return
	}
	data := attrGroupFormData(attrGroupFormOpts{
		Csrf: attrFormCSRF(c), ProjectID: projectID, Tr: shell.TranslateFor(c),
		Mode: mode, GroupID: groupID,
		// 属性值行同样按**这次提交**重建（新建抽屉里有行编辑器，编辑抽屉没有、重建出空行）：
		// 这是「用户刚编的行」唯一的来源，行序与去重口径与保存路径完全一致。
		Rows: attrRowsCtx{GroupID: groupID, Rows: rowsToResp(attrRowsFromForm(c))},
	})
	data["SubmitErr"] = msg
	for k, v := range formEchoData(c, attrGroupFormFields...) {
		data[k] = v
	}
	// 抽屉形态由**表单自己**申报（隐藏域 inDrawer）：抽屉内容被 ui/drawer.js 克隆到
	// document 级，由渲染路径反推（HX-Target / Referer / 请求 URL）都不可靠；
	// 一次表单提交能把这个事实原样带过来 —— 否则抽屉里失败一次，「取消」按钮就消失了。
	if strings.TrimSpace(c.PostForm("inDrawer")) == "" {
		delete(data, "InDrawer")
	}
	c.HTML(http.StatusOK, attrGroupFormTemplate, shell.Prepare(c, data))
}

// attrValuesFormFail 属性值表单的失败出口（分档口径与上面一致）。
func (h *productPageHandle) attrValuesFormFail(c *gin.Context, projectID, groupID, msg string) {
	if !isHXRequest(c) {
		c.Redirect(http.StatusFound, productAttributeListURL(projectID, msg))
		return
	}
	data := attrValuesFormData(attrValuesFormOpts{
		Csrf: attrFormCSRF(c), ProjectID: projectID, Tr: shell.TranslateFor(c),
		GroupID: groupID,
		Rows:    attrRowsCtx{GroupID: groupID, Rows: rowsToResp(attrRowsFromForm(c))},
	})
	data["SubmitErr"] = msg
	for k, v := range formEchoData(c, attrValuesFormFields...) {
		data[k] = v
	}
	if strings.TrimSpace(c.PostForm("inDrawer")) == "" {
		delete(data, "InDrawer")
	}
	c.HTML(http.StatusOK, attrValuesFormTemplate, shell.Prepare(c, data))
}

// —— 勾选态判据（同名隐藏域打底）——

// formValueHas 判定同名多值里是否存在 want。
//
// **必须遍历全部值，不能用 url.Values.Get**：Get 返回第一个，而「同名隐藏域打底 +
// 同名复选框」的形态（hidden "0" 在前、checkbox "1" 在后，见 partials/product_attribute_rows.html）
// 让 Get 恒取到 "0" —— 于是「参与变体 / 启用」这类勾选**永远为假**，而且不报任何错：
// 页面照样 200、值照样落库，只是用户的勾选被静默丢弃。属性页的勾选字段全部走这里。
func formValueHas(vals url.Values, name, want string) bool {
	for _, v := range vals[name] {
		if strings.TrimSpace(v) == want {
			return true
		}
	}
	return false
}

// attrFormValues 这次请求的**全部**表单值（gin 的 PostForm 只给单值口）。
//
// 自己兜一次解析：gin 的 PostForm 缓存是惰性填充的，直接读 c.Request.PostForm 在未触发
// 解析时是空 map（与 attrRowsFromForm 同一手法、同一理由）。
func attrFormValues(c *gin.Context) url.Values {
	if c == nil || c.Request == nil {
		return url.Values{}
	}
	if c.Request.PostForm == nil {
		_ = c.Request.ParseMultipartForm(formEchoMemory)
	}
	return c.Request.PostForm
}

// attrFormChecked 表单里「勾选态」的判据：提交里存在值等于 "1" 的那一项。
func attrFormChecked(c *gin.Context, name string) bool {
	return formValueHas(attrFormValues(c), name, "1")
}

// attrFormOptionalChecked 同上，但**字段完全没出现**时返回 nil（「没给」≠「明确没勾」）。
//
// 供 values[n].enabled 这类「缺省即启用」的字段用：service 对 Enabled == nil 兜底为 true
// （sanitizeAttributeValues），所以「表单没带这个字段」必须保持 nil，不能压成 false ——
// 压成 false 会让不经浏览器表单的调用方（API / 脚本）提交的属性值全部变成「已禁用」。
func attrFormOptionalChecked(vals url.Values, name string) *bool {
	if _, ok := vals[name]; !ok {
		return nil
	}
	v := formValueHas(vals, name, "1")
	return &v
}

// attrFormCSRF 抽屉表单里的 csrf token。
//
// 片段是**带参数 include** 的（数据是每个抽屉自己的数据类，不是页面 data），取不到
// shell.Prepare 注入的页面键，所以 token 由 handler 显式带进来 —— 两条取值路：
//
//  1. **失败重渲染**：token 就在这次提交的表单里（表单自带的 csrf_token 隐藏域）。
//     优先用它，因为它一定非空 —— 回落到会话时若 session 读不到（存储未初始化等），
//     片段里的表单会带着空 token，用户再点一次保存就是 403，而错误槽里什么都没说；
//  2. **页面首屏**：走 shell.Prepare 的**同一个**取值入口（builtin.GetCSRFToken），
//     否则同一张表单在首屏与失败重渲染两条路径上会带着两个不同的 token。
func attrFormCSRF(c *gin.Context) string {
	if tok := strings.TrimSpace(c.PostForm("csrf_token")); tok != "" {
		return tok
	}
	tok, err := builtin.GetCSRFToken(c)
	if err != nil {
		return ""
	}
	return tok
}
