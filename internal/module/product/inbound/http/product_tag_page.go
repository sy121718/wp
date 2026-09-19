// product_tag_handle.go — 后台商品标签管理页（issue #11）。
//
// 与属性 / 分类 / 品牌页同一模式：GET 渲染完整页，POST 处理完 302 回列表，
// 错误经 ?err= 回显（原生表单 + csrf_token 隐藏域）。只用 GET/POST。
//
// 本页承载本票的验收 1 / 2 / 4：
//
//	· 建手工标签并挂到商品（挂载表单在商品**详情页**的「商品标签」区块 ——
//	  标签归属是商品的属性，列表页只回答「有哪些商品」，见 admin-ui-logic §1）；
//	· 自动标签只给内置规则类型 + 白名单参数（规则类型下拉来自 service 的注册表，
//	  参数输入框固定三格：days / minPrice / maxPrice，服务端按规则类型取值并严格校验）；
//	· 标签列表用标准表格（标签 / URL 段 / 类型 / 规则 / 命中 / 重算时间 / 操作），
//	  「命中的商品」是一张平坦关系表，规则标签额外显示规则描述与重算时间。
//
// 重算时机在页面上写明（商品/变体写操作后、标签定义变更后、这里的「重算」按钮）。
package producthttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/web/shell"
)

// tagForm 标签表单的取值（创建与更新共用）。
type tagForm struct {
	name     string
	slug     string
	kind     string
	ruleType string
	params   json.RawMessage
	sort     int
}

// ProductTagsPage 标签管理页：工程切换 + 新建表单 + 规则类型说明 + 标签列表（含命中商品）。
func (h *productPageHandle) ProductTagsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_tag", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	tags, terr := h.listTags(ctx, selected)
	if terr != nil {
		shell.PageError(c, "product_tag", terr)
		return
	}
	// 每个标签再取一次详情拿命中商品（验收 4）：列表接口为了保持轻量只给数量，
	// 后台页要把命中商品直接铺在展开区里。标签数量是个位数，逐条取可以接受。
	rows := make([]gin.H, 0, len(tags))
	for _, t := range tags {
		detail, derr := h.products.GetTag(ctx, &productdto.GetTagReq{ProjectID: selected, ID: t.ID})
		if derr != nil {
			// 单个标签读失败不该让整页打不开：退回列表态（数量在、命中列表为空）。
			rows = append(rows, tagPageRow(t))
			continue
		}
		rows = append(rows, tagPageRow(detail))
	}
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口。
	c.HTML(http.StatusOK, "admin/product_tags.html", shell.Prepare(c, gin.H{
		"title":           "商品标签",
		"menu":            "product-tags",
		"Projects":        projects,
		"SelectedProject": selected,
		"Tags":            rows,
		"RuleTypes":       h.products.ListTagRuleTypes(ctx),
		// 读侧一律过白名单（product_err.go）：查询参数不是可信边界。
		"Err": productPageErr(c),
		// 批量删除的结果回带（?done=）：部分失败仍走 err（见 ProductTagsBulkDelete）。
		"Done": productPageDone(c),
	}))
}

// ProductTagsCreate 新建标签（手工 / 自动；自动标签建好即按规则重算一次）。
func (h *productPageHandle) ProductTagsCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	form := readTagForm(c)
	req := &productdto.CreateTagReq{
		ProjectID: projectID, Name: form.name, Slug: form.slug,
		Kind: form.kind, RuleType: form.ruleType, RuleParams: form.params, Sort: form.sort,
	}
	if _, err := h.products.CreateTag(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID)
}

// ProductTagsUpdate 修改标签（改名 / 换 slug / 换类型 / 改规则参数 / 排序）。
func (h *productPageHandle) ProductTagsUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	form := readTagForm(c)
	req := &productdto.UpdateTagReq{
		ProjectID: projectID,
		ID:        c.PostForm("id"), Name: &form.name, Slug: &form.slug,
		Kind: &form.kind, Sort: &form.sort,
	}
	// 只有自动标签才带规则定义：手工标签提交时规则字段一律不传，
	// 服务端据此把 rule→manual 的切换收敛成「清掉规则定义」。
	if form.kind == productenums.TagKindRule {
		req.RuleType = &form.ruleType
		req.RuleParams = form.params
	}
	if _, err := h.products.UpdateTag(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID)
}

