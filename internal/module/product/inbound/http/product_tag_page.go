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
//	  规则标签额外显示规则描述与重算时间；「命中的商品」按需加载（审计 PERF-02）：
//	  首屏只给数量，展开某个标签才按页取片段，见 ProductTagHitsFragment。
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

	"go_wp/internal/middleware/builtin"
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

// ProductTagsPage 标签管理页：工程切换 + 筛选栏 + 新建表单 + 规则类型说明 + 标签列表（含命中商品）。
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
	keyword := strings.TrimSpace(c.Query("keyword"))
	// 标签列表**分页下推到 service**（审计 D13 收口）：请求类型自带 Page/Size，总数由契约的
	// CountTags 给出（与 ListTags 同一份过滤条件），handler 不再「全量取回再切片」。
	// 命中数仍是 service 的一次批量聚合（审计 PERF-02）——分页后只聚合当页标签，
	// 页面 SQL 条数与标签总数无关这一条不变。
	//
	// 顺序是**先计数再取页**（理由同属性页 / 品牌页）：越界页码先收敛，否则会出现
	// 「表格为空、分页条却显示第 2 页」。
	page := productPageNumber(c.Query("page"))
	total := int64(0)
	rows := []gin.H{}
	var tagCreateForm gin.H
	// RuleTypes 全页只查一次：行级编辑片段、页面级新建片段与页面下拉共享同一份清单，
	// 逐行各查一遍是纯浪费（audit 同款：命中商品曾因每行一次 GetTag 被打回）。
	ruleTypes := h.products.ListTagRuleTypes(ctx)
	if selected != "" {
		// 过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size。
		filterReq := &productdto.ListTagReq{ProjectID: selected, Keyword: keyword}
		n, cerr := h.products.CountTags(ctx, filterReq)
		if cerr != nil {
			shell.PageError(c, "product_tag", cerr)
			return
		}
		total = n
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		list, lerr := h.products.ListTags(ctx, &listReq)
		if lerr != nil {
			shell.PageError(c, "product_tag", lerr)
			return
		}
		rows = make([]gin.H, 0, len(list))
		for _, t := range list {
			row := tagPageRow(shell.TranslateFor(c), t)
			row["EditForm"] = h.tagDrawerData(c, "update", selected, row, ruleTypes)
			rows = append(rows, row)
		}
		// 新建抽屉的片段数据与行级片段同源：同一份规则清单、同一份模板。
		tagCreateForm = h.tagDrawerData(c, "create", selected, nil, ruleTypes)
	}
	// 命中商品**不在这里取**（审计 PERF-02）：此前对每个标签再调一次 GetTag 拿命中商品，
	// 页面 SQL 条数随标签数线性增长；而「标签是个位数」只是当时的假设，协议没有使它成立。
	// 现在首屏只发「工程列表 + 标签总数 + 标签列表（当页）+ 一次批量计数」，命中商品由
	// 展开区按页拉片段（见 ProductTagHitsFragment）—— 1 / 100 / 1000 个标签的首屏 SQL 条数一样。
	// 注意不要用「开 goroutine 并发 N 次查询」来掩盖它：那是把 N 条 SQL 并行发出去，
	// 连接池压力与总条数都没变。
	//
	// 总数已由契约的 CountTags 给出（与 ListTags 同一份过滤条件：工程 + kind + 关键词）。
	// 注意它与「命中商品数」那一列是两回事：那一列是每个标签归属的商品数
	//（service 里一次批量聚合），本页面的分页只按标签条数算总页数。
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口。
	data := gin.H{
		"title":           shell.TranslateFor(c)(productenums.ProductTagsTitle, "商品标签"),
		"menu":            "product-tags",
		"Projects":        projects,
		"SelectedProject": selected,
		"Tags":            rows,
		"TagCreateForm":   tagCreateForm,
		"RuleTypes":       ruleTypes,
		// 筛选回显（GET 表单的 value）+ 空态分档依据：见 product_taxonomy_page.go 的同一手法。
		"FilterKeyword": keyword,
		"Filtered":      keyword != "",
		// 读侧一律过白名单（product_err.go）：查询参数不是可信边界。
		"Err": productPageErr(c),
		// 批量删除的结果回带（?done=）：部分失败仍走 err（见 ProductTagsBulkDelete）。
		"Done": productPageDone(c),
	}
	for k, v := range shell.BuildPagination(total, page, productSubListPageSize,
		productListBaseURL("/admin/product-tags", listFilterQuery(selected, keyword)),
		shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/product_tags.html", shell.Prepare(c, data))
}

