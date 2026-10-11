package producthttp

import (
	"context"
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/shell"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// product_attributes.go — 商品属性库（属性组 / 属性值）：列表页与它的抽屉表单。
//
// 属性分两组形态：**规格属性**（isVariation，参与变体生成）与普通属性（只作展示与筛选）。
// 属性值行（ProductAttributesValueRows）是 HTMX 片段，商品编辑页与新建页都复用它。

// attrRowPrefix 属性值行的表单字段前缀（values[0].id → "values[0]."）。
//
// 接口面（原先的 product_handle.go 属性段）在前，后台页面在后。

const attrRowPrefix = "values["

// ProductAttributesPage 属性管理页：工程切换 + 筛选栏 + 属性组列表 + 值编辑器 + 内联新建表单。
func (h *productPageHandle) ProductAttributesPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败**不在这里出口**：整页 GET 只有一条渲染路径（函数末尾那次 c.HTML），
	// 失败页与成功页渲染同一份模板、同一份页壳数据，差别只有状态码 / no-store / 失败槽
	//（契约见 docs/rules/template-boundary.md §整页 GET 的失败出口）。每个失败点各写一遍
	//「渲染失败页」等于每页各有一份「失败页长什么样」的实现，改一处必然漏一处。
	httpStatus := http.StatusOK
	var loadErr error
	fail := func(err error) {
		if loadErr == nil {
			loadErr = err
			// 取数没读到 = 500。参数不合法是另一档（400，见下面的工程校验）。
			httpStatus = http.StatusInternalServerError
		}
	}
	projects, err := h.projects.List(ctx)
	if err != nil {
		fail(err)
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 与商品列表页 / 详情页 / 新建页同一份判据（productProjectExists）：?project=<不存在的 id>
	// 一路渲染下去会得到「这个工程下没有属性组」—— 把「工程不存在」说成了「工程是空的」。
	// 判据是**本次请求已加载的工程集合**（h.projects.List 返回全量：契约「列出全部站点工程」，
	// 无 Page/Size 参数、model 只 Order 不 LIMIT/WHERE、projects 根表未开 RLS），且与同一份
	// pageData 里的工程选择器**同源**。工程被删除或并发变化会让旧链接里的工程失效 ——
	// 那时 400 正是准确的事实：参数指向的工程已不在可选集合里（不是「工程是空的」）。
	// 参数级失败仍是**页面请求**：整页 + 400 + no-store。
	if loadErr == nil && selected != "" && !productProjectExists(projects, selected) {
		httpStatus = http.StatusBadRequest
		loadErr = errors.New(productenums.ErrInvalidParam)
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	// Variation 取值 ""（全部）/ "1"（参与变体）/ "0"（不参与），与 service 的
	// ListAttributeReq.Variation 同一套值，也与表格里「变体」列的口径一致。
	variation := strings.TrimSpace(c.Query("variation"))
	if variation != "1" && variation != "0" {
		variation = ""
	}
	page := productPageNumber(c.Query("page"))
	// 属性组列表：分页**下推到 service**（ListAttributeReq 自带 Page/Size），总数由契约的
	// CountAttributes 给出 —— handler 不再「逐页拉满拿全量再切片」，也不再为了凑出 total
	// 而多发若干次查询（上一轮的 listAllAttributes）。
	//
	// 顺序是**先计数再取页**：反过来（先取第 N 页再数总数）时，越界页码会让 service 返回空页，
	// 而分页条按收敛后的页码渲染 —— 「表格为空、分页条却显示第 2 页」正是
	// product_list_paging_test.go 要挡的那种自相矛盾组合。两次查询的条数一样，不额外付代价。
	total := int64(0)
	rows := []gin.H{}
	// 抽屉表单片段需要的数据：csrf 与取词函数 —— 片段是**带参数 include** 的
	//（数据是每个抽屉自己的数据类，不是页面 data），取不到 adminui.Prepare 注入的页面键。
	// csrf 由 attrFormCSRF 显式给；取词函数由每个子 data 各自经 adminui.PrepareFragment 注入
	//（见 attrGroupFormData / attrValuesFormData）—— 控制器不再自己取词。
	csrf := attrFormCSRF(c)
	// 筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 从它读回，
	// 回跳列表页不丢关键词 / 变体筛选 / 页码。
	listQuery := productQueryFromRequest(c, productAttrBackKeys...)
	// 过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size，
	// 两处口径分叉（关键词 / variation 只归一在一侧）在这里是不可能的。
	//
	// 每条取数都套 loadErr == nil：主数据或参数一旦判定失败，后面的派生查询一条都不发 ——
	// 失败页不需要这些数据，也不该在已被判非法的工程上继续查。
	var filterReq *productdto.ListAttributeReq
	list := []*productdto.AttributeResp{}
	if loadErr == nil && selected != "" {
		filterReq = &productdto.ListAttributeReq{ProjectID: selected, Keyword: keyword, Variation: variation}
		if n, cerr := h.products.CountAttributes(ctx, filterReq); cerr != nil {
			fail(cerr)
		} else {
			total = n
		}
	}
	if loadErr == nil && filterReq != nil {
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		if got, lerr := h.products.ListAttributes(ctx, &listReq); lerr != nil {
			fail(lerr)
		} else {
			list = got
		}
	}
	if loadErr == nil && len(list) > 0 {
		rows = make([]gin.H, 0, len(list))
		for _, a := range list {
			// 两个抽屉表单（编辑属性组 / 保存属性值）的实例数据：与失败重渲染**同形**
			//（同一个 attrRowDrawerForms），本页只负责把它们挂到行上供 <template> 里的
			// include 取用 —— 两处各拼一份必然分叉，而分叉只会在失败路径上缺键、静默截断整页。
			editForm, valuesForm := attrRowDrawerForms(c, csrf, selected, listQuery, a)
			rows = append(rows, gin.H{
				"ID": a.ID, "Key": a.Key, "Name": a.Name,
				"IsVariation": a.IsVariation, "Sort": a.Sort,
				"ValueCount": a.ValueCount,
				"EditForm":   editForm, "ValuesForm": valuesForm,
			})
		}
	}
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言。
	data := gin.H{
		"title":           productenums.ProductAttributesTitle,
		"menu":            "product-attributes",
		"Projects":        projects,
		"SelectedProject": selected,
		"Attributes":      rows,
		// 页头与空态两个「新建属性组」入口共用同一个抽屉（tpl-attr-create），
		// 抽屉内容来自共享片段，本键是它这一实例的数据（见 attrCreateDrawerForm）。
		"AttrCreateForm": attrCreateDrawerForm(c, csrf, selected, listQuery),
		// 筛选回显（GET 表单的 value / selected）+ 空态分档依据：
		// 「筛出来是空的」与「这个工程本来就没有属性组」必须给不同文案。
		"FilterKeyword":   keyword,
		"FilterVariation": variation,
		"Filtered":        keyword != "" || variation != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go），
		// 所以模板里的提示条已整批删除 —— 用户当下看到的是列表本身。
		"ListQuery": listQuery,
	}
	// 分页条：Go 只交事实（总数 / 页码 / 每页条数 / 基地址），文案、页码窗口与链接由
	// admin/partials/pagination.html 用 pkg/pagination.Page + 通用模板函数渲染。
	// 基地址带上关键词与变体筛选，翻页不丢条件；单页或空数据时 HasPagination() 为假 → 不注入，
	// 模板据 isset 跳过 —— 与旧 BuildPagination 返回 nil → TemplateKeys 给空 map 同一形态。
	if pg := pagination.Facts(total, page, productSubListPageSize); pg.HasPagination() {
		pg.Base = productListBaseURL("/admin/product-attributes", attributeListFilterQuery(selected, keyword, variation))
		data["Pagination"] = pg
	}
	// 出口只有这一处（页面成败都走它）：失败槽 + no-store 只在这一处加，
	//「失败页长什么样」因此只有一份实现。
	if loadErr != nil {
		data["LoadFailed"] = true
		data["LoadError"] = productErrPageText(c, loadErr)
		// 失败页不该被缓存：重试必须真的回到服务端，否则用户看到的是缓存的失败页。
		c.Header("Cache-Control", "no-store")
	}
	c.HTML(httpStatus, "admin/product/product_attributes.html", adminui.Prepare(c, data))
}

// attributeListFilterQuery 属性页的筛选条件（project + keyword + variation）。
func attributeListFilterQuery(projectID, keyword, variation string) url.Values {
	q := listFilterQuery(projectID, keyword)
	if variation != "" {
		q.Set("variation", variation)
	}
	return q
}

// ProductAttributesValueRows 值编辑器片段：add / remove 一次，回渲染整段行列表。
//
// 行编辑只重建原始表单值与位置；落库时的归一 / 去重 / 排序仍由 service 负责。
func (h *productPageHandle) ProductAttributesValueRows(c *gin.Context) {
	rows := attrEchoRowsFromForm(c)
	action := strings.TrimSpace(c.PostForm("action"))
	switch action {
	case "add":
		index := 0
		if len(rows) > 0 {
			index = rows[len(rows)-1].Index + 1
		}
		rows = append(rows, productdto.AttrValueFormRow{Index: index, Enabled: true})
	case "remove":
		if idx := parseIntOr(c.PostForm("removeIndex"), -1); idx >= 0 && idx < len(rows) {
			rows = append(rows[:idx], rows[idx+1:]...)
		}
	}
	// 兜底：至少要有一行可编辑，空列表会让「添加」按钮无从下手。
	if len(rows) == 0 {
		rows = []productdto.AttrValueFormRow{{Enabled: true}}
	}
	groupID := strings.TrimSpace(c.PostForm("groupId"))
	if groupID == "" {
		groupID = "new"
	}
	c.HTML(http.StatusOK, "admin/product/product_attribute_rows.html", attrRowsCtxData(c, groupID, rows))
}

func attrRowsFromResp(rows []productdto.AttributeValueResp) []productdto.AttrValueFormRow {
	out := make([]productdto.AttrValueFormRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, productdto.AttrValueFormRow{
			Index: i, ID: row.ID, Key: row.Key, Label: row.Label,
			Sort: strconv.Itoa(row.Sort), Enabled: row.Enabled,
		})
	}
	return out
}

// attrFormRowIndexes 按数值顺序排列提交的行索引，避免 map 遍历顺序改变行序。
func attrFormRowIndexes(vals url.Values) []int {
	indexes := make([]int, 0, 8)
	seen := make(map[int]bool)
	for key := range vals {
		if !strings.HasPrefix(key, attrRowPrefix) {
			continue
		}
		rest := key[len(attrRowPrefix):]
		close := strings.IndexByte(rest, ']')
		if close <= 0 || !strings.HasPrefix(rest[close:], "].") {
			continue
		}
		idx, err := strconv.Atoi(rest[:close])
		if err != nil || idx < 0 || seen[idx] {
			continue
		}
		seen[idx] = true
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	return indexes
}

func attrEchoRowsFromForm(c *gin.Context) []productdto.AttrValueFormRow {
	vals := attrFormValues(c)
	rows := make([]productdto.AttrValueFormRow, 0)
	for _, idx := range attrFormRowIndexes(vals) {
		base := fmt.Sprintf("values[%d].", idx)
		rows = append(rows, productdto.AttrValueFormRow{
			Index: idx, ID: vals.Get(base + "id"), Key: vals.Get(base + "key"),
			Label: vals.Get(base + "label"), Sort: vals.Get(base + "sort"),
			Enabled: formValueHas(vals, base+"enabled", "1"),
		})
	}
	return rows
}

// splitIDs 解析「逗号 / 空白 / 换行分隔」的 id 串（后台文本框输入）。
//
// 与 values[n] 数组表单不同，商品页的属性引用是一个文本框：粘一行 id 即可绑定，
// 避免为「勾选 N 个属性组」再造一套动态表单。
func splitIDs(raw string) []string {
	out := []string{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	}) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// attrRowsCtxData 属性值编辑片段（include）的渲染数据。
//
// GroupID 既标识「这是哪个组的编辑器」，也是片段拼容器 id / hx-target 的依据
// （attr-values-<GroupID>）—— 片段的 hx-include 会自动带上按钮自身的 groupId，
// 故客户端不需要额外传参。
//
// 为什么是 gin.H 而不是一个数据类 struct：片段的两条渲染路径（HTMX 片段端点用本函数的
// 返回值、抽屉表单 include 传 .RowsCtx）都要**按请求语言取词**，而 Jet 在 struct 上不支持
// `.["t"]`（渲染时报 can't use t as field name in struct type）。曾经这里给 struct 挂
// `Tr func(key, fallback string) string` 字段由控制器填 shell.TranslateFor(c) —— 那是
// 控制器取词。改成 gin.H 之后 t / tf 由 adminui.PrepareFragment 注入，取词统一回到渲染期
// （与 values_form / group_form 两个片段同形）。
func attrRowsCtxData(c *gin.Context, groupID string, rows []productdto.AttrValueFormRow) gin.H {
	return adminui.PrepareFragment(c, gin.H{"GroupID": groupID, "Rows": rows})
}

// attrRowsFromForm 从表单的 values[n].* 字段构造写入请求（空行忽略、数值解析）。
//
// n 不保证连续（删了中间一行再提交），故先收集出现过的索引再按**数值升序**取行，
// 而不是按 0..n-1 硬编码、也不是按 map 的遍历顺序 —— PostForm 是 map，
// Go 的 range 顺序随机，直接按遍历顺序收集会让「删中间一行再提交」后的行序随机跳变
// （实测：同一份表单两次提交得到不同的行序，编辑器的行会莫名其妙换位）。
func attrRowsFromForm(c *gin.Context) []productdto.AttributeValueReq {
	// 显式解析表单：gin 的 PostForm 缓存由 GetPostForm 系列惰性填充，
	// 直接读 c.Request.PostForm 在未触发解析时是空 map（实测 HTMX 片段路由）。
	// 这里自己兜一次 ParseForm，语义与 gin 的 PostForm 一致（application/x-www-form-urlencoded）。
	if c.Request.PostForm == nil {
		_ = c.Request.ParseForm()
	}
	reqs := c.Request.PostForm
	indexes := attrFormRowIndexes(reqs)
	rows := make([]productdto.AttributeValueReq, 0, len(indexes))
	for _, idx := range indexes {
		base := fmt.Sprintf("values[%d].", idx)
		label := strings.TrimSpace(reqs.Get(base + "label"))
		id := strings.TrimSpace(reqs.Get(base + "id"))
		key := strings.TrimSpace(reqs.Get(base + "key"))
		if label == "" && id == "" && key == "" {
			continue
		}
		// enabled 也是「同名隐藏域打底 + 复选框」（hidden "0" 恒在，勾选时再追加 "1"）：
		// 判据必须是**值里存在 "1"**，按「字段是否存在」判会恒真、禁用永远不生效；
		// 字段完全没出现时保持 nil，交由 service 兜底为启用（见 attrFormOptionalChecked）。
		rows = append(rows, productdto.AttributeValueReq{
			ID: id, Key: key, Label: label,
			Sort:    parseIntOr(reqs.Get(base+"sort"), 0),
			Enabled: attrFormOptionalChecked(reqs, base+"enabled"),
		})
	}
	return rows
}

// ProductAttributesCreate 新建属性组，完成后回到列表。
//
// 分档（路径 A 渐进增强，口径见 product_attribute_page_util.go）：
//   - 失败：htmx 档 200 + 回填片段（错误槽 + 用户刚填的字段与值行），原生档整页提示；
//   - 成功：整页提示（shell.RenderJump）—— htmx 走 HX-Redirect（XHR 会跟随 302，
//     读不到 Location，整页 HTML 会被塞进抽屉里）。
func (h *productPageHandle) ProductAttributesCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.CreateAttributeReq{
		ProjectID: projectID,
		Key:       c.PostForm("key"),
		Name:      c.PostForm("name"),
		Sort:      parseIntOr(c.PostForm("sort"), 0),
		Values:    attrRowsFromForm(c),
	}
	// 「参与变体」是**同名隐藏域打底 + 复选框**：浏览器把 hidden(0) 与 checkbox(1) 都提交，
	// 按第一个值判（c.PostForm）会恒取到 "0" —— 用户的勾选被静默丢弃（页面照样 200）。
	// 判据见 formValueHas（按值命中）。
	isVariation := attrFormChecked(c, "isVariation")
	req.IsVariation = &isVariation
	if _, err := h.products.CreateAttribute(c.Request.Context(), req); err != nil {
		h.attrGroupFormFail(c, attrGroupModeCreate, projectID, "new", productErrMsg(c, err))
		return
	}
	productAttrJump(c, true, productActionDoneMsg)
}

// ProductAttributesUpdate 修改属性组本身（名称 / 标识 / 参与变体 / 排序）。
//
// 失败与成功的分档口径同 ProductAttributesCreate（编辑抽屉同样会丢用户刚改的字）。
func (h *productPageHandle) ProductAttributesUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	id := c.PostForm("id")
	name := strings.TrimSpace(c.PostForm("name"))
	key := strings.TrimSpace(c.PostForm("key"))
	// 勾选判据同上（隐藏域打底，按值命中）。
	isVariation := attrFormChecked(c, "isVariation")
	sortV := parseIntOr(c.PostForm("sort"), 0)
	req := &productdto.UpdateAttributeReq{
		ID: id, Name: &name, Key: &key, IsVariation: &isVariation, Sort: &sortV,
	}
	if _, err := h.products.UpdateAttribute(c.Request.Context(), req); err != nil {
		h.attrGroupFormFail(c, attrGroupModeEdit, projectID, id, productErrMsg(c, err))
		return
	}
	productAttrJump(c, true, productActionDoneMsg)
}

