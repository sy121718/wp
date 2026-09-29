// product_attribute_handle.go — 后台商品属性管理页（issue #7）。
//
// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约
// （与 product_handle.go 同一模式，避免把商品依赖掺进 dashboard 通用装配）。
//
// 交互遵循后台规范：
//   - GET /admin/product-attributes 渲染完整页；
//   - POST 处理完 302 回列表，错误经 ?err= 回显（原生表单 + csrf_token 隐藏域）；
//   - 属性值行的增删走 HTMX（hx-post → 片段），因为「编辑中的值」只存在于表单里，
//     必须服务端参与归一与去重（与设置页语言清单同一手法）。
package producthttp

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
)

// attrRowPrefix 属性值行的表单字段前缀（values[0].id → "values[0]."）。
const attrRowPrefix = "values["

// ProductAttributesPage 属性管理页：工程切换 + 筛选栏 + 属性组列表 + 值编辑器 + 内联新建表单。
func (h *productPageHandle) ProductAttributesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_attribute", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
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
	// 抽屉表单片段需要的数据：csrf 与取词函数 t —— 片段是**带参数 include** 的
	//（数据是每个抽屉自己的数据类，不是页面 data），取不到 shell.Prepare 注入的页面键，
	// 所以两者必须由这里显式给（与失败重渲染走同一对入口，见 attrFormCSRF / TranslateFor）。
	csrf := attrFormCSRF(c)
	tr := shell.TranslateFor(c)
	if selected != "" {
		// 过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size，
		// 两处口径分叉（关键词 / variation 只归一在一侧）在这里是不可能的。
		filterReq := &productdto.ListAttributeReq{ProjectID: selected, Keyword: keyword, Variation: variation}
		n, cerr := h.products.CountAttributes(ctx, filterReq)
		if cerr != nil {
			shell.PageError(c, "product_attribute", cerr)
			return
		}
		total = n
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		list, lerr := h.products.ListAttributes(ctx, &listReq)
		if lerr != nil {
			shell.PageError(c, "product_attribute", lerr)
			return
		}
		rows = make([]gin.H, 0, len(list))
		for _, a := range list {
			// 两个抽屉表单（编辑属性组 / 保存属性值）的实例数据：与失败重渲染**同形**
			//（同一个 attrRowDrawerForms），本页只负责把它们挂到行上供 <template> 里的
			// include 取用 —— 两处各拼一份必然分叉，而分叉只会在失败路径上缺键、静默截断整页。
			editForm, valuesForm := attrRowDrawerForms(csrf, selected, tr, a)
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
		"title":           shell.TranslateFor(c)(productenums.ProductAttributesTitle, "商品属性"),
		"menu":            "product-attributes",
		"Projects":        projects,
		"SelectedProject": selected,
		"Attributes":      rows,
		// 页头与空态两个「新建属性组」入口共用同一个抽屉（tpl-attr-create），
		// 抽屉内容来自共享片段，本键是它这一实例的数据（见 attrCreateDrawerForm）。
		"AttrCreateForm": attrCreateDrawerForm(csrf, selected, tr),
		// 筛选回显（GET 表单的 value / selected）+ 空态分档依据：
		// 「筛出来是空的」与「这个工程本来就没有属性组」必须给不同文案。
		"FilterKeyword":   keyword,
		"FilterVariation": variation,
		"Filtered":        keyword != "" || variation != "",
		// 读侧一律过白名单（product_err.go）：查询参数不是可信边界。
		"Err": productPageErr(c),
		// 批量删除的结果回带（?done=）：部分失败仍走 err（见 ProductAttributesBulkDelete）。
		"Done": productPageDone(c),
	}
	// 分页条（shell 组件，服务端渲染）：基地址带上关键词与变体筛选，翻页不丢条件；
	// 单页或空数据时 BuildPagination 返回 nil，TemplateKeys 给空 map，模板自然不渲染。
	for k, v := range shell.BuildPagination(total, page, productSubListPageSize,
		productListBaseURL("/admin/product-attributes", attributeListFilterQuery(selected, keyword, variation)),
		shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/product_attributes.html", shell.Prepare(c, data))
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
		rows = append(rows, attrValueFormRow{Index: index, Enabled: true})
	case "remove":
		if idx := parseIntOr(c.PostForm("removeIndex"), -1); idx >= 0 && idx < len(rows) {
			rows = append(rows[:idx], rows[idx+1:]...)
		}
	}
	// 兜底：至少要有一行可编辑，空列表会让「添加」按钮无从下手。
	if len(rows) == 0 {
		rows = []attrValueFormRow{{Enabled: true}}
	}
	groupID := strings.TrimSpace(c.PostForm("groupId"))
	if groupID == "" {
		groupID = "new"
	}
	c.HTML(http.StatusOK, "admin/product/product_attribute_rows.html", attrRowsCtx{
		GroupID: groupID,
		Rows:    rows,
		Tr:      shell.TranslateFor(c),
	})
}