// ProductTagHitsFragment 标签「命中商品」片段（审计 PERF-02 的展开区）。
//
// 为什么按需取：标签页首屏此前对每个标签取一次命中商品（GetTag），SQL 条数与标签数
// 成正比，且命中数据（每标签最多 500 行）会一起撑大页面。现在首屏只给数量，
// 展开某个标签时才发这一组查询（标签存在性 + 总数 + 本页行），与页面上有多少标签无关。
//
// 归属：挂在后台**页面组**（Session + CSRF），不叠加 Casbin 权限点 —— 与同组的
// /products/seo-score、/products/variant/preview 同一先例：它是只读渲染，不落任何库
// （写面仍然逐个挂 Casbin），因此不需要新权限点，也不会出现「有路由无权限点 ⇒ 含超管
// 全员 403」。渲染出的商品行只读，行内没有任何写入口。
//
// 失败也回 200 的片段（不是整页错误页）：它替换的是页面里的一小块，
// 回整页 HTML 会把展开区之外的内容一起换掉；错误文案过 productErrText 白名单。
func (h *productPageHandle) ProductTagHitsFragment(c *gin.Context) {
	req := &productdto.ListTagProductsPageReq{
		TagID:     strings.TrimSpace(c.Query("id")),
		ProjectID: strings.TrimSpace(c.Query("project")),
		Page:      parseIntOr(c.Query("page"), 1),
		Size:      parseIntOr(c.Query("size"), 0),
	}
	data := gin.H{
		"TagID":     req.TagID,
		"ProjectID": req.ProjectID,
	}
	res, err := h.products.ListTagProductsPage(c.Request.Context(), req)
	if err != nil {
		data["Err"] = productErrText(c, err)
		c.HTML(http.StatusOK, "admin/product/product_tag_hits.html", shell.Prepare(c, data))
		return
	}
	data["TagName"] = res.TagName
	data["Total"] = res.Total
	data["Page"] = res.Page
	data["PageSize"] = res.PageSize
	data["TotalPage"] = res.TotalPage
	data["Items"] = tagProductRows(res.Items)
	// 分页条文案复用 shell 的同一词条（shell.pagination.info）：后台各处的
	// 「共 N 条，第 X-Y 条」只有这一份取法，片段里再造一句就会与列表页不一致。
	data["PageInfo"] = tagHitsPageInfo(c, res.Total, res.Page, res.PageSize, len(res.Items))
	// 翻页链接由服务端算好（页数边界只有一处判断）：模板不做「还有没有下一页」的推断。
	if res.Page > 1 {
		data["PrevURL"] = tagHitsURL(req.ProjectID, res.TagID, res.Page-1)
	}
	if res.Page < res.TotalPage {
		data["NextURL"] = tagHitsURL(req.ProjectID, res.TagID, res.Page+1)
	}
	c.HTML(http.StatusOK, "admin/product/product_tag_hits.html", shell.Prepare(c, data))
}

// tagHitsPageInfo 命中商品片段的分页文案（「共 N 条，第 X-Y 条」）。
//
// 复用 shell.pagination.info 这一个词条（占位符统一 %s，走 strconv 填数字），
// 与 buildPagination 的取法一致 —— 两处各写一份的后果是静默的：
// 后台列表页改了措辞，展开区还是旧句子。
func tagHitsPageInfo(c *gin.Context, total, page, size, items int) string {
	from, to := 0, 0
	if items > 0 {
		from = (page-1)*size + 1
		to = from + items - 1
	}
	return fmt.Sprintf(shell.TranslateFor(c)("shell.pagination.info", "共 %s 条，第 %s-%s 条"),
		strconv.Itoa(total), strconv.Itoa(from), strconv.Itoa(to))
}

// tagHitsURL 命中商品片段的翻页地址（展开区用 hx-get 打回本片段，不走整页）。
func tagHitsURL(projectID, tagID string, page int) string {
	q := url.Values{}
	q.Set("project", projectID)
	q.Set("id", tagID)
	q.Set("page", strconv.Itoa(page))
	return "/admin/product-tags/hits?" + q.Encode()
}