// ProductAttributesSetValues 整体保存属性值（表单上已有的行就是全部值）。
//
// 失败时回填的是**用户刚编辑的那几行**（attrRowsFromForm 保序重建，与保存路径同一归一），
// 而不是库里的旧值 —— 整表替换语义下，「库里旧值」正是用户想改掉的东西。
func (h *productPageHandle) ProductAttributesSetValues(c *gin.Context) {
	projectID := c.PostForm("projectId")
	groupID := c.PostForm("id")
	req := &productdto.SetAttributeValuesReq{
		ID:     groupID,
		Values: attrRowsFromForm(c),
	}
	if _, err := h.products.SetAttributeValues(c.Request.Context(), req); err != nil {
		h.attrValuesFormFail(c, projectID, groupID, productErrMsg(c, err))
		return
	}
	productAttrJump(c, true, productActionDoneMsg)
}

// ProductAttributesDelete 删除属性组（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductAttributesDelete(c *gin.Context) {
	if err := h.products.DeleteAttribute(c.Request.Context(), &productdto.DeleteAttributeReq{ID: c.PostForm("id")}); err != nil {
		productAttrJump(c, false, productErrMsg(c, err))
		return
	}
	productAttrJump(c, true, productBulkDoneMsg(productAttrBulkDone))
}