// attrValueFormRow 留存失败回显所需的原始索引与控件值；写入解析仍保持原有的空行过滤。
type attrValueFormRow struct {
	Index   int
	ID      string
	Key     string
	Label   string
	Sort    string
	Enabled bool
}

func attrRowsFromResp(rows []productdto.AttributeValueResp) []attrValueFormRow {
	out := make([]attrValueFormRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, attrValueFormRow{
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

func attrEchoRowsFromForm(c *gin.Context) []attrValueFormRow {
	vals := attrFormValues(c)
	rows := make([]attrValueFormRow, 0)
	for _, idx := range attrFormRowIndexes(vals) {
		base := fmt.Sprintf("values[%d].", idx)
		rows = append(rows, attrValueFormRow{
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

// attrRowsCtx 属性值编辑片段（include）的数据类。
//
// GroupID 既标识「这是哪个组的编辑器」，也是片段拼容器 id / hx-target 的依据
// （attr-values-<GroupID>）—— 片段的 hx-include 会自动带上按钮自身的 groupId，
// 故客户端不需要额外传参。
type attrRowsCtx struct {
	GroupID string
	Rows    []attrValueFormRow
	// Tr 是本片段的取词函数。片段的两条渲染路径都**直接传本结构体**（HTMX 片段端点用
	// 字面量、抽屉表单 include 传 .RowsCtx），而 Jet 在 struct 上不支持 `.["t"]`
	//（渲染时报 can't use t as field name in struct type），取词函数只能随数据类一起传
	// —— 与 attrGroupFormOpts.Tr 同一形态。
	//
	// 用 shell.TranslateFor(c) 填（按请求语言）。**每个构造点都要给**：零值时片段里的
	// tr(...) 是 nil 调用，会 panic。
	Tr func(key, fallback string) string
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
//   - 失败：htmx 档 200 + 回填片段（错误槽 + 用户刚填的字段与值行），原生档 302 + ?err=；
//   - 成功：redirectWhere —— htmx 的 XHR 会自己跟随 302，最终响应里读不到 Location，
//     整页 HTML 会被塞进抽屉里，只有 HX-Redirect 能让它整页跳转。
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
		h.attrGroupFormFail(c, attrGroupModeCreate, projectID, "new", productErrText(c, err))
		return
	}
	redirectWhere(c, productAttributeListURL(projectID, ""))
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
		h.attrGroupFormFail(c, attrGroupModeEdit, projectID, id, productErrText(c, err))
		return
	}
	redirectWhere(c, productAttributeListURL(projectID, ""))
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
		h.attrValuesFormFail(c, projectID, groupID, productErrText(c, err))
		return
	}
	redirectWhere(c, productAttributeListURL(projectID, ""))
}

// ProductAttributesDelete 删除属性组（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductAttributesDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteAttribute(c.Request.Context(), &productdto.DeleteAttributeReq{ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID)
}

// ProductAttributesBulkDelete 批量删除属性组（连同其全部属性值，由 service 保证）。
//
// 逐条走同一条删除路径：被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductAttributesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, "/admin/product-attributes?project="+url.QueryEscape(projectID)+
			"&err="+url.QueryEscape(productErrText(c, berr)))
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
	target := "/admin/product-attributes?project=" + url.QueryEscape(projectID)
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productAttrBulkPartial),
			strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productAttrBulkDone), strconv.Itoa(deleted)))
	}
	c.Redirect(http.StatusFound, target)
}

// parseIntOr 解析十进制整数，失败返回兜底值（后台表单容错，不因一个脏字段 500）。
func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}