// ProductTagsDelete 删除标签（服务端会把商品上的引用一起解绑）。
func (h *productPageHandle) ProductTagsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteTag(c.Request.Context(), &productdto.DeleteTagReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID)
}

// ProductTagsBulkDelete 批量删除标签（手工 / 自动一视同仁：删除即解绑商品上的该标签）。
//
// 逐条走同一条删除路径：失败的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductTagsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	target := "/admin/product-tags?project=" + url.QueryEscape(projectID)
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		c.Redirect(http.StatusFound, target+"&err="+url.QueryEscape(productErrText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteTag(c.Request.Context(), &productdto.DeleteTagReq{ProjectID: projectID, ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	switch {
	case skipped > 0:
		target += "&err=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productTagBulkPartial),
			strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		target += "&done=" + url.QueryEscape(fmt.Sprintf(productBulkTextOf(c, productTagBulkDone), strconv.Itoa(deleted)))
	}
	c.Redirect(http.StatusFound, target)
}

// ProductTagsRecalc 手动重算（tagId 为空即重算该工程全部自动标签）。
func (h *productPageHandle) ProductTagsRecalc(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.RecalcTagsReq{
		ProjectID: projectID,
		TagID:     strings.TrimSpace(c.PostForm("tagId")),
	}
	if _, err := h.products.RecalcTags(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID+"&err="+url.QueryEscape(productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/product-tags?project="+projectID)
}

// ProductsTagsSet 整体替换某商品的手工标签（issue #11 验收 1 的后台入口）。
//
// 一个都不勾时浏览器不发该字段，而这里「一个都不勾」是明确的「解绑全部手工标签」，
// 故把 nil 归一成空切片（与分类的「整体替换」语义一致）。自动标签不在这份表单里：
// 它们的归属由重算维护，服务端也不会接受手工挂载。
func (h *productPageHandle) ProductsTagsSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	tagIDs := c.PostFormArray("tagIds")
	if tagIDs == nil {
		tagIDs = []string{}
	}
	req := &productdto.UpdateReq{ProjectID: projectID, ID: formProductID(c), TagIDs: tagIDs}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ID, productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, productDetailLocation(projectID, req.ID, ""))
}

// listTags 取某工程的标签列表（工程为空时返回空列表）。
func (h *productPageHandle) listTags(ctx context.Context, projectID string) (out []*productdto.TagResp, err error) {
	if projectID == "" {
		return []*productdto.TagResp{}, nil
	}
	return h.products.ListTags(ctx, &productdto.ListTagReq{ProjectID: projectID})
}

// readTagForm 读标签表单。
//
// 规则参数只在 kind=rule 时构造：手工标签即使表单里残留了 days / 价格输入也不带过去，
// 否则「切回手工」会被参数校验挡住（服务端对手工标签带规则是明确拒绝的）。
func readTagForm(c *gin.Context) tagForm {
	form := tagForm{
		name: strings.TrimSpace(c.PostForm("name")),
		slug: strings.TrimSpace(c.PostForm("slug")),
		kind: strings.TrimSpace(c.PostForm("kind")),
		sort: parseIntOr(c.PostForm("sort"), 0),
	}
	if form.kind == "" {
		form.kind = productenums.TagKindManual
	}
	if form.kind != productenums.TagKindRule {
		return form
	}
	form.ruleType = strings.TrimSpace(c.PostForm("ruleType"))
	params, err := tagRuleParamsFromForm(c, form.ruleType)
	if err != nil {
		// 参数解析失败时给空对象：真正的校验（键 / 类型 / 取值范围）在 service，
		// 由它给出统一的业务错误，这里不重复一套规则。
		params = json.RawMessage("{}")
	}
	form.params = params
	return form
}

// tagRuleParamsFromForm 按规则类型从表单拼参数对象。
//
// 三个输入框（days / minPrice / maxPrice）与规则类型一一对应，服务端只取本类型用得到的键 ——
// 「自动标签只接受内置参数」在后台这一侧同样成立（多余输入不会进 params）。
// 非法数字（如 days=abc）返回错误，由调用方落成空对象交给 service 报参数错误。
func tagRuleParamsFromForm(c *gin.Context, ruleType string) (raw json.RawMessage, err error) {
	params := map[string]any{}
	switch ruleType {
	case productenums.TagRuleNewArrival:
		if v := strings.TrimSpace(c.PostForm("days")); v != "" {
			n, perr := strconv.Atoi(v)
			if perr != nil {
				return nil, errors.New(productenums.ErrTagRuleParamsInvalid)
			}
			params["days"] = n
		}
	case productenums.TagRulePriceRange:
		if v := strings.TrimSpace(c.PostForm("minPrice")); v != "" {
			f, perr := parseFloat(v)
			if perr != nil {
				return nil, errors.New(productenums.ErrTagRuleParamsInvalid)
			}
			params["minPrice"] = f
		}
		if v := strings.TrimSpace(c.PostForm("maxPrice")); v != "" {
			f, perr := parseFloat(v)
			if perr != nil {
				return nil, errors.New(productenums.ErrTagRuleParamsInvalid)
			}
			params["maxPrice"] = f
		}
	case productenums.TagRuleOnSale:
		// 无参数规则：空对象即可（给了也不会有其它键）。
	default:
		// 未知规则类型：参数留空，由 service 给出「规则类型不合法」。
	}
	return json.Marshal(params)
}

// tagPageRow 标签 → 模板行（规则描述与命中商品都由服务端算好，模板不做第二套解释）。
func tagPageRow(t *productdto.TagResp) gin.H {
	row := gin.H{
		"ID": t.ID, "Name": t.Name, "Slug": t.Slug, "Kind": t.Kind,
		"KindLabel": tagKindLabel(t.Kind), "IsRule": t.Kind == productenums.TagKindRule,
		"RuleType": t.RuleType, "RuleLabel": t.RuleLabel,
		"RecalcAt":     recalcLabel(t.RecalcAt),
		"ProductCount": t.ProductCount, "Sort": t.Sort,
		"Products":      tagProductRows(t.Products),
		"HasRuleParams": t.Kind == productenums.TagKindRule,
		"RuleDays":      ruleParamText(t.RuleParams, "days"),
		"RuleMinPrice":  ruleParamText(t.RuleParams, "minPrice"),
		"RuleMaxPrice":  ruleParamText(t.RuleParams, "maxPrice"),
	}
	row["KindRule"] = productenums.TagKindRule
	row["KindManual"] = productenums.TagKindManual
	return row
}

// tagProductRows 命中商品 → 模板行。
func tagProductRows(items []*productdto.TagProductResp) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, p := range items {
		out = append(out, gin.H{"ID": p.ID, "Name": p.Name, "Slug": p.Slug, "Status": p.Status})
	}
	return out
}

// tagKindLabel 标签类型的中文标签。
func tagKindLabel(kind string) string {
	if kind == productenums.TagKindRule {
		return "自动"
	}
	return "手工"
}

// recalcLabel 重算时间的展示文本（手工标签或从未重算时给一句可读说明）。
//
// 输入是 RFC3339，这里压成「2006-01-02 15:04」：既好读，又不会因为那一长串
// 时间戳在窄屏折叠行里连成不可断行的 token 把卡片撑破。
func recalcLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "未按规则重算过"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// ruleParamText 从规则参数里取某个键的文本（缺失返回空串，用于表单回填）。
func ruleParamText(params json.RawMessage, key string) string {
	if len(params) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	return s
}

// checkedTagOptions 商品页的手工标签勾选框（勾选态由服务端算好，模板不做集合运算）。
func checkedTagOptions(tags []*productdto.TagResp, attached []string) []gin.H {
	have := map[string]bool{}
	for _, id := range attached {
		have[id] = true
	}
	out := make([]gin.H, 0, len(tags))
	for _, t := range tags {
		if t == nil || t.Kind != productenums.TagKindManual {
			continue
		}
		out = append(out, gin.H{"ID": t.ID, "Name": t.Name, "Checked": have[t.ID]})
	}
	return out
}

// attachedAutoTags 商品已归属的自动标签（只读展示：归属由规则重算维护）。
func attachedAutoTags(tags []*productdto.TagResp, attached []string) []gin.H {
	have := map[string]bool{}
	for _, id := range attached {
		have[id] = true
	}
	out := make([]gin.H, 0, len(tags))
	for _, t := range tags {
		if t == nil || t.Kind != productenums.TagKindRule || !have[t.ID] {
			continue
		}
		out = append(out, gin.H{"ID": t.ID, "Name": t.Name, "RuleLabel": t.RuleLabel})
	}
	return out
}