// ProductAttributesBulkDelete 批量删除属性组（连同其全部属性值，由 service 保证）。
//
// 逐条走同一条删除路径：被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductAttributesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productAttrJump(c, false, productBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteAttribute(c.Request.Context(), &productdto.DeleteAttributeReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(deleted, skipped, productAttrBulkPartial, productAttrBulkDone)
	productAttrJump(c, ok, msg)
}

// parseIntOr 解析十进制整数，失败返回兜底值（后台表单容错，不因一个脏字段 500）。
func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

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
// values[n].* 不在这里：属性值行的回填走 RowsCtx（由 attrEchoRowsFromForm 按原始索引重建），
// 不是单值回填 —— 塞进 FormEcho 只会得到一串没人读的字符串。
var attrValuesFormFields = []string{"projectId", "id", "groupId"}

// attrGroupFormOpts 属性组抽屉表单的数据入参。
//
// 用结构体而不是位置参数：这里连着两个 id（工程 / 属性组）与一个模式串，位置写反
// （工程 id 塞进属性组槽）不会编译失败 —— 只会在失败片段里把工程 id 渲染进隐藏域，
// 提交后回跳到一个不存在的工程，且只在失败路径上出现。
type attrGroupFormOpts struct {
	Csrf      string
	ProjectID string
	Mode      string
	GroupID   string
	// ListQuery 是属性页当前的筛选上下文（表单 action 的 query）——
	// 写动作失败时由 shell.BackPath 从它读回，回跳列表页不丢筛选。
	ListQuery string
	// Name / Key / Sort 是**首屏**的初值（编辑抽屉取该组的当前值，新建给零值）；
	// 失败重渲染时它们由片段的 else 分支让位给 FormEcho.*（模板里一眼可见）。
	Name      string
	Key       string
	Sort      int
	Variation bool
	RowsCtx   gin.H
}