// tagDrawerData 装配标签编辑片段数据（create 时 row 为 nil）。
// ruleTypes 由调用方传入：页面装配时取一次全行共享，失败分支自己取一次，
// 不在片段装配里重复查契约。CSRF 直接取上下文令牌，不走整份 Prepare（避免逐行重复装配导航）。
func (h *productPageHandle) tagDrawerData(c *gin.Context, mode, projectID string, row gin.H, ruleTypes []*productdto.TagRuleTypeResp) gin.H {
	csrf, _ := builtin.GetCSRFToken(c)
	data := gin.H{
		"Mode": mode, "Project": projectID, "Csrf": csrf, "t": shell.TranslateFor(c),
		"RuleTypes": ruleTypes,
		"ID": "", "Name": "", "Slug": "", "IsRule": false, "Sort": 0,
		"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
	}
	if row != nil {
		for _, key := range []string{"ID", "Name", "Slug", "IsRule", "Sort", "RuleType", "RuleDays", "RuleMinPrice", "RuleMaxPrice"} {
			data[key] = row[key]
		}
	}
	return data
}

// tagFormFail 写失败分档：htmx 请求 200 + 片段自身（错误槽 + 原值回填），
// 原生提交维持 302 + ?err= 回本页 —— 用户输入比错误文案贵，两种档都不丢字段。
func (h *productPageHandle) tagFormFail(c *gin.Context, mode string, err error) {
	projectID := c.PostForm("projectId")
	msg := productErrText(c, err)
	if !isHXRequest(c) {
		c.Redirect(http.StatusFound, "/admin/product-tags?project="+url.QueryEscape(projectID)+"&err="+url.QueryEscape(msg))
		return
	}
	data := h.tagDrawerData(c, mode, projectID, nil, h.products.ListTagRuleTypes(c.Request.Context()))
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "kind", "sort", "ruleType", "days", "minPrice", "maxPrice"})
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_tag_form.html", data)
}

// tagFormSuccess 写成功分档：htmx 走 HX-Redirect（XHR 会跟随 302，读不到 Location），
// 原生提交维持既有 302 回列表。
func tagFormSuccess(c *gin.Context, projectID string) {
	redirectWhere(c, "/admin/product-tags?project="+url.QueryEscape(projectID))
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
		h.tagFormFail(c, "create", err)
		return
	}
	tagFormSuccess(c, projectID)
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
		h.tagFormFail(c, "update", err)
		return
	}
	tagFormSuccess(c, projectID)
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
		c.Redirect(http.StatusFound, productEditLocation(projectID, req.ID, productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, productEditLocation(projectID, req.ID, ""))
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

// tagPageRow 标签 → 模板行（规则描述与命中数都由服务端算好，模板不做第二套解释；
// 命中商品本身不在这里，展开时才由 ProductTagHitsFragment 给 —— 审计 PERF-02）。
func tagPageRow(tr func(key, fallback string) string, t *productdto.TagResp) gin.H {
	row := gin.H{
		"ID": t.ID, "Name": t.Name, "Slug": t.Slug, "Kind": t.Kind,
		"KindLabel": tagKindLabel(tr, t.Kind), "IsRule": t.Kind == productenums.TagKindRule,
		"RuleType": t.RuleType, "RuleLabel": t.RuleLabel,
		"RecalcAt":     recalcLabel(tr, t.RecalcAt),
		"ProductCount": t.ProductCount, "Sort": t.Sort,
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

// tagKindLabel 标签类型 → 当前语言标签。
func tagKindLabel(tr func(key, fallback string) string, kind string) string {
	if kind == productenums.TagKindRule {
		return tr(productenums.ProductTagsKindRule, "自动")
	}
	return tr(productenums.ProductTagsKindManual, "手工")
}

// recalcLabel 重算时间的展示文本（手工标签或从未重算时给一句可读说明）。
//
// 输入是 RFC3339，这里压成「2006-01-02 15:04」：既好读，又不会因为那一长串
// 时间戳在窄屏折叠行里连成不可断行的 token 把卡片撑破。
// 时间是数据（格式固定），只有「没重算过」那句话是文案，走词条。
func recalcLabel(tr func(key, fallback string) string, at string) string {
	if strings.TrimSpace(at) == "" {
		return tr(productenums.ProductTagsRecalcNone, "未按规则重算过")
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
