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
	"go_wp/internal/web/shell"
)

// attrRowPrefix 属性值行的表单字段前缀（values[0].id → "values[0]."）。
const attrRowPrefix = "values["

// ProductAttributesPage 属性管理页：工程切换 + 属性组列表 + 值编辑器 + 内联新建表单。
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
	rows := make([]gin.H, 0, 50)
	if selected != "" {
		list, lerr := h.products.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: selected, Size: 200})
		if lerr != nil {
			shell.PageError(c, "product_attribute", lerr)
			return
		}
		for _, a := range list {
			rows = append(rows, gin.H{
				"ID": a.ID, "Key": a.Key, "Name": a.Name,
				"IsVariation": a.IsVariation, "Sort": a.Sort,
				"ValueCount": a.ValueCount,
				// RowsCtx 是值编辑器片段（include）的数据类：
				// 容器 id 按约定 attr-values-<GroupID>，片段据此定位 hx-target。
				"RowsCtx": attrRowsCtx{GroupID: a.ID, Rows: a.Values},
			})
		}
	}
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言。
	c.HTML(http.StatusOK, "admin/product_attributes.html", shell.Prepare(c, gin.H{
		"title":           "商品属性",
		"menu":            "product-attributes",
		"Projects":        projects,
		"SelectedProject": selected,
		"Attributes":      rows,
		"NewRowsCtx":      attrRowsCtx{GroupID: "new", Rows: nil},
		// 读侧一律过白名单（product_err.go）：查询参数不是可信边界。
		"Err": productPageErr(c),
		// 批量删除的结果回带（?done=）：部分失败仍走 err（见 ProductAttributesBulkDelete）。
		"Done": productPageDone(c),
	}))
}

// ProductAttributesValueRows 值编辑器片段：add / remove 一次，回渲染整段行列表。
//
// 行数据由服务端从「表单里已有的行」重建（有序），保证归一 / 去重 / 排序
// 只有一份实现（service 的 sanitizeAttributeValues），前端不做第二套。
func (h *productPageHandle) ProductAttributesValueRows(c *gin.Context) {
	rows := attrRowsFromForm(c)
	action := strings.TrimSpace(c.PostForm("action"))
	switch action {
	case "add":
		rows = append(rows, productdto.AttributeValueReq{})
	case "remove":
		if idx := parseIntOr(c.PostForm("removeIndex"), -1); idx >= 0 && idx < len(rows) {
			rows = append(rows[:idx], rows[idx+1:]...)
		}
	}
	// 兜底：至少要有一行可编辑，空列表会让「添加」按钮无从下手。
	if len(rows) == 0 {
		rows = []productdto.AttributeValueReq{{}}
	}
	groupID := strings.TrimSpace(c.PostForm("groupId"))
	if groupID == "" {
		groupID = "new"
	}
	c.HTML(http.StatusOK, "admin/partials/product_attribute_rows.html", attrRowsCtx{
		GroupID: groupID,
		Rows:    rowsToResp(rows),
	})
}

// rowsToResp 把表单行数据转成模板数据（只影响回渲染的形态，不落库）。
//
// 归一 / 去重 / 排序的真源在 service（sanitizeAttributeValues），这里只做
// 「表单里怎么填就怎么回显」的直译，避免出现第二套归一规则。
func rowsToResp(rows []productdto.AttributeValueReq) []productdto.AttributeValueResp {
	out := make([]productdto.AttributeValueResp, 0, len(rows))
	for _, r := range rows {
		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}
		out = append(out, productdto.AttributeValueResp{
			ID: r.ID, Key: r.Key, Label: r.Label, Sort: r.Sort, Enabled: enabled,
		})
	}
	return out
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
	Rows    []productdto.AttributeValueResp
}

// attrRowsFromForm 从表单的 values[n].* 字段重建有序行数据。
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
	order := make([]string, 0, 8)
	seen := map[string]bool{}
	for key := range reqs {
		if !strings.HasPrefix(key, attrRowPrefix) {
			continue
		}
		rest := key[len(attrRowPrefix):]
		close := strings.Index(rest, "]")
		if close <= 0 {
			continue
		}
		idx := rest[:close]
		if !seen[idx] {
			seen[idx] = true
			order = append(order, idx)
		}
	}
	// 数值升序 = 浏览器提交表单时的文档顺序（行不会随机换位）。
	sort.Slice(order, func(i, j int) bool {
		ni, ierr := strconv.Atoi(order[i])
		nj, jerr := strconv.Atoi(order[j])
		if ierr != nil || jerr != nil {
			return order[i] < order[j]
		}
		return ni < nj
	})
	rows := make([]productdto.AttributeValueReq, 0, len(order))
	for _, idx := range order {
		base := attrRowPrefix + idx + "]."
		label := strings.TrimSpace(reqs.Get(base + "label"))
		id := strings.TrimSpace(reqs.Get(base + "id"))
		key := strings.TrimSpace(reqs.Get(base + "key"))
		if label == "" && id == "" && key == "" {
			continue
		}
		enabled := reqs.Get(base+"enabled") != ""
		rows = append(rows, productdto.AttributeValueReq{
			ID: id, Key: key, Label: label,
			Sort: parseIntOr(reqs.Get(base+"sort"), 0), Enabled: &enabled,
		})
	}
	return rows
}

// ProductAttributesCreate 新建属性组，完成后回到列表。
func (h *productPageHandle) ProductAttributesCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.CreateAttributeReq{
		ProjectID: projectID,
		Key:       c.PostForm("key"),
		Name:      c.PostForm("name"),
		Sort:      parseIntOr(c.PostForm("sort"), 0),
		Values:    attrRowsFromForm(c),
	}
	// 复选框未勾选时浏览器不发字段，不能与「没给」区分 —— 表单里用同名隐藏域打底，
	// 这里按字符串判别：隐藏域为 "0"，勾选后同名字段变 "1"。
	isVariation := c.PostForm("isVariation") == "1"
	req.IsVariation = &isVariation
	if _, err := h.products.CreateAttribute(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID)
}

// ProductAttributesUpdate 修改属性组本身（名称 / 标识 / 参与变体 / 排序）。
func (h *productPageHandle) ProductAttributesUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	id := c.PostForm("id")
	name := strings.TrimSpace(c.PostForm("name"))
	key := strings.TrimSpace(c.PostForm("key"))
	isVariation := c.PostForm("isVariation") == "1"
	sortV := parseIntOr(c.PostForm("sort"), 0)
	req := &productdto.UpdateAttributeReq{
		ID: id, Name: &name, Key: &key, IsVariation: &isVariation, Sort: &sortV,
	}
	if _, err := h.products.UpdateAttribute(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID)
}

// ProductAttributesSetValues 整体保存属性值（表单上已有的行就是全部值）。
func (h *productPageHandle) ProductAttributesSetValues(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.SetAttributeValuesReq{
		ID:     c.PostForm("id"),
		Values: attrRowsFromForm(c),
	}
	if _, err := h.products.SetAttributeValues(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-attributes?project="+projectID)
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