// attrGroupFormData 属性组抽屉表单的渲染数据（**首屏与失败重渲染共用**）。
//
// 键分两类：
//   - 结构性键（Csrf / Project / t / tf / Mode / Action / IsCreate / GroupID /
//     VariationChecked / RowsCtx）—— 恒定给齐，片段在**任何**渲染路径下都能独立成立；
//   - 回填三键（FormEcho / FormEchoChecked / FormEchoMulti）—— 只在失败重渲染时并入
//     （见 attrGroupFormFail），片段以 `gfFill := isset(.FormEcho)` 区分首屏与回填。
//
// t / tf 由 adminui.PrepareFragment 注入：片段是**带参数 include** 的（数据是每个抽屉
// 自己的数据类，不是页面 data），拿不到 adminui.Prepare 注入的页面键 —— 缺了 t 片段里的
// `.["t"](...)` 静默变空串（200、日志干净），缺了 tf 则是渲染中断（500，htmx 不 swap）。
// **不注入 csrf_token**（PrepareFragment 只管多语言），token 仍由 Csrf 键显式带。
//
// InDrawer 恒为 true：属性页的表单**只**存在于抽屉里（页面上没有行内属性表单）。
// 失败重渲染时若表单没申报抽屉形态，它会由 attrGroupFormFail 删掉。
func attrGroupFormData(c *gin.Context, o attrGroupFormOpts) gin.H {
	return adminui.PrepareFragment(c, gin.H{
		"Csrf": o.Csrf, "Project": o.ProjectID,
		"Mode": o.Mode, "Action": withListQuery(attrGroupFormAction(o.Mode), o.ListQuery),
		"GroupID": o.GroupID, "IsCreate": o.Mode == attrGroupModeCreate,
		// 首屏初值（三个键恒在：片段的 else 分支直接读它们，缺键会让 Jet 从那行截断）。
		"Name": o.Name, "Key": o.Key, "Sort": o.Sort,
		// 首屏勾选态（新建默认勾选、编辑取该组的当前值）；失败重渲染时模板走
		// FormEchoMulti 的按值命中分支，这个键只是结构上必须在场（模板 else 分支要读它）。
		"VariationChecked": o.Variation,
		"RowsCtx":          o.RowsCtx,
		"InDrawer":         true,
	})
}

// attrValuesFormOpts 属性值抽屉表单的数据入参。
type attrValuesFormOpts struct {
	Csrf      string
	ProjectID string
	GroupID   string
	// ListQuery 同 attrGroupFormOpts.ListQuery：失败回跳时保留列表筛选。
	ListQuery string
	RowsCtx   gin.H
}

// attrValuesFormData 属性值抽屉表单的渲染数据（首屏与失败重渲染共用）。
//
// 回填的**主体是 RowsCtx**（用户刚编辑的那几行，由 attrEchoRowsFromForm 保留位置和原值）；
// FormEcho* 三键只承载 projectId / id / groupId 这几个隐藏域 —— 值表单的可见字段
// 全是 values[n].*，没有单值字段可回填。t / tf 同 attrGroupFormData（PrepareFragment 注入）。
func attrValuesFormData(c *gin.Context, o attrValuesFormOpts) gin.H {
	return adminui.PrepareFragment(c, gin.H{
		"Csrf": o.Csrf, "Project": o.ProjectID,
		"GroupID": o.GroupID, "Action": withListQuery(attrValuesAction, o.ListQuery),
		"RowsCtx": o.RowsCtx, "InDrawer": true,
	})
}

// attrCreateDrawerForm 「新建属性组」抽屉的表单数据（页头与空态两个入口共用同一份）。
func attrCreateDrawerForm(c *gin.Context, csrf, projectID, listQuery string) gin.H {
	return attrGroupFormData(c, attrGroupFormOpts{
		Csrf: csrf, ProjectID: projectID, ListQuery: listQuery,
		// 新建时没有可继承的组：勾选态沿用既有默认（勾上「参与变体」）；
		// 属性值行**首屏为空**（Rows: nil）：编辑器渲染 0 行 + 「还没有属性值…」的引导文案，
		// 用户点「+ 添加值」后由 ProductAttributesValueRows 兜底保证至少一行。两者口径**不同**，
		// 别按「同口径」推 —— 这里若补一行空输入，抽屉一打开就是一行空行配着引导文案，更费解。
		Mode: attrGroupModeCreate, GroupID: "new", Variation: true,
		RowsCtx: attrRowsCtxData(c, "new", nil),
	})
}

// attrRowDrawerForms 属性组列表里**一行**对应的两个抽屉表单数据（页面首屏）。
//
// 两个抽屉的初值都从同一行数据来：编辑抽屉取该组的名称 / 标识 / 排序 / 参与变体，
// 属性值抽屉取该组的属性值列表。它们与失败重渲染时的数据形状**逐键一致**
// （差别只在 FormEcho* 三键与 SubmitErr 的有无），片段因此只有一套渲染分支。
func attrRowDrawerForms(c *gin.Context, csrf, projectID, listQuery string,
	a *productdto.AttributeResp) (gin.H, gin.H) {

	group := attrGroupFormData(c, attrGroupFormOpts{
		Csrf: csrf, ProjectID: projectID, ListQuery: listQuery,
		Mode: attrGroupModeEdit, GroupID: a.ID,
		Name: a.Name, Key: a.Key, Sort: a.Sort, Variation: a.IsVariation,
	})
	values := attrValuesFormData(c, attrValuesFormOpts{
		Csrf: csrf, ProjectID: projectID, GroupID: a.ID, ListQuery: listQuery,
		RowsCtx: attrRowsCtxData(c, a.ID, attrRowsFromResp(a.Values)),
	})
	return group, values
}

// attrGroupFormFail 属性组表单（新建 / 编辑）的失败出口。
//
//	· htmx 请求：200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回属性页 —— 取代原先的 302 + ?err=。
//
// **成功路径也走同一条出口**（调用点用 productAttrJump）：见本文件顶部注释。
func (h *productPageHandle) attrGroupFormFail(c *gin.Context, mode, projectID, groupID string, msg productMsg) {
	if !isHXRequest(c) {
		productAttrJump(c, false, msg)
		return
	}
	data := attrGroupFormData(c, attrGroupFormOpts{
		Csrf: attrFormCSRF(c), ProjectID: projectID,
		ListQuery: productQueryFromRequest(c, productAttrBackKeys...),
		Mode:      mode, GroupID: groupID,
		// 新建抽屉按本次提交的原始位置和值重建，编辑抽屉没有值编辑器。
		RowsCtx: attrRowsCtxData(c, groupID, attrEchoRowsFromForm(c)),
	})
	// 错误槽收**事实**（productMsg），取词在 product_attribute_group_form.html 的 tf 里做。
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
	c.HTML(http.StatusOK, attrGroupFormTemplate, data)
}

// attrValuesFormFail 属性值表单的失败出口（分档口径与上面一致）。
func (h *productPageHandle) attrValuesFormFail(c *gin.Context, projectID, groupID string, msg productMsg) {
	if !isHXRequest(c) {
		productAttrJump(c, false, msg)
		return
	}
	data := attrValuesFormData(c, attrValuesFormOpts{
		Csrf: attrFormCSRF(c), ProjectID: projectID,
		GroupID:   groupID,
		ListQuery: productQueryFromRequest(c, productAttrBackKeys...),
		RowsCtx:   attrRowsCtxData(c, groupID, attrEchoRowsFromForm(c)),
	})
	// 错误槽收**事实**（productMsg），取词在 product_attribute_values_form.html 的 tf 里做。
	data["SubmitErr"] = msg
	for k, v := range formEchoData(c, attrValuesFormFields...) {
		data[k] = v
	}
	if strings.TrimSpace(c.PostForm("inDrawer")) == "" {
		delete(data, "InDrawer")
	}
	c.HTML(http.StatusOK, attrValuesFormTemplate, data)
}

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
// adminui.Prepare 注入的页面键，所以 token 由 handler 显式带进来 —— 两条取值路：
//
//  1. **失败重渲染**：token 就在这次提交的表单里（表单自带的 csrf_token 隐藏域）。
//     优先用它，因为它一定非空 —— 回落到会话时若 session 读不到（存储未初始化等），
//     片段里的表单会带着空 token，用户再点一次保存就是 403，而错误槽里什么都没说；
//  2. **页面首屏**：走 adminui.Prepare 的**同一个**取值入口（builtin.GetCSRFToken），
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

// attributeIDsFromForm 收商品表单里的属性组引用。
//
// 抽屉是勾选列表（同名多值，浏览器逐项提交），同时兼容老式的「逗号分隔单值」形态
// （接口 / 脚本路径）—— 两种形态都归一到 splitIDs 解析（分隔符与去空白只有一份规则）。
func attributeIDsFromForm(c *gin.Context) []string {
	values := c.PostFormArray("attributeIds")
	if len(values) == 0 {
		return splitIDs(c.PostForm("attributeIds"))
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		for _, id := range splitIDs(raw) {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// attributeOptions 属性组勾选列表（新建抽屉 / 编辑页；本批起不再让人手打 UUID）。
//
// 取数与标签口径（名字 + 标识 key，只给名字会在同名属性组之间选错）唯一一份在
// service 的 AttributeOptionViews —— 编辑页这条老路径与新建页读模型共用同一个结论，
// 两处各拼一遍时「带上 key」这条规则改一处就漏一处。
//
// 上限同样取 service 的常量：抽屉与新建页是同一类「一屏全量勾选」，两处各写一个 200
// 时改一处会静默分叉。
func (h *productPageHandle) attributeOptions(ctx context.Context, projectID string) (out []productdto.AttributeOptionView, err error) {
	out = []productdto.AttributeOptionView{}
	if strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	list, lerr := h.products.ListAttributes(ctx, &productdto.ListAttributeReq{
		ProjectID: projectID, Size: productservice.PageAttributeOptionSize,
	})
	if lerr != nil {
		return nil, lerr
	}
	return productservice.AttributeOptionViews(list), nil
}

// ==========================================================================
// 属性接口：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：值组的归一 / 去重 / 排序、引用校验一律在 service。
// ==========================================================================
// CreateAttribute 新建属性组（可同时带初始属性值）。
func (h *Handle) CreateAttribute(c *gin.Context) {
	req := &productdto.CreateAttributeReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgCreateSuccess, res)
}

// UpdateAttribute 修改属性组本身（值走 SetAttributeValues）。
func (h *Handle) UpdateAttribute(c *gin.Context) {
	req := &productdto.UpdateAttributeReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// SetAttributeValues 整体保存属性值（全量替换：请求里没有的值被删除）。
func (h *Handle) SetAttributeValues(c *gin.Context) {
	req := &productdto.SetAttributeValuesReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SetAttributeValues(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgUpdateSuccess, res)
}

// GetAttribute 属性组详情。
func (h *Handle) GetAttribute(c *gin.Context) {
	req := &productdto.GetAttributeReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetAttribute(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDetailSuccess, res)
}

// ListAttributes 属性组列表。
func (h *Handle) ListAttributes(c *gin.Context) {
	req := &productdto.ListAttributeReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListAttributes(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgListSuccess, list)
}

// DeleteAttribute 删除属性组（被商品引用时拒绝）。
func (h *Handle) DeleteAttribute(c *gin.Context) {
	req := &productdto.DeleteAttributeReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteAttribute(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "product", err)
		return
	}
	response.SuccessWithMessage(c, productenums.MsgDeleteSuccess, nil)
}
