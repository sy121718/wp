package producthttp

// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约
// （与 product_handle.go 同一模式，避免把商品依赖掺进 dashboard 通用装配）。
//
// 交互遵循后台规范：
//   - GET /admin/product-attributes 渲染完整页；
//   - POST 处理完渲染整页提示（shell.RenderJump，成功 1 秒后自动回列表页）；
//   - 属性值行的增删走 HTMX（hx-post → 片段），因为「编辑中的值」只存在于表单里，
//     必须服务端参与归一与去重（与设置页语言清单同一手法）。

// 「提交失败原地留住输入」出口（路径 A 渐进增强）。
//
// 形状与口径**照批 1 的商品建表单**（product_new_page.go + partials/product_create_form.html），
// 分档判据只有一份（product_page_util.go 的 isHXRequest / hxFragment / formEchoData）：
//
//	· htmx 请求：失败返 200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回列表页（取代原先的 302 + ?err=）；
//	· 成功出口：一律整页提示 —— htmx 由 shell.RenderJump 输出 HX-Redirect（整页跳转），
//	  原生渲染提示页。
//
// 为什么单独一个文件：属性页有三个抽屉表单（新建属性组 / 编辑属性组 / 保存属性值），
// 每个抽屉的数据既要在**页面首屏**渲染（初值 = 库里当前值），又要在**提交失败**时由
// handler 单独重渲染（值 = 用户刚打的字）。两处各拼一份 data 必然分叉，而分叉不会编译
// 失败 —— 只会让失败重渲染缺一个键，于是 Jet 在那一行渲染失败、**整块响应被丢弃（500）**，
// 而 htmx 把 5xx 判成不 swap：用户点「保存」后页面上毫无反应（比白页更难查）。
// 因此组装只有 attrGroupFormData / attrValuesFormData 两个入口，首屏与失败共用。

// 与商品页同族（挂在 productPageHandle 上）：选工程 → 选商品 → 配选项规则 → 保存。
// 表单是**原生并行数组**（variantId / required / defaultQty / minQty / maxQty 各自多值），
// 零自定义 JS 就能表达「一行的多个字段」—— 不用 JSON 字符串塞进隐藏域，
// 也就没有「前端拼 JSON 出错但服务端照单全收」的缝隙。
//
// 页面底部内嵌前台配置器片段（hx-get /_fragments/bundleConfigurator）：
// 运营在这里看到的就是访客看到的那一份渲染。

// 三个动作都只改「这一个商品」的呈现，不碰共享模板；原生表单 POST，结论由
// shell.RenderJump 渲染整页提示回编辑页，与商品页其它写动作同一形态
//（form 里带 csrf_token 隐藏域，路由挂 pages 组）：
//
//	POST /admin/products/reapply-preset     放弃独立文档，回到跟随模板（可反悔的另一半）
//	POST /admin/products/rollback-document  取历史快照的文档重发（重新编译，数据取最新）
//	POST /admin/products/rollback-artifact  产物指针回滚（秒级，不重新编译）
//
// 权限复用「商品更新」权限点（与详情页其它写动作一致）：路由上显式指定
// CasbinMiddlewareForPath("/api/product/update")，因此不需要新增权限点 seed。
//
// 成功回执用一条通用文案（页面上「模式徽标」本身就是结果：跟随模板 ↔ 独立文档）；
// 失败文案过白名单 + 归口（productErrText），原文只进日志。

// 定位（spec #2「商品展示资产的落点」延伸）：商品详情页是**内容模板（完整文档层）**。
// issue #6 落地了一种类级默认模板（迁移 085），本票把「用哪套模板」变成商品可见的选择：
//
//	· 同一实体类型（product）下可建多套**命名模板**，每套各自版本化
//	  （模板与版本是 contenttemplate 模块的两层：换一套模板换 TemplateID，
//	   改版式产生新版本，本页只做创建与查看；模板 Document 编辑走
//	   /workbench?template={templateId}（contenttemplate 模块，非商品页工作台））；
//	· 商品发布时可指定使用某套模板（首次发布 = create 带 templateId，
//	   已发布 = rebuild 带 templateId 切换绑定）；
//	· 发布前可预览：预览走 presentation.PreviewInstance（只读渲染，不落库不激活），
//	  页面把渲染结果直接输出到新标签页，看到的就是发布时会产出的字节；
//	· 未指定模板的实例仍按实体类型取默认模板（既有行为不变）。
//
// 交互遵循后台规范：GET 渲染完整页，POST 处理完渲染整页提示回页面（shell.RenderJump，
// 原生表单 + csrf_token 隐藏域），只用 GET/POST；预览表单 target=_blank 打开渲染结果。

// 为什么抽出来：详情页改为**只读**之后，这个面板被两页消费 ——
//
//	· 详情页：只读展示（模式徽标 / 当前模板 / 预览入口 / 未发布的引导语）；
//	· 编辑页：承担写动作（进入自定义 / 编辑模板 / 重新套用预设 / 回滚文档）。
//
// 两处各组装一遍必然分叉（典型形态：一边算了「影响 N 个商品」、另一边没算，
// 或者一边给了 Snapshots、另一边回滚下拉是空的），而分叉不会编译失败。

// 为什么单独成页，而不是并进商品详情页：
//   - 详情页的职能是**子资源维护**（变体清单 / 评分 / 属性引用 / 分类与品牌 / 手工标签），
//     那里的每个表单都作用在「这个商品的某个子资源」上；
//   - 商品**自身**的字段（名称 / URL 段 / SKU / 状态 / 价格 / 单位 / 重量 / SEO / 图集）
//     自本页出现之前**只有创建时能填**，建完之后再也没有入口 —— 编辑入口缺位是真实缺口，
//     不是版式问题；
//   - 商品域翻译工作台（多语言）原先占着列表操作列的一格，本批收进编辑页：多语言改的是
//     这个商品的字段译文，与「改这个商品」是同一件事。
//
// 表单协议：字段名与 /api/product/update 的 UpdateReq 的 json 标签**逐字对齐**
// （name / slug / sku / status / defaultPrice / unit / weight / seoTitle / seoDescription /
// images / imageAlts / attributeIds / categoryIds / primaryCategoryId / brandId / tagIds），
// 改字段名等于改协议。
//
// 提交后回编辑页（PRG）而不是回列表：用户在这里改的是**这一个商品**，
// 弹回列表等于让他重新找一遍再点进来（与详情页的写操作同一口径）。

// 左侧基础信息表单（字段名与原抽屉逐字一致，复用 /admin/products/create），
// 右侧模板卡片区（默认模板 + 候选列表 + 预览入口）；模板绑定与可视化自定义
// 在创建后的详情页继续（create 不落实例，绑定发生在首次发布）。

// i18n key 是字符串协议：这些 key 原登记在 dashboard/enums，页面回迁后随域归入本模块，
// 字面值与词条表（迁移 163 起）保持一致 —— 改 key 必须同步 sys_i18n 词条。

// 与属性 / 分类 / 品牌 / 标签页同一模式：GET 渲染完整页，POST 处理完渲染整页提示
// （shell.RenderJump，原生表单 + csrf_token 隐藏域）。只用 GET/POST。
//
// 三个与其它后台页不同的地方，都是本票的验收要求：
//
//  1. **预览**（验收 4）：POST /admin/product-pricing/preview 直接把试算结果渲染回同一页
//     （200，不是提示页）—— 预览必须能看到明细，而明细只在这一次响应里；
//     表单值原样回填，用户接着点「应用调价」用的就是刚刚预览过的那份参数。
//  2. **应用**（验收 3）：POST /admin/product-pricing/apply 走 service 落库 + 留痕，
//     完成后渲染整页提示（成功回执含本次改动的变体数）。
//  3. **留痕**（验收 4）：页面下半部分是调价台账，每个批次可展开看到逐变体「原价 → 新价」。

// 与属性 / 分类 / 品牌页同一模式：GET 渲染完整页，POST 处理完渲染整页提示
// （shell.RenderJump，原生表单 + csrf_token 隐藏域）。只用 GET/POST。
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

// 与属性页同一模式：独立于 dashboard 的通用 Handle，只依赖 product 契约与 project 契约；
// GET 渲染完整页，POST 处理完渲染整页提示（shell.RenderJump，原生表单 + csrf_token 隐藏域）。
//
// 分类树在服务端已算好层级（CategoryResp.Depth），这里只按 DFS 前序摊平给模板 ——
// 父子规则只有 service 一份，模板不做第二套。
//
// 列表页形态（审计 02-M 的 D12 / D13）：分类页与品牌页都带关键词筛选栏、按页渲染，
// 并在空态区分「筛出来是空的」与「工程里本来就没有」。
//
// 分类后台页一律按**树根**分页：浏览态是顶级分类，搜索态是命中所属的根分类，
// 每页都把该页的整棵树一次读出来交给模板渲染。逐层点进去的导航与子级懒加载已退役，
// 旧 ListCategories 全树契约仅保留给其它调用方。

// 为什么单独成文件：这一段是「仓库侧只回答这条货在这个仓叫什么」在商品页的唯一落点 ——
// 候选按仓分组、每仓有上限、未接库存契约时整块降级为空。与商品页其余部分（列表 / 详情 /
// 抽屉业务字段）没有耦合，放在一起只会让两边互相牵制。
//
// 前端只是便捷入口：ProductsCreate 会带着仓库 id 让 service 在**该仓**重新复核一遍。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/content/dto"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/contenttemplate/enums"
	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/module/product/model"
	"go_wp/internal/module/product/service"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/project/dto"
	"go_wp/internal/pipeline"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/internal/siteurl"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/money"
	"go_wp/pkg/response"
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
	// 筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 从它读回，
	// 回跳列表页不丢关键词 / 变体筛选 / 页码。
	listQuery := productQueryFromRequest(c, productAttrBackKeys...)
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
			editForm, valuesForm := attrRowDrawerForms(csrf, selected, listQuery, tr, a)
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
		"AttrCreateForm": attrCreateDrawerForm(csrf, selected, listQuery, tr),
		// 筛选回显（GET 表单的 value / selected）+ 空态分档依据：
		// 「筛出来是空的」与「这个工程本来就没有属性组」必须给不同文案。
		"FilterKeyword":   keyword,
		"FilterVariation": variation,
		"Filtered":        keyword != "" || variation != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go），
		// 所以模板里的提示条已整批删除 —— 用户当下看到的是列表本身。
		"ListQuery": listQuery,
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
		h.attrGroupFormFail(c, attrGroupModeCreate, projectID, "new", productErrText(c, err))
		return
	}
	productAttrJump(c, true, productActionDoneText(c))
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
	productAttrJump(c, true, productActionDoneText(c))
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
	productAttrJump(c, true, productActionDoneText(c))
}

// ProductAttributesDelete 删除属性组（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductAttributesDelete(c *gin.Context) {
	if err := h.products.DeleteAttribute(c.Request.Context(), &productdto.DeleteAttributeReq{ID: c.PostForm("id")}); err != nil {
		productAttrJump(c, false, productErrText(c, err))
		return
	}
	productAttrJump(c, true, productBulkDoneText(c, productAttrBulkDone))
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
		productAttrJump(c, false, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := productBulkDeleteResult(c, deleted, skipped, productAttrBulkPartial, productAttrBulkDone)
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
// values[n].* 不在这里：属性值行的回填走 RowsCtx（由 attrEchoRowsFromForm 按原始索引重建），
// 不是单值回填 —— 塞进 FormEcho 只会得到一串没人读的字符串。
var attrValuesFormFields = []string{"projectId", "id", "groupId"}

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
	// ListQuery 是属性页当前的筛选上下文（表单 action 的 query）——
	// 写动作失败时由 shell.BackPath 从它读回，回跳列表页不丢筛选。
	ListQuery string
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
		"Mode": o.Mode, "Action": withListQuery(attrGroupFormAction(o.Mode), o.ListQuery),
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
	// ListQuery 同 attrGroupFormOpts.ListQuery：失败回跳时保留列表筛选。
	ListQuery string
	Rows      attrRowsCtx
}

// attrValuesFormData 属性值抽屉表单的渲染数据（首屏与失败重渲染共用）。
//
// 回填的**主体是 RowsCtx**（用户刚编辑的那几行，由 attrEchoRowsFromForm 保留位置和原值）；
// FormEcho* 三键只承载 projectId / id / groupId 这几个隐藏域 —— 值表单的可见字段
// 全是 values[n].*，没有单值字段可回填。
func attrValuesFormData(o attrValuesFormOpts) gin.H {
	return gin.H{
		"Csrf": o.Csrf, "Project": o.ProjectID, "t": o.Tr,
		"GroupID": o.GroupID, "Action": withListQuery(attrValuesAction, o.ListQuery),
		"RowsCtx": o.Rows, "InDrawer": true,
	}
}

// attrCreateDrawerForm 「新建属性组」抽屉的表单数据（页头与空态两个入口共用同一份）。
func attrCreateDrawerForm(csrf, projectID, listQuery string, tr func(key, fallback string) string) gin.H {
	return attrGroupFormData(attrGroupFormOpts{
		Csrf: csrf, ProjectID: projectID, Tr: tr, ListQuery: listQuery,
		// 新建时没有可继承的组：勾选态沿用既有默认（勾上「参与变体」）；
		// 属性值行**首屏为空**（Rows: nil）：编辑器渲染 0 行 + 「还没有属性值…」的引导文案，
		// 用户点「+ 添加值」后由 ProductAttributesValueRows 兜底保证至少一行。两者口径**不同**，
		// 别按「同口径」推 —— 这里若补一行空输入，抽屉一打开就是一行空行配着引导文案，更费解。
		Mode: attrGroupModeCreate, GroupID: "new", Variation: true,
		Rows: attrRowsCtx{GroupID: "new", Rows: nil, Tr: tr},
	})
}

// attrRowDrawerForms 属性组列表里**一行**对应的两个抽屉表单数据（页面首屏）。
//
// 两个抽屉的初值都从同一行数据来：编辑抽屉取该组的名称 / 标识 / 排序 / 参与变体，
// 属性值抽屉取该组的属性值列表。它们与失败重渲染时的数据形状**逐键一致**
// （差别只在 FormEcho* 三键与 SubmitErr 的有无），片段因此只有一套渲染分支。
func attrRowDrawerForms(csrf, projectID, listQuery string, tr func(key, fallback string) string,
	a *productdto.AttributeResp) (gin.H, gin.H) {

	group := attrGroupFormData(attrGroupFormOpts{
		Csrf: csrf, ProjectID: projectID, Tr: tr, ListQuery: listQuery,
		Mode: attrGroupModeEdit, GroupID: a.ID,
		Name: a.Name, Key: a.Key, Sort: a.Sort, Variation: a.IsVariation,
	})
	values := attrValuesFormData(attrValuesFormOpts{
		Csrf: csrf, ProjectID: projectID, Tr: tr, GroupID: a.ID, ListQuery: listQuery,
		Rows: attrRowsCtx{GroupID: a.ID, Rows: attrRowsFromResp(a.Values), Tr: tr},
	})
	return group, values
}

// —— 失败出口（分档口径唯一，别在调用点各写一份头判断）——

// attrGroupFormFail 属性组表单（新建 / 编辑）的失败出口。
//
//	· htmx 请求：200 + 片段（错误槽 + 回填后的表单）—— 抽屉原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回属性页 —— 取代原先的 302 + ?err=。
//
// **成功路径也走同一条出口**（调用点用 productAttrJump）：见本文件顶部注释。
func (h *productPageHandle) attrGroupFormFail(c *gin.Context, mode, projectID, groupID, msg string) {
	if !isHXRequest(c) {
		productAttrJump(c, false, msg)
		return
	}
	tr := shell.TranslateFor(c)
	data := attrGroupFormData(attrGroupFormOpts{
		Csrf: attrFormCSRF(c), ProjectID: projectID, Tr: tr,
		ListQuery: productQueryFromRequest(c, productAttrBackKeys...),
		Mode:      mode, GroupID: groupID,
		// 新建抽屉按本次提交的原始位置和值重建，编辑抽屉没有值编辑器。
		Rows: attrRowsCtx{GroupID: groupID, Rows: attrEchoRowsFromForm(c), Tr: tr},
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
		productAttrJump(c, false, msg)
		return
	}
	tr := shell.TranslateFor(c)
	data := attrValuesFormData(attrValuesFormOpts{
		Csrf: attrFormCSRF(c), ProjectID: projectID, Tr: tr,
		GroupID:   groupID,
		ListQuery: productQueryFromRequest(c, productAttrBackKeys...),
		Rows:      attrRowsCtx{GroupID: groupID, Rows: attrEchoRowsFromForm(c), Tr: tr},
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

// ProductBundlePage 捆绑配置页。
func (h *productPageHandle) ProductBundlePage(c *gin.Context) {
	h.renderBundlePage(c, nil, "", false)
}

func (h *productPageHandle) renderBundlePage(c *gin.Context, submitted *productdto.BundleConfig, failure string, fragment bool) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_bundle", err)
		return
	}
	selectedProject := bundleQueryParam(c, "project", "projectId")
	if selectedProject == "" && len(projects) > 0 {
		selectedProject = projects[0].ID
	}
	var list []*productdto.ProductResp
	if selectedProject != "" {
		list, err = h.products.List(ctx, &productdto.ListReq{ProjectID: selectedProject, Size: 100})
		if err != nil {
			shell.PageError(c, "product_bundle", err)
			return
		}
	}
	selectedProduct := bundleQueryParam(c, "product", "productId")
	var detail *productdto.BundleConfigResp
	if selectedProduct != "" {
		detail, err = h.products.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{ProductID: selectedProduct})
		if err != nil {
			// 配置读不出来时仍渲染页面骨架。
			detail = nil
		}
	}
	if submitted != nil && detail != nil {
		detail.Config = *submitted
	}
	skus, serr := h.products.ListBundleSKUs(ctx, &productdto.ListBundleSKUReq{ProjectID: selectedProject})
	if serr != nil {
		shell.PageError(c, "product_bundle", serr)
		return
	}
	// —— 成员来源面板的数据（docs/14 §1.2 的三种来源，批次 C）——
	//
	// 来源 / 来源商品 / 来源仓都是**页级上下文**（与工程、商品同一层）：用 GET 重新渲染，
	// 而不是让前端拼参数 —— 属性值勾选清单与仓库 SKU 候选都由服务端给出，
	// 前端不需要复刻任何口径（它只负责把选中的行追加进成员清单）。
	tr := shell.TranslateFor(c)
	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		source = productenums.BundleSourceProduct
	}
	sourceProductID := strings.TrimSpace(c.Query("sourceProduct"))
	sourceWarehouseID := strings.TrimSpace(c.Query("sourceWarehouse"))
	sourceAttrs := h.bundleSourceAttributes(ctx, sourceProductID)
	warehouses, werr := h.warehouseOptions(ctx, selectedProject, shell.TranslateFor(c))
	if werr != nil {
		shell.PageError(c, "product_bundle", werr)
		return
	}
	// 仓库 SKU 候选**按仓分组**逐仓取（复用「从仓库选」的既有能力，见 product_warehouse_sku_view.go）：
	// 一次取全库再在内存分组会把整张库存表拉进内存，而这里只需要「最近常用的一屏」。
	warehouseGroups, gerr := h.warehouseSKUOptions(ctx, selectedProject, warehouses)
	if gerr != nil {
		shell.PageError(c, "product_bundle", gerr)
		return
	}
	template := "admin/product/product_bundle.html"
	if fragment {
		template = "admin/product/product_bundle_form.html"
	}
	// failure 只由**写动作失败重渲染**填（ProductBundleSave）——取代原先 302 + ?err= 的那条
	// 读侧判定已整批删除（见 product_jump.go 的文件头），首屏不再从 query 取任何提示。
	rows := bundleConfigRows(tr, detail, skus)
	if submitted != nil && detail != nil {
		rows = bundleSubmittedRows(tr, detail, skus, c)
	}
	c.HTML(http.StatusOK, template, shell.Prepare(c, gin.H{
		"title":           tr(productenums.ProductBundleTitle, "捆绑配置"),
		"menu":            "products",
		"Projects":        projects,
		"SelectedProject": selectedProject,
		"Products":        list,
		"SelectedProduct": selectedProduct,
		"Detail":          detail,
		"Rows":            rows,
		"CandidateSKUs":   bundleCandidateOptions(skus),
		"SkuCount":        len(skus),
		"MaxOptions":      productdto.BundleMaxOptionsLimit,
		"Err":             failure,
		// 成员来源面板（批次 C）：三种来源 + 各自的候选数据。
		"Sources":           bundleSourceOptions(tr, source),
		"Source":            source,
		"SourceProducts":    bundleSourceProductOptions(tr, list, sourceProductID),
		"SourceAttributes":  sourceAttrs,
		"SourceWarehouses":  bundleSourceWarehouseGroups(warehouseGroups, sourceWarehouseID),
		"SourceWarehouseID": sourceWarehouseID,
		"ResolveURL":        "/admin/products/bundle/members/resolve",
		// 页面上下文（表单 action 的 query）：保存失败 / 成功时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productBundleBackKeys...),
	}))
}

// ProductBundleSave 保存捆绑配置；失败在当前请求回填，HX 成功整页跳转。
func (h *productPageHandle) ProductBundleSave(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	if productID == "" {
		productBundleJump(c, false, shell.TranslateFor(c)(productBundleNoProductKey, productBundleNoProductFallback))
		return
	}
	cfg := productdto.NewEmptyBundleConfig()
	if v, aerr := strconv.Atoi(strings.TrimSpace(c.PostForm("maxOptions"))); aerr == nil && v > 0 {
		cfg.MaxOptions = v
	}
	cfg.MinTotalQty = atoiOrZero(c.PostForm("minTotalQty"))
	cfg.MaxTotalQty = atoiOrZero(c.PostForm("maxTotalQty"))
	ids := c.PostFormArray("variantId")
	required := c.PostFormArray("required")
	defaults := c.PostFormArray("defaultQty")
	mins := c.PostFormArray("minQty")
	maxes := c.PostFormArray("maxQty")
	// 来源快照是**并行数组**（与上面五个同一种形态）：成员表的每一行都提交这四个字段，
	// 因此索引与 variantId 一一对应。来源只作溯源与展示，服务端仍按白名单归一
	//（未知来源在 service 里被拒，非仓库来源的仓库字段被清空）。
	sourceKinds := c.PostFormArray("sourceKind")
	warehouseIDs := c.PostFormArray("memberWarehouseId")
	warehouseSKUs := c.PostFormArray("memberWarehouseSku")
	externalSKUs := c.PostFormArray("memberExternalSku")
	for i, vid := range ids {
		vid = strings.TrimSpace(vid)
		if vid == "" {
			continue
		}
		cfg.Options = append(cfg.Options, productdto.BundleOption{
			VariantID:  vid,
			Required:   atoiAt(required, i) == 1,
			DefaultQty: atoiAt(defaults, i),
			MinQty:     atoiAt(mins, i),
			MaxQty:     atoiAt(maxes, i),

			SourceKind:   strAt(sourceKinds, i),
			WarehouseID:  strAt(warehouseIDs, i),
			WarehouseSKU: strAt(warehouseSKUs, i),
			ExternalSKU:  strAt(externalSKUs, i),
		})
	}
	if _, err := h.products.SetBundleConfig(ctx, &productdto.SetBundleConfigReq{
		ProductID: productID,
		Config:    cfg,
		// 操作人只从会话取（主数据变更记录要记「谁改的」）。
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		// 失败**原地重渲染**（保留用户刚配的成员清单），不跳提示页：
		// 成员表是这一屏里最贵的内容，跳走等于让它整屏消失（见 internal/templates/CLAUDE.md
		// 「写表单失败时原地留住输入」）。htmx 档换片段、原生档换整页。
		q := url.Values{"product": {productID}}
		if project := strings.TrimSpace(c.PostForm("projectId")); project != "" {
			q.Set("project", project)
		}
		c.Request.URL.RawQuery = q.Encode()
		h.renderBundlePage(c, &cfg, productErrText(c, err), isHXRequest(c))
		return
	}
	// 成功走整页提示（htmx 由 shell.RenderJump 输出 HX-Redirect）——取代原先的 302 / HX-Redirect。
	productBundleJump(c, true, productActionDoneText(c))
}

// bundleQueryParam 取本页的上下文参数（工程 / 商品），按候选名依次认第一个非空值。
//
// 参数名有两套且都合法：本页自己的两个选择器用 project / product；商品详情页的
// 「编辑捆绑构成」入口用 projectId / productId（与商品页其余表单同一套字段名，
// 直接复用商品 id 不会串）。两个都认，入口从哪来都能选中同一个商品 ——
// 少认一个的表现是「点入口进来是空骨架」，而页面上看不出哪里不对。
func bundleQueryParam(c *gin.Context, names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(c.Query(name)); v != "" {
			return v
		}
	}
	return ""
}

// bundleConfigRows 配置表单的行（已配置的项 + 唯一空白候选行）。
//
// 空白行保留原生表单无 JS 单次添加；已存成员仅展示 SKU 与隐藏身份。
// **每一行都提交同一组字段**（variantId / required / defaultQty / minQty / maxQty /
// sourceKind / memberWarehouseId / memberWarehouseSku / memberExternalSku）：
// 并行数组靠 DOM 顺序对齐，少一个字段就会让后面所有行的索引错位。
func bundleConfigRows(tr func(key, fallback string) string, detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp) []gin.H {
	rows := make([]gin.H, 0, productdto.BundleMaxOptionsLimit)
	// 已配置项的可用量来自配置详情（service 已按真源批量取好），
	// 新加的空白行没有可用量可言（还没选 SKU）。
	skuByID := make(map[string]string, len(skus))
	for _, s := range skus {
		if s != nil {
			skuByID[s.VariantID] = s.ProductName + " · " + s.SKUCode
		}
	}
	if detail != nil {
		for _, o := range detail.Options {
			if o == nil {
				continue
			}
			label := skuByID[o.VariantID]
			if label == "" {
				label = strings.TrimSpace(o.ProductName + " · " + o.SKUCode)
				if o.SKUCode == "" {
					label = o.VariantID
				}
			}
			rows = append(rows, bundleRow(o.BundleOption, o.Available,
				bundleSourceLabel(tr, o.BundleOption), label))
		}
	}
	if len(rows) < productdto.BundleMaxOptionsLimit {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, "", ""))
	}
	return rows
}

// bundleRow 构造一行数据；候选 SKU 列表仅供唯一空白行使用。
func bundleRow(o productdto.BundleOption, available int, sourceLabel, skuLabel string) gin.H {
	return gin.H{
		"VariantID":  o.VariantID,
		"SKULabel":   skuLabel,
		"Required":   o.Required,
		"DefaultQty": o.DefaultQty,
		"MinQty":     o.MinQty,
		"MaxQty":     o.MaxQty,
		"Available":  available,
		// 来源快照（仅作溯源与展示，不是身份）：四个字段原样回填进隐藏域，
		// 代表当前这一行的来源；标签已按当前语言算好。
		"SourceKind":   o.SourceKind,
		"WarehouseID":  o.WarehouseID,
		"WarehouseSKU": o.WarehouseSKU,
		"ExternalSKU":  o.ExternalSKU,
		"SourceLabel":  sourceLabel,
	}
}

// bundleSubmittedRows rebuilds rows from the posted arrays, not persisted configuration.
func bundleSubmittedRows(tr func(key, fallback string) string, detail *productdto.BundleConfigResp, skus []*productdto.BundleSKUResp, c *gin.Context) []gin.H {
	labels := make(map[string]string, len(skus))
	for _, s := range skus {
		if s != nil {
			labels[s.VariantID] = s.ProductName + " · " + s.SKUCode
		}
	}
	for _, o := range detail.Options {
		if o != nil && labels[o.VariantID] == "" && o.SKUCode != "" {
			labels[o.VariantID] = strings.TrimSpace(o.ProductName + " · " + o.SKUCode)
		}
	}
	ids := c.PostFormArray("variantId")
	required, defaults := c.PostFormArray("required"), c.PostFormArray("defaultQty")
	mins, maxes := c.PostFormArray("minQty"), c.PostFormArray("maxQty")
	kinds, warehouses := c.PostFormArray("sourceKind"), c.PostFormArray("memberWarehouseId")
	warehouseSKUs, externalSKUs := c.PostFormArray("memberWarehouseSku"), c.PostFormArray("memberExternalSku")
	rows := make([]gin.H, 0, len(ids)+1)
	hasBlank := false
	for i, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			hasBlank = true
		}
		option := productdto.BundleOption{
			VariantID: id, Required: atoiAt(required, i) == 1,
			DefaultQty: atoiAt(defaults, i), MinQty: atoiAt(mins, i), MaxQty: atoiAt(maxes, i),
			SourceKind: strAt(kinds, i), WarehouseID: strAt(warehouses, i),
			WarehouseSKU: strAt(warehouseSKUs, i), ExternalSKU: strAt(externalSKUs, i),
		}
		label := labels[id]
		if label == "" && id != "" {
			label = id
		}
		rows = append(rows, bundleRow(option, 0, bundleSourceLabel(tr, option), label))
	}
	if len(rows) < productdto.BundleMaxOptionsLimit && !hasBlank {
		rows = append(rows, bundleRow(productdto.BundleOption{Required: true, DefaultQty: 1, MinQty: 1}, 0, "", ""))
	}
	return rows
}

func bundleCandidateOptions(skus []*productdto.BundleSKUResp) []gin.H {
	options := make([]gin.H, 0, len(skus))
	for _, s := range skus {
		if s != nil {
			options = append(options, gin.H{"ID": s.VariantID, "Label": s.ProductName + " · " + s.SKUCode})
		}
	}
	return options
}

// —— 成员来源面板（docs/14 §1.2 的三种来源，批次 C）——

// bundleSourceOptions 来源下拉的三项（值即 enums 常量 —— 服务端按它选解析分支）。
func bundleSourceOptions(tr func(key, fallback string) string, selected string) []gin.H {
	items := []struct {
		Value    string
		Fallback string
	}{
		{productenums.BundleSourceProduct, "从商品导入（该商品的启用变体）"},
		{productenums.BundleSourceWarehouse, "从仓库选（按仓挑选仓库 SKU）"},
		{productenums.BundleSourceAttributes, "自选属性值组合（服务端重算笛卡尔积）"},
	}
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		out = append(out, gin.H{
			"Value":    it.Value,
			"Label":    tr(bundleSourceKey(it.Value), it.Fallback),
			"Selected": it.Value == selected,
		})
	}
	return out
}

// bundleSourceKey 来源值 → 词条 key（与模板/词条表同一份对应关系）。
func bundleSourceKey(value string) string {
	switch value {
	case productenums.BundleSourceWarehouse:
		return productenums.ProductBundleSourceWarehouse
	case productenums.BundleSourceAttributes:
		return productenums.ProductBundleSourceAttributes
	default:
		return productenums.ProductBundleSourceProduct
	}
}

// bundleSourceProductOptions 来源商品下拉项（工程内全部商品；自引用由服务端拒绝）。
func bundleSourceProductOptions(tr func(key, fallback string) string,
	products []*productdto.ProductResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(products)+1)
	out = append(out, gin.H{
		"ID": "", "Label": tr(productenums.ProductBundleOptionNone, "— 不选 —"), "Selected": selected == "",
	})
	for _, p := range products {
		if p == nil {
			continue
		}
		out = append(out, gin.H{"ID": p.ID, "Label": p.Name, "Selected": p.ID == selected})
	}
	return out
}

// bundleSourceWarehouseGroups 来源仓下拉 + 各仓的仓库 SKU 勾选清单。
//
// 复用「从仓库选」的既有投影（warehouseSKUOptions）：每个仓一屏候选，
// 选中即代表「这条货在这个仓」—— 服务端解析时仍会回该仓复核（前端只是线索）。
func bundleSourceWarehouseGroups(groups []gin.H, selectedWarehouseID string) []gin.H {
	out := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		id, _ := g["WarehouseID"].(string)
		out = append(out, gin.H{
			"WarehouseID": id,
			"Label":       g["Label"],
			"IsDefault":   g["IsDefault"],
			"Selected":    id != "" && id == selectedWarehouseID,
			"Items":       g["Items"],
		})
	}
	return out
}

// bundleSourceAttributes 来源商品的「参与变体」属性组（自选属性组合来源的勾选清单）。
//
// 只列参与变体的属性组：不参与变体的组构不出组合（与变体生成抽屉同一口径）。
// 未选来源商品 / 读不出来 / 该商品没有参与变体的组，都返回空清单 ——
// 页面据它渲染一句可行动的提示（而不是让面板变成一块无法操作的空白）。
func (h *productPageHandle) bundleSourceAttributes(ctx context.Context, productID string) (attrs []*productdto.AttributeResp) {
	attrs = []*productdto.AttributeResp{}
	id := strings.TrimSpace(productID)
	if id == "" {
		return attrs
	}
	detail, err := h.products.Get(ctx, &productdto.GetReq{ID: id})
	if err != nil || detail == nil {
		return attrs
	}
	return variationAttributes(detail.Attributes)
}

// productBundleNoProductText 捆绑页的参数级提示（未选商品就提交保存），走整页提示页。
//
// key + 中文兜底：写侧取词（英文站点不再显示中文）。
const (
	productBundleNoProductKey      = "admin.product_bundle.noProduct"
	productBundleNoProductFallback = "请先选择商品"
)

// strAt 取并行数组的第 i 个字符串（缺失一律空串）。
//
// 与 atoiAt 同一手法：这里不承担校验 —— 来源字段的白名单与清理在 service
// （未知来源会被明确拒绝，不是在这里静默丢弃）。
func strAt(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[i])
}

// ProductsBundleMembersResolve 解析捆绑成员的候选行（POST，**不落库**）。
//
// 与变体清单的预览端点（/admin/products/variant/preview）同一形态：服务端把候选行与逐条
// 跳过原因算好回 JSON，前端只负责把行**追加**进成员清单；「保存配置」才是落库动作。
//
// 为什么要 JSON 而不是服务端渲染的片段：成员清单是**未保存的前端状态**（与变体清单同理），
// 服务端渲染的片段落不进那个状态里；这里回的行结构与服务端渲染的行结构逐字一致。
//
// 失败回 400 + 可读文案（走 productErrText，不把 enums 裸 key 或 PG 报错铺给前端）。
func (h *productPageHandle) ProductsBundleMembersResolve(c *gin.Context) {
	ctx := c.Request.Context()
	req := &productdto.ResolveBundleMembersReq{
		ProductID:       strings.TrimSpace(c.PostForm("productId")),
		ProjectID:       strings.TrimSpace(c.PostForm("projectId")),
		Source:          strings.TrimSpace(c.PostForm("source")),
		SourceProductID: strings.TrimSpace(c.PostForm("sourceProductId")),
		WarehouseID:     strings.TrimSpace(c.PostForm("sourceWarehouse")),
		WarehouseSKUs:   c.PostFormArray("warehouseSku"),
		// 属性组合的勾选用生成组合抽屉的同一套字段名（attr:<属性组 id>=值 id）：
		// 收拢规则只有一份，两个入口不会一个认前缀、另一个漏读。
		Selections: variantSelectionFromForm(c),
		// 前端清单里已有的成员：服务端据此去重（同一变体只出现一次）。
		ExistingVariantIDs: c.PostFormArray("existingVariantId"),
	}
	res, err := h.products.ResolveBundleMembers(ctx, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": productErrText(c, err)})
		return
	}
	tr := shell.TranslateFor(c)
	// 规格文本要用来源商品的属性组：「组合在商品侧没有对应变体」必须指得出是哪一组。
	attrs := []*productdto.AttributeResp{}
	if req.SourceProductID != "" {
		if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: req.SourceProductID}); derr == nil && detail != nil {
			attrs = detail.Attributes
		}
	}
	members := make([]gin.H, 0, len(res.Members))
	for _, m := range res.Members {
		if m == nil {
			continue
		}
		// 键名一律小驼峰：gin.H 是 map，JSON 键就是这里的字面量 ——
		// 页面 JS 按 variantId / sourceKind / warehouseSku 取值（与其余 JSON 接口同一口径），
		// 写成 PascalCase 会让前端拿到 undefined 却看不出任何错。
		members = append(members, gin.H{
			"variantId":   m.VariantID,
			"skuCode":     m.SKUCode,
			"productId":   m.ProductID,
			"productName": m.ProductName,
			"spec":        specLabel(m.OptionValues, attrs),
			"sourceKind":  m.Source.Kind,
			"sourceLabel": bundleSourceLabel(tr, productdto.BundleOption{
				SourceKind: m.Source.Kind, WarehouseID: m.Source.WarehouseID,
				WarehouseSKU: m.Source.WarehouseSKU, ExternalSKU: m.Source.ExternalSKU,
			}),
			"warehouseId":  m.Source.WarehouseID,
			"warehouseSku": m.Source.WarehouseSKU,
			"externalSku":  m.Source.ExternalSKU,
		})
	}
	skips := make([]gin.H, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		// 跳过的行要能被认出来：「从规格组合来的」用可读规格文本，「从仓库来的」用仓库 SKU 文本。
		label := strings.TrimSpace(s.SKUCode)
		if label == "" {
			label = strings.TrimSpace(s.WarehouseSKU)
		}
		if label == "" {
			label = strings.TrimSpace(s.VariantID)
		}
		if spec := specLabel(s.OptionValues, attrs); spec != "" && spec != "—" {
			if label == "" {
				label = spec
			} else {
				label = label + " · " + spec
			}
		}
		skips = append(skips, gin.H{
			"reason": s.Reason,
			"text":   tr(s.Reason, bundleMemberSkipFallbacks[s.Reason]),
			"label":  label,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "added": len(members), "skipped": len(skips),
		"members": members, "skips": skips,
	})
}

// bundleMemberSkipFallbacks 成员来源解析被跳过时的中文兜底（key 见 productenums.BundleMember*）。
//
// 与 variantSkipFallbacks 同一形态：词条缺失时页面上仍是可读的一句话，
// 而不是裸 key。
var bundleMemberSkipFallbacks = map[string]string{
	productenums.BundleMemberNotOnProduct:        "该属性值组合在商品侧没有对应变体，未加入（请先到该商品上生成这个规格的变体）",
	productenums.BundleMemberSkippedInList:       "该 SKU 已在成员清单里，未重复加入",
	productenums.BundleMemberWarehouseSKUMissing: "该仓库里没有这条仓库 SKU，未加入（请确认仓库选对了，或先在该仓建好这条货）",
	productenums.BundleMemberVariantDisabled:     "该变体已停用，未加入（停用的 SKU 挂进套餐会变成前台选不了又躲不开的必选项）",
	productenums.BundleMemberOptionsExceeded:     "已达该捆绑配置的选项数量上限，未加入（可先调大上限再解析）",
	productenums.ErrBundleSelfReference:          "该 SKU 属于这个捆绑容器自己，未加入（不能自引用）",
}

// atoiAt 取并行数组的第 i 个并转整数（缺失 / 非法一律 0）。
//
// 不用「解析失败即报错」：这里的 0 会进入 service 的配置校验，
// 该拒的（负数、必选数量为 0 等）由那边统一给出可读原因，前端只负责搬运。
func atoiAt(values []string, i int) int {
	if i < 0 || i >= len(values) {
		return 0
	}
	return atoiOrZero(values[i])
}

// atoiOrZero 解析整数，非法一律 0。
func atoiOrZero(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// errModeUnavailable 双轨能力未装配（装配缺陷）：走归口文案，不直出原文。
var errModeUnavailable = errors.New("详情页双轨能力未装配")

// modeWriteTarget 解析双轨写动作的目标（工程 / 商品 / 发布实例）。
//
// 实例定位一律经 GetByEntity（工程作用域内），失败按 ErrNotFound 回带 —— 与
// 详情页其它写动作的失败口径一致（用户看到的是「商品不存在」，而不是内部错误）。
func (h *productPageHandle) modeWriteTarget(c *gin.Context) (
	projectID, productID string, inst *presentationcontract.InstanceResp, err error) {
	if h.modePort == nil || h.templates == nil || h.instances == nil {
		return "", "", nil, errModeUnavailable
	}
	projectID = strings.TrimSpace(c.PostForm("projectId"))
	productID = strings.TrimSpace(c.PostForm("productId"))
	if projectID == "" || productID == "" {
		return projectID, productID, nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, ierr := h.instances.GetByEntity(c.Request.Context(), &presentationcontract.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID, ProjectID: projectID,
	})
	if ierr != nil || inst == nil {
		return projectID, productID, nil, errors.New(presentationenums.ErrNotFound)
	}
	return projectID, productID, inst, nil
}

// modeRedirect 统一的落点：成功 / 失败都渲染整页提示（回商品编辑页）。
//
// 取代原先的 302 + ?err=：那条通道要求读侧再判一次「这条提示是不是本仓给的」
// （productNoticeTexts），而查询参数不是可信边界（见 product_jump.go 的文件头）。
func modeRedirect(c *gin.Context, projectID, productID string, err error) {
	if err != nil {
		productEditJump(c, false, projectID, productID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductsReapplyPreset 重新套用预设：放弃该商品独立文档，回到跟随模板。
func (h *productPageHandle) ProductsReapplyPreset(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	_, err = h.modePort.ReapplyPreset(c.Request.Context(), &presentationcontract.ReapplyPresetReq{
		InstanceID: inst.ID, ProjectID: projectID,
		// 可选：同时换一套模板（换底稿）。留空 = 沿用当前绑定。
		TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	})
	modeRedirect(c, projectID, productID, err)
}

// ProductsRollbackDocument 快照级文档回滚：取历史快照的文档重发。
func (h *productPageHandle) ProductsRollbackDocument(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	snapshotID := strings.TrimSpace(c.PostForm("snapshotId"))
	if snapshotID == "" {
		modeRedirect(c, projectID, productID, errors.New(presentationenums.ErrInvalidParam))
		return
	}
	_, err = h.modePort.RollbackDocument(c.Request.Context(), &presentationcontract.RollbackDocumentReq{
		InstanceID: inst.ID, ProjectID: projectID, SnapshotID: snapshotID,
	})
	modeRedirect(c, projectID, productID, err)
}

// ProductsRollbackArtifact 产物指针回滚（秒级，不重新编译）。
func (h *productPageHandle) ProductsRollbackArtifact(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	targetHash := strings.TrimSpace(c.PostForm("targetHash"))
	if targetHash == "" {
		modeRedirect(c, projectID, productID, errors.New(presentationenums.ErrInvalidParam))
		return
	}
	_, err = h.modePort.RollbackArtifact(c.Request.Context(), &presentationcontract.RollbackArtifactReq{
		InstanceID: inst.ID, ProjectID: projectID, TargetHash: targetHash,
	})
	modeRedirect(c, projectID, productID, err)
}

// productEntityType 商品详情模板的实体类型（与 product 模块注册进实体类型注册表的
// 类型名逐字一致；模板与发布实例都以它为键）。
const productEntityType = "product"

// productDetailTemplatePath 详情页模板页路径（列表行内入口）。
const productDetailTemplatePath = "/admin/products/template"

// productDetailTemplateNoProductPrompt 缺少 product 参数时的引导文案（经整页提示回带）。
//
// 侧栏菜单「内容 › 商品详情模板」的 path 是不带 product 的 productDetailTemplatePath，
// 点进来必然命中缺参分支：原先只做裸 302 回列表，用户看到页面闪回且**毫无提示**，
// 只会以为菜单坏了。引导必须指名**去哪选**（商品列表），否则「回到列表」与「操作失败」
// 在用户眼里是同一件事。
//
// 这条文案由 productDetailTemplateNoProductPromptText 取词，直接渲染进提示页（成品文案）。
const (
	productDetailTemplateNoProductPromptKey      = "admin.product_detail_template.noProductPrompt"
	productDetailTemplateNoProductPromptFallback = "请先从商品列表选择一件商品，再配置它的详情页模板。"
)

// productDetailTemplateNoProductPromptText 上面那条引导的当前语言文本（写侧唯一的取法）。
func productDetailTemplateNoProductPromptText(c *gin.Context) string {
	return shell.TranslateFor(c)(productDetailTemplateNoProductPromptKey, productDetailTemplateNoProductPromptFallback)
}

// ProductPagePorts 后台商品相关页面消费的自动发布能力：翻译工作台要「按依赖标记待重建」，
// 详情页模板页要「读绑定 / 预览 / 发布 / 切换模板」。两个窄接口的并集作为装配参数类型，
// 页面各自只持有自己需要的那一个（字段类型仍是对应窄接口）。
type ProductPagePorts interface {
	ProductTranslationInstancePort
	ProductDetailTemplatePort
}

// ProductDetailTemplatePort 「详情页模板」页所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本页真正用到的四个方法：读绑定、预览、首次发布、切换模板重新发布。
type ProductDetailTemplatePort interface {
	GetByEntity(ctx context.Context, req *presentationcontract.GetByEntityReq) (res *presentationcontract.InstanceResp, err error)
	PreviewInstance(ctx context.Context, req *presentationcontract.PreviewInstanceReq) (res *presentationcontract.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationcontract.CreateInstanceReq) (res *presentationcontract.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationcontract.RebuildReq) (res *presentationcontract.InstanceResp, err error)
	// UpdateURL 改 URL（发布后换路径）：新路径激活 + 旧路径 301 / 取消激活。
	UpdateURL(ctx context.Context, req *presentationcontract.UpdateURLReq) (res *presentationcontract.InstanceResp, err error)
}

// ProductDetailTemplateModePort 双轨能力（迁移 282）在 product 侧的**重导出**：
// 接口由 presentation 的 contract 声明（presentationcontract.DetailTemplateModePort，
// 独立成文件的收窄端口，理由与 published_locator.go 相同：让消费方在类型上够不着
// 创建 / 重建 / 删除）。这里只保留别名，既有调用点（handle 字段、下面的 setter、
// 页面测试）一字不改。
//
// 早先的形态是「本模块自定义同形接口 + 装配期运行期类型断言注入」，理由是当时
// presentation 的 contract 正由另一批工作维护；那批工作已落地（contract 声明了该端口，
// presentation service 侧有编译期断言 var _ …DetailTemplateModePort = (*Service)(nil)），
// 因此运行期断言已删除：「presentation 提不提供这个能力」现在由编译器回答，
// 缺失是构建失败，不再存在静默降级的路径。
//
// 跨模块形状不在这里另造一份（两份逐字段等价的定义经不起「一处改、另一处静默分叉」）。
type ProductDetailTemplateModePort = presentationcontract.DetailTemplateModePort

// SetDetailTemplateModePort 注入双轨能力（装配期调用）。
//
// 实参静态类型 presentationcontract.PresentationService 已嵌入该端口，装配处直接传值、
// 无需类型断言。仍然保留 setter：页面测试按两参数构造商品页 handle，
// 不注入时详情页降级为只有基础面板（绑定 / 预览 / 发布），降级可见。
func (h *productPageHandle) SetDetailTemplateModePort(port ProductDetailTemplateModePort) {
	h.modePort = port
}

// SetDetailTemplateDeps 注入「详情页模板」页所需的两份契约（装配期调用）。
//
// 用 setter 而非构造参数：既有装配（含页面测试）按两参数构造商品页 handle，
// 详情模板是增量能力；未注入时页面明确提示，而不是整体不可用。
func (h *productPageHandle) SetDetailTemplateDeps(templates contenttemplatecontract.ContentTemplateService,
	instances ProductDetailTemplatePort) {
	h.templates = templates
	h.instances = instances
}

// ProductDetailTemplatePage GET /admin/products/template：某商品的详情页模板选择与预览页。
func (h *productPageHandle) ProductDetailTemplatePage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_detail_template", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	productID := strings.TrimSpace(c.Query("product"))
	// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go），
	// 所以本页没有 Err 键 —— 模板里的提示条已整批删除。
	data := gin.H{
		"title": shell.TranslateFor(c)(productenums.ProductDetailTemplateTitle, "商品详情页模板"), "menu": "products",
		"Projects": projects, "SelectedProject": selected,
	}
	if h.templates == nil || h.instances == nil {
		c.HTML(http.StatusOK, "admin/product/product_detail_template.html",
			shell.Prepare(c, withDetailTemplateMissing(data)))
		return
	}
	if productID == "" {
		// 菜单入口（不带 product）走这里：带一句指名去哪选的引导再回列表，
		// 不静默跳转 —— 整页提示（取代原先的 302 + ?err=）。
		productListJump(c, false, productDetailTemplateNoProductPromptText(c))
		return
	}
	product, err := h.products.Get(ctx, &productdto.GetReq{ID: productID})
	if err != nil {
		productListJump(c, false, productErrText(c, err))
		return
	}
	// 模板清单（多套命名模板）与当前默认模板：默认模板 = 该类型当前解析到的那套，
	// 实例未显式绑定模板时商品就发布在它上面。
	rows, err := h.templates.List(ctx, &contenttemplatecontract.ListReq{EntityType: productEntityType})
	if err != nil {
		shell.PageError(c, "product_detail_template", err)
		return
	}
	defaultID := ""
	if tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType); rerr == nil {
		defaultID = tpl.TemplateID
	}
	boundID := ""
	instanceExists := false
	instanceStatus := ""
	instanceURL := ""
	if inst, ierr := h.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID,
	}); ierr == nil && inst != nil {
		instanceExists, boundID = true, inst.TemplateID
		instanceStatus, instanceURL = inst.Status, inst.URLPath
	}
	templates := make([]gin.H, 0, len(rows))
	for _, t := range rows {
		templates = append(templates, gin.H{
			"ID": t.ID, "Name": t.Name, "DraftVersion": t.DraftVersion,
			"UpdatedAt": t.UpdatedAt,
			"IsBound":   t.ID == boundID,
			"IsDefault": t.ID == defaultID,
		})
	}
	if !instanceExists {
		boundID = defaultID
	}
	data["Product"] = gin.H{
		"ID": product.ID, "Name": product.Name, "Slug": product.Slug,
		// 表单预填的发布路径：按选中工程的 URL 规则派生（用户可改）。
		"URLPath": shell.SiteDetailPath(ctx, h.projects, selected, siteurl.KindProduct, product.Slug, product.ID),
	}
	data["Templates"] = templates
	data["TemplateCount"] = len(templates)
	data["BoundTemplateID"] = boundID
	data["DefaultTemplateID"] = defaultID
	data["InstanceExists"] = instanceExists
	data["InstanceStatus"] = instanceStatus
	data["InstanceURL"] = instanceURL
	data["Ready"] = true
	c.HTML(http.StatusOK, "admin/product/product_detail_template.html", shell.Prepare(c, data))
}

// withDetailTemplateMissing 未注入模板契约时的页面数据（装配缺陷的可见提示）。
func withDetailTemplateMissing(data gin.H) gin.H {
	data["Ready"] = false
	return data
}

// productDetailPath 商品详情页的默认 URL（与商品 slug 一致；发布实例按它激活静态产物）。
// ProductDetailTemplateCreate POST /admin/products/template/create：
// 新建一套命名模板（复制指定模板或当前默认模板的文档），初始版本 v1。
//
// 不在本页做模板可视化编辑：模板内容编辑走 /workbench?template=…；本页解决的是
// 「同一个商品类型下有多套命名模板可选、各自版本化」的落地与选择。
func (h *productPageHandle) ProductDetailTemplateCreate(c *gin.Context) {
	if h.templates == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	name := strings.TrimSpace(c.PostForm("name"))
	copyFrom := strings.TrimSpace(c.PostForm("copyFrom"))
	if name == "" {
		productDetailTemplateJump(c, false, projectID, productID,
			shell.TranslateFor(c)(productDetailTemplateNameRequiredKey, productDetailTemplateNameRequiredFallback))
		return
	}
	doc, err := h.templateDocument(c.Request.Context(), projectID, copyFrom)
	if err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateTemplateErrText(c, err))
		return
	}
	if _, err = h.templates.Create(c.Request.Context(), &contenttemplatecontract.CreateReq{
		EntityType: productEntityType, Name: name, DraftDocument: doc, ProjectID: projectID,
	}); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateTemplateErrText(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneText(c))
}

// templateDocument 取「复制来源」的模板文档：指定模板优先，缺省取该类型默认模板。
func (h *productPageHandle) templateDocument(ctx context.Context, projectID, templateID string) (doc json.RawMessage, err error) {
	id := strings.TrimSpace(templateID)
	if id == "" {
		tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType)
		if rerr != nil {
			return nil, rerr
		}
		id = tpl.TemplateID
	}
	res, gerr := h.templates.Get(ctx, &contenttemplatecontract.GetReq{ID: id})
	if gerr != nil {
		return nil, gerr
	}
	return res.DraftDocument, nil
}

// ProductDetailTemplatePublish POST /admin/products/template/publish：
// 首次为该商品建立发布实例并发布（可指定模板），激活静态产物。
func (h *productPageHandle) ProductDetailTemplatePublish(c *gin.Context) {
	if h.instances == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	req := &presentationcontract.CreateInstanceReq{
		ProjectID: projectID, EntityType: productEntityType, EntityID: productID,
		TemplateID: strings.TrimSpace(c.PostForm("templateId")),
		URLPath:    strings.TrimSpace(c.PostForm("urlPath")),
	}
	if req.URLPath == "" {
		product, err := h.products.Get(c.Request.Context(), &productdto.GetReq{ID: productID})
		if err != nil {
			productDetailTemplateJump(c, false, projectID, productID, productErrText(c, err))
			return
		}
		// 路径按站点 URL 规则派生（SiteSettings.urlPatterns，未配置则用 siteurl 的默认模式）：
		// 这里只是把表单预填好，用户填了就用用户的 —— 见 internal/siteurl 的三条口径。
		req.URLPath = shell.SiteDetailPath(c.Request.Context(), h.projects, projectID,
			siteurl.KindProduct, product.Slug, productID)
	}
	if _, err := h.instances.CreateInstance(c.Request.Context(), req); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateFacingError(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductDetailTemplateUpdateURL POST /admin/products/template/url：
// 修改商品详情页的线上路径（改 URL）。
//
// 内容（商品字段、模板绑定、产物内容）完全不动 —— 路径是站点事实而不是内容
// 的一部分：改它的代价是重建产物 + 处置旧链接，不是重新编辑商品。
func (h *productPageHandle) ProductDetailTemplateUpdateURL(c *gin.Context) {
	if h.instances == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	newPath := strings.TrimSpace(c.PostForm("newPath"))
	if newPath == "" {
		productDetailTemplateJump(c, false, projectID, productID,
			shell.TranslateFor(c)(productDetailTemplatePathRequiredKey, productDetailTemplatePathRequiredFallback))
		return
	}
	if _, err := h.instances.UpdateURL(c.Request.Context(), &presentationcontract.UpdateURLReq{
		EntityType:   productEntityType,
		EntityID:     productID,
		NewPath:      newPath,
		WithRedirect: c.PostForm("withRedirect") != "",
	}); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateFacingError(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductDetailTemplateApply POST /admin/products/template/apply：
// 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化（验收 4）。
func (h *productPageHandle) ProductDetailTemplateApply(c *gin.Context) {
	if h.instances == nil {
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if _, err := h.instances.Rebuild(c.Request.Context(), &presentationcontract.RebuildReq{
		EntityID: productID, TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	}); err != nil {
		productDetailTemplateJump(c, false, projectID, productID, detailTemplateFacingError(c, err))
		return
	}
	productDetailTemplateJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductDetailTemplatePreview POST /admin/products/template/preview：
// 发布前预览模板渲染效果 —— 只读渲染，直接把构建结果的 HTML 输出到新标签页；
// 响应头带上实际使用的模板与版本，便于核对「预览的是哪一套」。
func (h *productPageHandle) ProductDetailTemplatePreview(c *gin.Context) {
	if h.instances == nil {
		// 装配缺陷也走「整页提示回来源页」这一条出口（原先是一行 http.StatusBadRequest
		// 的纯文本：脱壳、不翻译、也没有回去的地方）。
		h.detailTemplateDepsMissingRedirect(c)
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationcontract.PreviewInstanceReq{
		ProjectID: c.PostForm("projectId"), EntityType: productEntityType,
		EntityID: c.PostForm("productId"), TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	})
	if err != nil {
		// 「预览失败」对作者是可用信息，原因（模板不存在 / 文档非法 / 渲染出错）只进日志。
		shell.PageErrorBadRequest(c, "product_detail_template", err)
		return
	}
	if res.TemplateName != "" {
		c.Header("X-Preview-Template", fmt.Sprintf("%s@v%d", res.TemplateName, res.TemplateVersion))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// detailTemplateFacingMessages 详情页模板页可展示的失败文案（键是 presentation 的 enums 常量值）。
//
// presentation 返回的错误是 enums key（ErrPathOccupied 这种），直接回显到页面
// 等于让运营看常量名 —— 而改 URL 最常见的失败恰恰是「路径撞车」，必须说人话。
// 发布 / 切换模板 / 改 URL 三个写动作共用同一张表。
var detailTemplateFacingMessages = map[string]string{
	presentationenums.ErrInvalidParam:         "参数不完整，请检查工程、商品与路径。",
	presentationenums.ErrNotFound:             "这个商品还没有详情页实例，先发布一次。",
	presentationenums.ErrNoTemplate:           "该类型没有可用的内容模板，先建一套商品详情模板。",
	presentationenums.ErrBuildFailed:          "构建失败，请检查模板与商品数据后重试。",
	presentationenums.ErrTemplateTypeMismatch: "这套模板不是商品类型的，换一套再试。",
	presentationenums.ErrInvalidPath:          "访问路径不合法：必须以 / 开头，且不含空格、引号与 .. 路径段。",
	presentationenums.ErrSamePath:             "新路径与当前路径相同，没有需要修改的地方。",
	presentationenums.ErrPathOccupied:         "这个路径已被其他页面或详情页占用，换一个再试。",
}

// detailTemplateTemplateMessages 模板契约（contenttemplate）的业务错误 → 可展示文案。
//
// 这一页的 err 来自三个依赖（product / contenttemplate / presentation），三份白名单
// **分开**查：同名 key 在各自语境下含义不同（contenttemplate 的 ErrNotFound 是「模板不存在」，
// product 的是「商品不存在」），合并成一张表必然吃掉一边。
var detailTemplateTemplateMessages = map[string]string{
	contenttemplateenums.ErrInvalidParam:        "参数不完整，请检查工程、模板名与复制来源。",
	contenttemplateenums.ErrNotFound:            "这套模板不存在，可能已被删除。",
	contenttemplateenums.ErrTemplateInUse:       "这套模板仍被自动发布实例引用，不能删除。",
	contenttemplateenums.ErrInvalidType:         "不支持的内容类型（本页只处理商品详情模板）。",
	contenttemplateenums.ErrDataInvalid:         "模板文档格式非法：请回到工作台重新保存后再复制。",
	contenttemplateenums.ErrFieldBindingInvalid: "模板里的字段绑定越界：请回到工作台改用本模板数据源内的字段。",
	contenttemplateenums.ErrProjectRequired:     "站点里有多个工程，请显式选择这套模板所属的工程。",
	contenttemplateenums.ErrProjectNotFound:     "选择的站点工程不存在，请刷新后重试。",
}

// facingLookup 在「裸 key」或「key: 明细」两种形态的白名单里查文案；未命中返回空串。
//
// 只在这两种形态里认：service 会用 fmt.Errorf("%s: %w", enumsKey, err) 把 key 拼进整句话，
// 精确匹配会让这类错误全部落到归口文案 —— 运营看到「系统内部错误」而实际问题只是
// 路径撞车。这与 content 模块 articleFacingText 的 key 前缀判定是同一判据。
//
// 查到的中文是**兜底**、不是产物：enums 常量值本身就是 i18n key（库里已有通用词条，
// 如 ErrPathOccupied → 页面访问路径已被占用），所以这里按请求语言取词，词条缺失 /
// i18n 未初始化时才回落这张表的中文 —— 否则英文站点上这条提示永远是中文。
func facingLookup(tr func(key, fallback string) string, raw string, table map[string]string) string {
	raw = strings.TrimSpace(raw)
	if msg, ok := table[raw]; ok {
		return tr(raw, msg)
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		key := strings.TrimSpace(raw[:idx])
		if msg, ok := table[key]; ok {
			return tr(key, msg)
		}
	}
	return ""
}

// detailTemplateFacingError 把 presentation 的错误翻译成可展示文案。
//
// 未命中白名单时**不再原样返回**：原先的「宁可显示原始错误」正是把构建器 / 文件系统
// 细节铺到页面上的那条路。现在原文只进日志，对外给归口文案。
func detailTemplateFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := facingLookup(shell.TranslateFor(c), err.Error(), detailTemplateFacingMessages); msg != "" {
		return msg
	}
	return productInternalText(c, err)
}

// detailTemplateTemplateErrText 模板契约错误 → 可展示文案（同一骨架，白名单换成模板那份）。
func detailTemplateTemplateErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := facingLookup(shell.TranslateFor(c), err.Error(), detailTemplateTemplateMessages); msg != "" {
		return msg
	}
	return productInternalText(c, err)
}

// errTemplateDepsMissing 详情模板契约未装配时的提示（装配缺陷，页面可见）。
//
// 中文原文**兼作取词兜底**（与 product_err.go 的 key + 兜底形态一致）：词条缺失时页面显示
// 这句话，而不是裸 key（页面上出现 `admin.product_detail_template.depsMissing` 这种串，
// 运营只会以为后台坏了）。
const errTemplateDepsMissing = "详情页模板能力未装配（缺少内容模板或自动发布契约）"

// detailTemplateDepsMissingKey 上面那条提示的 i18n key（迁移 407 登记中英词条）。
const detailTemplateDepsMissingKey = "admin.product_detail_template.depsMissing"

// detailTemplateDepsMissingText 取当前语言的「依赖未装配」提示。
//
// 五个入口共用这一个取法，且必须与读侧候选同源（product_err.go 的 productOwnPageTexts
// 登记了 key / 中文原文 / 译文三种形态）：写侧的产物只要有一种不在候选里，
// 表现就是「写侧发了提示、页面上什么都不显示」—— 不报错、不记日志。
func detailTemplateDepsMissingText(c *gin.Context) string {
	return shell.TranslateFor(c)(detailTemplateDepsMissingKey, errTemplateDepsMissing)
}

// detailTemplateDepsMissingRedirect 依赖未装配时的统一出口。
//
// 为什么不是 `c.String(400, errTemplateDepsMissing)`（本处原先的写法）：浏览器里没有页面，
// 只有一行纯文本，且**没走 i18n**（英文站点上照样是中文）。预览又是 `target="_blank"` 打开的
// 新标签页 —— 用户看到一块白底黑字，既不知道缺什么、也没有回去的地方。
//
// 整页提示（取代原先的 303 + ?err=）回详情页：这是 POST 表单提交失败，把人送回来源页
// 改一处再试（提示页不自动跳转，避免刷新重复提交）。
//
// 回跳目标带 project / product，而不是裸 productDetailTemplatePath：后者落到页面 handler 的
// 缺参分支，会**再**提示「请先从商品列表选择一件商品」—— 把装配缺陷说成用户没选商品。
func (h *productPageHandle) detailTemplateDepsMissingRedirect(c *gin.Context) {
	productDetailTemplateJump(c, false, c.PostForm("projectId"), c.PostForm("productId"), detailTemplateDepsMissingText(c))
}

// 本页的两个参数级提示（key + 中文兜底；写侧取词，提示页直接渲染成品文案）。
const (
	productDetailTemplateNameRequiredKey      = "admin.product_detail_template.nameRequired"
	productDetailTemplateNameRequiredFallback = "模板名不能为空"
	productDetailTemplatePathRequiredKey      = "admin.product_detail_template.pathRequired"
	productDetailTemplatePathRequiredFallback = "请填写新的访问路径。"
)

// detailTemplatePanel 组装详情页模板面板的数据（docs/04-C-instance-override.md §5）。
//
// 三态：未绑定（引导首次发布）/ 已绑定（预览 + 进入自定义）/ 能力未装配（降级提示）。
// 未装配模板能力（templates / instances 未注入）或没给商品 id 时，只回 {"Avail": false}，
// 模板据 isset 给降级提示，页面照常渲染。
func (h *productPageHandle) detailTemplatePanel(ctx context.Context, selected, productID string) gin.H {
	tplPanel := gin.H{"Avail": false}
	if h.templates == nil || h.instances == nil || strings.TrimSpace(productID) == "" {
		return tplPanel
	}
	tplPanel["Avail"] = true
	if inst, ierr := h.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID, ProjectID: selected,
	}); ierr == nil && inst != nil {
		tplPanel["InstanceID"] = inst.ID
		tplPanel["TemplateID"] = inst.TemplateID
		tplPanel["Published"] = inst.Status == "published" || inst.Status == "active"
		tplPanel["URLPath"] = inst.URLPath
		tplPanel["PreviewQS"] = "template=" + inst.TemplateID + "&entityType=product&entityId=" +
			productID + "&projectId=" + selected
		// 双轨（迁移 282）：模式徽标 + 两个模式的入口分流。
		// document：可进入自定义、可重新套用预设、可按历史快照回滚；
		// template：布局的正确修改位置是模板 —— 按钮写成「编辑模板（影响 N 个商品）」，
		// 影响面用真实计数（含该模板下 template 模式的实例数），不写就让用户凭猜。
		isDoc := inst.RenderMode == presentationcontract.RenderModeDocument
		tplPanel["RenderMode"] = presentationcontract.RenderModeTemplate
		if isDoc {
			tplPanel["RenderMode"] = presentationcontract.RenderModeDocument
		}
		tplPanel["IsDocumentMode"] = isDoc
		// 「预设有新版本」：document 模式不会自动跟随模板，只能靠快照记录的
		// 模板版本与模板最新版比对来提示（判定依据由 toResp 给出）。
		if isDoc {
			if tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType); rerr == nil && tpl != nil &&
				inst.SourceTemplateVersionID != "" && tpl.VersionID != inst.SourceTemplateVersionID {
				tplPanel["PresetUpdated"] = true
			}
			if h.modePort != nil {
				if snaps, serr := h.modePort.ListSnapshots(ctx, &presentationcontract.ListSnapshotsReq{
					InstanceID: inst.ID, ProjectID: selected, Limit: 8,
				}); serr == nil {
					tplPanel["Snapshots"] = snaps
				}
			}
		}
		if h.modePort != nil {
			if counts, cerr := h.modePort.CountByTemplate(ctx, &presentationcontract.CountByTemplateReq{
				TemplateID: inst.TemplateID, ProjectID: selected,
			}); cerr == nil && counts != nil {
				tplPanel["AffectedCount"] = counts.TemplateMode
			}
		}
	}
	if rows, terr := h.templates.List(ctx, &contenttemplatecontract.ListReq{EntityType: productEntityType}); terr == nil {
		tplPanel["Templates"] = rows
	}
	return tplPanel
}

// ProductEditPage GET /admin/products/edit：商品基本字段编辑页。
func (h *productPageHandle) ProductEditPage(c *gin.Context) {
	h.renderProductEditPage(c, strings.TrimSpace(c.Query("project")), strings.TrimSpace(c.Query("product")), "", false)
}

func (h *productPageHandle) renderProductEditPage(c *gin.Context, selected, productID, submitErr string, echo bool) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_edit", err)
		return
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	data := gin.H{
		"title":           MsgProductsTitle,
		"menu":            "products",
		"Projects":        projects,
		"SelectedProject": selected,
		"ProductID":       productID,
		"HasProduct":      false,
		"EditBlocked":     false,
		// 变体的两个抽屉（新建 / 生成组合）要归属仓下拉：仓库清单在取数段拿到后填进来
		// （下面 detail 读到之前先给空切片，模板的 len 判断自然跳过）。
		"WarehouseOptions": []gin.H{},
		// 回列表的链接与「取消」都回到用户来的地方（工程上下文保留）。
		"BackURL": productListFilterURL(selected, "", ""),
		// 状态下拉：当前商品读不出来时按「全部状态」那一档渲染（页面仍完整）。
		"Statuses": productStatusOptions(c, ""),
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// 只剩**原地重渲染**这一条路会给 Err（表单校验失败 / 选项读取失败），见下方 submitErr。
	}
	if submitErr != "" {
		data["Err"] = submitErr
	}
	if selected != "" && productID != "" {
		// 取数顺序与列表页一致（分类 / 品牌 / 标签 / 仓库各取一次），
		// 因为行数据复用同一个 productRow 组装 —— 两页各算一遍必然分叉。
		flat, ferr := h.flatCategories(ctx, selected)
		if ferr != nil {
			shell.PageError(c, "product_edit", ferr)
			return
		}
		brands, berr := h.listBrands(ctx, selected)
		if berr != nil {
			shell.PageError(c, "product_edit", berr)
			return
		}
		tags, terr := h.listTags(ctx, selected)
		if terr != nil {
			shell.PageError(c, "product_edit", terr)
			return
		}
		warehouseOptions, werr := h.warehouseOptions(ctx, selected, shell.TranslateFor(c))
		if werr != nil {
			shell.PageError(c, "product_edit", werr)
			return
		}
		// 仓库清单要进模板（变体的两个抽屉用它渲染归属仓下拉）——
		// 只在 productRow 里用掉、忘了放进 data 的话，模板第一行的变量声明就会中断渲染。
		data["WarehouseOptions"] = warehouseOptions
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: selected})
		if derr == nil && detail != nil {
			data["HasProduct"] = true
			row := h.productRow(ctx, selected, flat, brands, tags, warehouseOptions, detail, shell.TranslateFor(c))
			// productRow 的汇总段是给**列表列**用的（名称 / 状态 / 价格区间 / 分类 / 品牌 / 标签），
			// 编辑表单还要几个列表列不需要的字段（副标题 / 单位 / SEO 两栏）——
			// 在这里补，而不是往共享组装里塞：那会让列表页也背上只有编辑页才用的键。
			row["Subtitle"] = detail.Subtitle
			row["Unit"] = detail.Unit
			row["SEOTitle"] = detail.SEOTitle
			row["SEODescription"] = detail.SEODescription
			// 商品描述在库里是 {"html": "..."}（jsonb 对象），富文本字段要的是裸 HTML。
			// 这里破一次「表单字段名 = DTO json 标签」的例：两边形态本来就不同
			//（对象 vs HTML 字符串），硬对齐只会把包装逻辑推到前端散落脚本里。
			row["DescriptionHTML"] = productDescriptionHTML(detail.Description)
			data["Product"] = row
			data["Statuses"] = productStatusOptions(c, detail.Status)
			// 属性组勾选态（详情页用的是逗号分隔的 id 输入框，编辑页是勾选列表）：
			// 可选值来自工程属性组清单，勾选态由商品已引用的 id 决定。
			opts, aerr := h.attributeOptions(ctx, selected)
			if aerr != nil {
				// 选择器缺失与主动取消勾选在 POST 上同形；读故障时撤掉保存入口。
				data["EditBlocked"] = true
				data["Err"] = productInternalText(c, aerr)
			} else {
				data["AttributeChecks"] = checkedAttributeOptions(opts, detail.AttributeIDs)
			}
			// 图集与数值字段：表单里是文本（textarea / input），按行与可空文本回填。
			data["ImagesText"] = strings.Join(detail.Images, "\n")
			data["ImageAltsText"] = strings.Join(detail.ImageAlts, "\n")
			data["WeightText"] = nullableNumberText(detail.Weight)
			data["DefaultPriceText"] = nullableNumberText(detail.DefaultPrice)
			if echo {
				form := formEchoFrom(c)
				for field, key := range map[string]string{
					"name": "Name", "subtitle": "Subtitle", "slug": "Slug", "sku": "SKUCode",
					"unit": "Unit", "seoTitle": "SEOTitle", "seoDescription": "SEODescription",
				} {
					row[key] = form.value(field)
				}
				data["ImagesText"] = c.PostForm("images")
				data["ImageAltsText"] = c.PostForm("imageAlts")
				data["WeightText"] = c.PostForm("weight")
				data["DefaultPriceText"] = c.PostForm("defaultPrice")
				data["Statuses"] = productStatusOptions(c, form.value("status"))
				if checks, ok := data["AttributeChecks"].([]gin.H); ok {
					data["AttributeChecks"] = checkedProductEditOptions(checks, form.list("attributeIds"))
				}
				row["CategoryChecks"] = checkedProductEditOptions(row["CategoryChecks"].([]gin.H), form.list("categoryIds"))
				row["TagChecks"] = checkedProductEditOptions(row["TagChecks"].([]gin.H), form.list("tagIds"))
				row["PrimaryOptions"] = selectedProductEditOptions(row["PrimaryOptions"].([]gin.H), form.value("primaryCategoryId"))
				row["BrandOptions"] = selectedProductEditOptions(row["BrandOptions"].([]gin.H), form.value("brandId"))
			}
			// 捆绑容器：构成表在详情页只读展示，编辑入口在独立页 /admin/products/bundle ——
			// 这里只给一个链接标记（type=variant 时不给这个键，模板据 isset 整块跳过）。
			data["IsBundle"] = isBundleProduct(detail.Type)
			// 详情页模板面板：详情页只读展示绑定状态，写动作（进入自定义 / 编辑模板 /
			// 重新套用预设 / 回滚）都在本页 —— 与详情页共用同一份组装。
			data["TplPanel"] = h.detailTemplatePanel(ctx, selected, productID)
		}
	}
	c.HTML(http.StatusOK, "admin/product/product_edit.html", shell.Prepare(c, data))
}

func checkedProductEditOptions(options []gin.H, selected []string) []gin.H {
	for _, option := range options {
		id, _ := option["ID"].(string)
		option["Checked"] = containsString(selected, id)
	}
	return options
}

func selectedProductEditOptions(options []gin.H, selected string) []gin.H {
	for _, option := range options {
		id, _ := option["ID"].(string)
		option["Selected"] = id == selected
	}
	return options
}

// ProductsUpdate POST /admin/products/update：保存商品基本字段。
//
// 语义与 service 的 Update 一一对应（**整体替换** vs **不改**）：
//   - 文本字段总是提交（空串 = 清空该字段）；
//   - 多选字段（属性组 / 分类 / 标签）总是非 nil —— 一个都没勾是「解绑全部」，
//     不是「本次不改」（浏览器在没有任何勾选时根本不提交该字段，直接透传会把
//     「取消勾选全部」变成静默无操作）；
//   - 数值字段留空 = 不改（nil），不写 0：0 元与「没填」是两回事。
func (h *productPageHandle) ProductsUpdate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	id := formProductID(c)
	if id == "" {
		productListJump(c, false, productErrText(c, errors.New(productenums.ErrInvalidParam)))
		return
	}
	req := &productdto.UpdateReq{ID: id, ProjectID: projectID}

	// 名称必填：模板上有 required，但服务端不信任前端（缺了会静默保留旧名）。
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		productEditJump(c, false, projectID, id, productErrText(c, errors.New(productenums.ErrNameRequired)))
		return
	}
	req.Name = &name

	// URL 段：留空即不改（service 侧空串会被当成非法参数，且改名不该顺手清空路径）。
	if slug := strings.TrimSpace(c.PostForm("slug")); slug != "" {
		req.Slug = &slug
	}
	subtitle := strings.TrimSpace(c.PostForm("subtitle"))
	req.Subtitle = &subtitle
	unit := strings.TrimSpace(c.PostForm("unit"))
	req.Unit = &unit
	// 表单已不再提交 seoTitle / seoDescription（两者合并进商品名与副标题）。
	// **这里不能改成 req.SEOTitle = &""**：那会让「改个商品名顺手清空 SEO 标题」——
	// 两列保留着编辑者写过的历史值，不传即不改才是它们该有的归宿。
	// 商品描述：富文本字段给的是裸 HTML，入库形态是 {"html": "..."}。
	// 包装在这里做（不 trim：正文里的空白是有意义的排版）。
	if descHTML, derr := json.Marshal(map[string]string{"html": c.PostForm("descriptionHtml")}); derr == nil {
		req.Description = descHTML
	}
	if status := strings.TrimSpace(c.PostForm("status")); status != "" {
		req.Status = &status
	}
	// 主体 SKU：留空即不改（存量编码一律不重写；清空编码在 service 侧是明确拒绝的错误）。
	if sku := strings.TrimSpace(c.PostForm("sku")); sku != "" {
		req.SKUCode = &sku
	}
	if raw := strings.TrimSpace(c.PostForm("defaultPrice")); raw != "" {
		v, perr := parseFloat(raw)
		if perr != nil {
			h.productEditValidationFail(c, projectID, id,
				shell.TranslateFor(c)(productenums.ProductEditErrDefaultPriceInvalid, "默认价格格式无效，请输入数字"))
			return
		}
		req.DefaultPrice = &v
	}
	if raw := strings.TrimSpace(c.PostForm("weight")); raw != "" {
		v, perr := parseFloat(raw)
		if perr != nil {
			h.productEditValidationFail(c, projectID, id,
				shell.TranslateFor(c)(productenums.ProductEditErrWeightInvalid, "重量格式无效，请输入数字"))
			return
		}
		req.Weight = &v
	}
	// 图集与 alt：按行切分（textarea 一行一个），空文本 = 清空该字段。
	req.Images = splitFormLines(c.PostForm("images"))
	req.ImageAlts = splitFormLines(c.PostForm("imageAlts"))

	// 多选字段：**总是非 nil**（见方法注释）。
	req.AttributeIDs = postFormArrayAlways(c, "attributeIds")
	req.CategoryIDs = postFormArrayAlways(c, "categoryIds")
	req.TagIDs = postFormArrayAlways(c, "tagIds")
	primary := strings.TrimSpace(c.PostForm("primaryCategoryId"))
	req.PrimaryCategoryID = &primary
	brand := strings.TrimSpace(c.PostForm("brandId"))
	req.BrandID = &brand

	// 空 attributeIds 只有选项读取成功时才表示用户主动取消勾选。
	if _, err := h.attributeOptions(c.Request.Context(), projectID); err != nil {
		h.renderProductEditPage(c, projectID, id, productInternalText(c, err), true)
		return
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, id, productErrText(c, err))
		return
	}
	// 保存成功的回执走提示页（成品文案，走响应体不再进 URL）：
	// 文案由 productSaved 词条给出，页面刷新后能看见「商品已保存」。
	productEditJump(c, true, projectID, id, productBulkTextOf(c, productSaved))
}

func (h *productPageHandle) productEditValidationFail(c *gin.Context, projectID, id, msg string) {
	h.renderProductEditPage(c, projectID, id, msg, true)
}

// postFormArrayAlways 取同名多值字段，**总是**返回非 nil 切片。
//
// 「一个都没勾」与「本次不改这个字段」是两回事：UpdateReq 里 nil = 不改、
// 空数组 = 整体替换为空，而浏览器在没有任何勾选时根本不提交该字段
// （PostFormArray 给 nil）—— 直接透传会让「取消勾选全部」变成静默无操作。
func postFormArrayAlways(c *gin.Context, field string) []string {
	if vals := c.PostFormArray(field); len(vals) > 0 {
		return vals
	}
	return []string{}
}

// splitFormLines 把 textarea 的文本按行切成切片（去首尾空白、丢弃空行、**总是非 nil**）。
//
// 空文本给空切片而不是 nil：nil 在 UpdateReq 里表示「本次不改」，
// 而用户在编辑页清空图集是一个明确意图（清空），不是「什么都没做」。
func splitFormLines(raw string) []string {
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// nullableNumberText 可空数值 → 输入框文本（未设置给空串，不是 0）。
//
// 与列表展示用的 formatNullableAmount（空显示为 —）分开：— 是**展示**符号，
// 填进 input 的 value 会变成一个待提交的非法值。
func nullableNumberText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// checkedAttributeOptions 属性组勾选列表：给每组补一个 Checked（商品是否引用它）。
func checkedAttributeOptions(options []gin.H, selected []string) []gin.H {
	out := make([]gin.H, 0, len(options))
	for _, o := range options {
		item := gin.H{}
		for k, v := range o {
			item[k] = v
		}
		id, _ := o["ID"].(string)
		item["Checked"] = containsString(selected, id)
		out = append(out, item)
	}
	return out
}

// productDescriptionHTML 从商品描述的 JSON 里取正文（富文本字段要裸 HTML）。
//
// 兼容两种存量形态：{"html": "..."} 与裸字符串。取不到就给空串 ——
// 这个值只用于**回填编辑器**，猜错会让编辑者看到别人的正文，比空着危险得多。
func productDescriptionHTML(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return s
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return ""
}

// ProductNewPage GET /admin/products/new：商品新建整页。
func (h *productPageHandle) ProductNewPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "products_new", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 建表单片段需要的数据（与列表页同一 helper 同款口径）：属性组勾选列表 +
	// 「从仓库选」候选。两处取数分叉会让某条入口静默少字段 —— 本会话实测过。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected, shell.TranslateFor(c))
	if werr != nil {
		shell.PageError(c, "products_new", werr)
		return
	}
	attributeOptions, aerr := h.attributeOptions(ctx, selected)
	if aerr != nil {
		shell.PageError(c, "products_new", aerr)
		return
	}
	warehouseSKUGroups, wserr := h.warehouseSKUOptions(ctx, selected, warehouseOptions)
	if wserr != nil {
		shell.PageError(c, "products_new", wserr)
		return
	}
	// 模板清单与默认模板：卡片展示用；未装配模板能力时右侧给降级提示。
	var tplRows []*contenttemplatecontract.TemplateResp
	defaultID := ""
	tplAvail := false
	if h.templates != nil {
		tplAvail = true
		if rows, terr := h.templates.List(ctx, &contenttemplatecontract.ListReq{EntityType: productEntityType}); terr == nil {
			tplRows = rows
		}
		if tpl, rerr := h.templates.ResolveTemplate(ctx, productEntityType); rerr == nil {
			defaultID = tpl.TemplateID
		}
	}
	c.HTML(http.StatusOK, "admin/product/products_new.html", shell.Prepare(c, gin.H{
		"title": shell.TranslateFor(c)(productenums.ProductsCreateSubmit, "新建商品"), "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"WarehouseOptions": warehouseOptions, "AttributeOptions": attributeOptions,
		"WarehouseSKUOptions": warehouseSKUGroups,
		"Templates":           tplRows, "DefaultTemplateID": defaultID, "TemplatesAvail": tplAvail,
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// 只有 htmx 失败回填片段仍带 SubmitErr（原地留住输入，见 product_create_form.html）。
	}))
}

// —— 商品建表单的失败分档出口（渐进增强；片段见 partials/product_create_form.html）——

// productCreateFormFields 建表单片段的回填字段清单（片段与 handler 之间的协议）。
//
// 改片段里的 `name=` 必须同步改这里：helper 按这份清单给每个字段补零值键，漏列的字段
// 在失败片段里读不到 —— 而 Jet 读缺失的 map 键会**从那一行截断整页**（HTTP 仍 200，
// 错误槽之后的表单整块消失），现象是「保存失败后表单没了」，且没有任何报错。
var productCreateFormFields = []string{
	"projectId", "name", "type", "slug", "skuSource", "sku", "warehouseSku",
	"externalSku", "defaultPrice", "quantity", "trackQuantity",
	"attributeIds", "warehouseIds",
}

// productCreateFormData 建表单片段的渲染 data（**只在提交失败重渲染时**用）。
//
// 首屏（新建整页 / 列表页抽屉）不注入 FormEcho* 三键 —— 片段以 cfFill 开关区分
// 「空表单」与「回填表单」，两个页面 handler 因此不必为它加键。
func (h *productPageHandle) productCreateFormData(c *gin.Context, projectID, submitErr string) (gin.H, error) {
	ctx := c.Request.Context()
	projectID = strings.TrimSpace(projectID)
	warehouses, err := h.warehouseOptions(ctx, projectID, shell.TranslateFor(c))
	if err != nil {
		return nil, err
	}
	attributes, err := h.attributeOptions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	skuGroups, err := h.warehouseSKUOptions(ctx, projectID, warehouses)
	if err != nil {
		return nil, err
	}
	data := formEchoData(c, productCreateFormFields...)
	data["SelectedProject"] = projectID
	data["WarehouseOptions"] = warehouses
	data["AttributeOptions"] = attributes
	data["WarehouseSKUOptions"] = skuGroups
	data["SubmitErr"] = submitErr
	return data, nil
}

// productCreateFail 商品建表单的失败出口（分档口径唯一，别在调用点各写一份头判断）。
//
//	· htmx 请求：200 + 片段（错误槽 + 回填后的表单）—— 整页原地留住已填内容；
//	· 原生请求：整页提示（shell.RenderJump）回本页（无 JS 环境：不跳走、错误可读）。
//
// **成功路径也走同一条出口**（ProductsCreate 用 productEditJump / productListJump）。
func (h *productPageHandle) productCreateFail(c *gin.Context, projectID, msg string) {
	if !isHXRequest(c) {
		// 原生档：整页提示（取代原先的 302 + ?err= 回本页）。无 JS 环境下不跳走、错误可读。
		productNewJump(c, false, projectID, msg)
		return
	}
	data, err := h.productCreateFormData(c, projectID, msg)
	if err != nil {
		// 回填片段取数再失败：退回整页提示（错误仍可读），不把半个片段当成功返回。
		logger.Scene("product").With("project", projectID).Error(err, "商品新建失败后重建表单片段失败，退回提示页")
		productNewJump(c, false, projectID, msg)
		return
	}
	c.HTML(http.StatusOK, "admin/product/product_create_form.html", shell.Prepare(c, data))
}

// product_handle.go — 后台商品管理页（issue #5 / T3a）。

//

// 独立于 dashboard 的通用 Handle：只依赖 product 契约与 project 契约，

// 避免把商品依赖掺进 dashboard 的通用装配。

//

// 交互遵循后台规范：GET 渲染完整页，POST 处理完 302 回列表（原生表单 + csrf_token 隐藏域）。

// variantSelectionPrefix 组合生成表单里「属性组 → 勾选值」的字段名前缀。

// 字段名形如 attr:<属性组 id>（多选 checkbox），一个属性组一组值；
// 提交时按前缀收拢成 service 的 selections。
const variantSelectionPrefix = "attr:"

// productPageHandle 商品后台页处理器。
type productPageHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
	// templates / instances 供「详情页模板」页（issue #14）消费：模板列表与版本、
	// 商品发布实例的模板绑定与预览渲染。经 SetDetailTemplateDeps 注入 ——
	// 未注入时该页给出装配提示，不影响商品列表页与既有测试的构造签名。
	templates contenttemplatecontract.ContentTemplateService
	instances ProductDetailTemplatePort
	// modePort 双轨能力（迁移 282）：独立文档 / 重新套用预设 / 回滚 / 影响面计数。
	// 单独字段而不是并入 instances：这六条动作属于另一条收窄端口
	//（presentationcontract.DetailTemplateModePort），装配期直接注入（实参
	// presentationSvc 已嵌入该端口，无需断言）；未注入时降级为只有基础面板。
	modePort ProductDetailTemplateModePort
	// inventories 仓库清单（issue #15）：变体新增表单的「归属仓」下拉，
	// 「不选」即兜底该工程的默认仓。经 SetInventoryDeps 注入 —— 未注入时
	// 表单不带仓库下拉（商品页其余功能一字不变，既有测试构造签名也不受影响）。
	inventories inventorycontract.InventoryService
	// seoPages / seoContents 编辑期 title 唯一性检查的另外两个数据源（审计 SEO-018）：
	// 页面草稿的 SEO 标题与文章标题都算「同站点已存在的内容」，只比商品域会漏掉
	// 跨内容的重复。经 SetSeoTitleSources 注入 —— 可空，未注入时索引退化为商品域。
	seoPages    pagecontract.PageService
	seoContents contentcontract.ContentService
}

// NewProductPageHandle 构造。
func NewProductPageHandle(products productcontract.ProductService, projects projectcontract.ProjectService) *productPageHandle {
	return &productPageHandle{products: products, projects: projects}
}

// SetInventoryDeps 注入仓库清单依赖（issue #15，装配期调用）。
// 未注入时商品页不渲染「归属仓」下拉，变体创建按「不指定仓库」处理。
func (h *productPageHandle) SetInventoryDeps(inventory inventorycontract.InventoryService) {
	h.inventories = inventory
}

// ProductsPage 商品管理页：工程切换 + 商品列表 + 每个商品的变体面板 + 内联新建表单。
func (h *productPageHandle) ProductsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 筛选与分页（服务端渲染，零 JS）：关键词 / 状态 / 页码全部走查询串，
	// 分页条与筛选表单是普通 GET —— 无 JS 也能用，且刷新后条件不丢。
	// 查询串不是可信边界：关键词与状态在这里归一，页码非法值一律退回第 1 页。
	keyword := strings.TrimSpace(c.Query("keyword"))
	status := strings.TrimSpace(c.Query("status"))
	page := productPageNumber(c.Query("page"))
	limit := productListPageSize
	rows := make([]gin.H, 0, limit)
	// 分类树与品牌列表一次取好：每个商品行都要渲染「挂哪些分类 / 主分类 / 品牌」，
	// 放在循环里取会变成 2×N 次查询。
	flat, ferr := h.flatCategories(ctx, selected)
	if ferr != nil {
		shell.PageError(c, "product", ferr)
		return
	}
	brands, berr := h.listBrands(ctx, selected)
	if berr != nil {
		shell.PageError(c, "product", berr)
		return
	}
	// 标签一次取好（issue #11）：每个商品行要渲染「挂哪些手工标签 / 命中了哪些自动标签」，
	// 放在循环里取会变成 N 次查询。
	tags, terr := h.listTags(ctx, selected)
	if terr != nil {
		shell.PageError(c, "product", terr)
		return
	}
	// 归属仓下拉（issue #15）与多仓勾选：变体 / 首建商品的归属仓在这里选（不选 = 默认仓）。
	// **必须在商品行之前取好**：列表「库存」列的分仓明细要按工程仓库清单补齐未入库的仓。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected, shell.TranslateFor(c))
	if werr != nil {
		shell.PageError(c, "product", werr)
		return
	}
	// 分页总数与列表**同一份过滤条件**（service 的 CountProducts 与 List 共用 model 的
	// 关键词 / 状态条件）：两处口径分叉时「共 N 条」与实际能翻出来的条数对不上。
	total := int64(0)
	if selected != "" {
		list, lerr := h.products.List(ctx, &productdto.ListReq{
			ProjectID: selected, Keyword: keyword, Status: status, Page: page, Size: limit,
		})
		if lerr != nil {
			shell.PageError(c, "product", lerr)
			return
		}
		total, lerr = h.products.CountProducts(ctx, &productdto.ListReq{
			ProjectID: selected, Keyword: keyword, Status: status,
		})
		if lerr != nil {
			shell.PageError(c, "product", lerr)
			return
		}
		for _, p := range list {
			// 列表项不含变体明细，逐个取详情（一页 20 条，后台页可接受）。
			detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
			if derr != nil {
				continue
			}
			rows = append(rows, h.productRow(ctx, selected, flat, brands, tags, warehouseOptions, detail,
				shell.TranslateFor(c)))
		}
	}
	// 属性组勾选列表（本批）：新建抽屉引用属性组时不再手打 UUID —— 可选值由服务端给出
	// （与归属仓同一手法），用户看到的是一排「颜色（color）」，而不是一个暗示 UUID 的输入框。
	attributeOptions, aerr := h.attributeOptions(ctx, selected)
	if aerr != nil {
		shell.PageError(c, "product", aerr)
		return
	}
	// 「从仓库选」的候选（docs/14 §1.1 入口 A，迁移 251）：按仓分组的仓库 SKU 清单。
	// 前端只是便捷入口 —— 服务端在 Create 里会带着仓库 id 再复核一遍（不信任前端）。
	warehouseSKUGroups, wserr := h.warehouseSKUOptions(ctx, selected, warehouseOptions)
	if wserr != nil {
		shell.PageError(c, "product", wserr)
		return
	}
	// 批量改价抽屉的规则清单与表单初值：三样全部取自定价工具（ListPricingRuleTypes /
	// ListPricingRoundingOptions / defaultPricingForm）—— 抽屉与独立定价页共用同一个
	// 规则表单片段（partials/pricing_rule_fields.html），键名或取值来源分叉会让其中
	// 一处渲染成空下拉。
	pricingRules := h.products.ListPricingRuleTypes(ctx)
	pricingRoundings := h.products.ListPricingRoundingOptions(ctx)
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言，
	// 与其它后台页面同一渲染入口（缺 token 时表单提交会被 CSRF 中间件挡下）。
	pageData := gin.H{
		"title":            MsgProductsTitle,
		"menu":             "products",
		"Projects":         projects,
		"SelectedProject":  selected,
		"WarehouseOptions": warehouseOptions,
		"AttributeOptions": attributeOptions,
		// 可选键：未接库存契约 / 该工程的仓库里还没有货时是空数组，模板据 isset + len
		// 整块跳过（直接渲染模板的单测不带这个键，缺键会让整页在此中断）。
		"WarehouseSKUOptions": warehouseSKUGroups,
		"Rules":               pricingRules,
		"Roundings":           pricingRoundings,
		"Form":                defaultPricingForm(pricingRules, pricingRoundings),
		"Products":            rows,
		// 筛选回显（GET 表单的 value / selected）：提交后条件留在控件上，
		// 否则用户看不出「现在到底筛了什么」。
		"FilterKeyword": keyword,
		"FilterStatus":  status,
		"Statuses":      productStatusOptions(c, status),
		"Page":          page,
		"Limit":         limit,
		"Total":         total,
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go），
		// 所以模板里的 ?err= / ?done= 提示条已整批删除。
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productListBackKeys...),
	}
	// 分页条（shell 组件，服务端渲染）：基地址带当前筛选条件，翻页不丢条件。
	// 单页或空数据时 BuildPagination 返回 nil，TemplateKeys 给空 map，模板自然不渲染。
	for k, v := range shell.BuildPagination(total, page, limit,
		productListFilterURL(selected, keyword, status), shell.TranslateFor(c)).TemplateKeys() {
		pageData[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/products.html", shell.Prepare(c, pageData))
}

// productRow 组装单个商品的页面视图数据（列表页与详情页共用）。
//
// 键分两段：汇总段（VariantCount / PriceMin / PriceMax / HasRating / RatingAvg /
// RatingCount / CategoryCell / CategoryOthers / BrandName）只回答「有哪些商品」，
// 列表页只用这一段；明细段（Variants / Ratings / CategoryChecks / TagChecks /
// AutoTags / VariationAttributes …）是某个商品的子资源，只有详情页用。
//
// 两页共用同一份组装：同一个值如果在两处各算一遍，分叉时没有任何东西会报错
// （与「同一概念两处口径」是同一类问题）。
func (h *productPageHandle) productRow(ctx context.Context, selected string,
	flat []*productdto.CategoryResp, brands []*productdto.BrandResp, tags []*productdto.TagResp,
	warehouses []gin.H, detail *productdto.ProductResp, tr func(key, fallback string) string) gin.H {
	// 评分读一次：明细 + 投影值（issue #33）。读不到按「没有评分」处理，页面照常渲染。
	rating := ratingOf(h.products, ctx, selected, detail.ID)
	ratingRows := make([]gin.H, 0, len(rating.Items))
	for _, it := range rating.Items {
		ratingRows = append(ratingRows, gin.H{
			"ID": it.ID, "Score": formatScore(it.Score),
			"Source": it.Source, "CreatedAt": it.CreatedAt,
		})
	}
	categoryCell, categoryOthers := categoryCellLabel(flat, detail.CategoryIDs, detail.PrimaryCategoryID)
	// 标签与自动标签各算一次（列表列的取值与详情页的勾选态共用同一份结果）。
	tagChecks := checkedTagOptions(tags, detail.TagIDs)
	autoTags := attachedAutoTags(tags, detail.TagIDs)
	return gin.H{
		"ID": detail.ID, "Name": detail.Name, "Slug": detail.Slug,
		// 主体 SKU（products.sku_code）：详情页基本信息区只读展示 + 一句话说明唯一性范围。
		"SKUCode":  detail.SKUCode,
		"Status":   detail.Status,
		"PriceMin": formatAmount(detail.PriceMin), "PriceMax": formatAmount(detail.PriceMax),
		"VariantCount": detail.VariantCount,
		// 变体**清单**行（docs/14 §8）：种子是库里已有的变体（不是重算笛卡尔积），
		// 行上带可编辑的 SKU 与规范化后的 option_values（保存时原样回传）。
		"Variants": variantListRows(detail),
		// 引用的属性组（issue #7）：同一属性组可被多个商品共用，
		// 这里只展示引用与属性值，编辑入口在 /admin/product-attributes。
		"AttributeIDs":    detail.AttributeIDs,
		"AttributeIDsCSV": strings.Join(detail.AttributeIDs, ","),
		"Attributes":      detail.Attributes,
		// 组合生成面板（issue #8）只列参与变体的组。
		"VariationAttributes": variationAttributes(detail.Attributes),
		// 分类与品牌（issue #10）：勾选态 / 选中态都由服务端算好，
		// 模板只做展示；分类下拉带层级缩进（层级真源在 service 的树组装）。
		// CategoryCell / CategoryOthers 是**列表列**要的「一个值 + 其余数量」：
		// 一列一个概念，单元格放值，不把「挂载 N 个分类 · 主分类 X」拼成一句话。
		"CategoryIDs":         detail.CategoryIDs,
		"CategoryChecks":      checkedCategoryOptions(flat, detail.CategoryIDs),
		"CategoryCell":        categoryCell,
		"CategoryOthers":      categoryOthers,
		"PrimaryOptions":      primaryCategoryOptions(tr, flat, detail.PrimaryCategoryID),
		"BrandOptions":        brandPickOptions(tr, brands, detail.BrandID),
		"PrimaryCategoryName": categoryNameByID(flat, detail.PrimaryCategoryID),
		"BrandName":           brandNameByID(brands, detail.BrandID),
		// 标签（issue #11）：手工标签勾选挂载（勾选态服务端算好）；自动标签只读展示 ——
		// 归属由规则重算维护，手工改会被下一次重算覆盖，故不提供勾选框。
		"TagIDs":    detail.TagIDs,
		"TagChecks": tagChecks,
		"AutoTags":  autoTags,
		// TagLabel 是**列表列**要的单个值：这个商品挂了哪些标签（手工 + 自动合并的标签名）。
		"TagLabel": tagCellLabel(tr, tagChecks, autoTags),
		// 评分（issue #30 / #33）：明细 + 投影值。评分是独立表，这里读的是
		// ListRatings 算出的平均值与条数；**没有评分时 HasRating=false** ——
		// 空态与「评分 0」是两回事，模板据它给出不同文案。
		"Ratings":     ratingRows,
		"HasRating":   rating.HasRating,
		"RatingAvg":   formatScore(rating.Rating),
		"RatingCount": rating.RatingCount,
		// 库存列（docs/14 §1.4）：三态 + 分仓明细。真源是 inventory_stocks，
		// 这里只是把服务端算好的聚合结果整理成页面要的形状（服务端返回的是**有行**的仓，
		// 未入库的仓由工程仓库清单补齐）。
		"Stock": productStockCell(detail, warehouses),
	}
}

// categoryCellLabel 商品行「分类」列要显示的单个值（一列一个概念：单元格是值，不是句子）。
//
// 取值优先级：主分类 > 挂载分类里的第一个 —— 挂了分类却没设主分类时显示空值是误导
// （用户会以为这个商品没有分类）。都没有才返回空串，由模板渲染成 —。
// others 是「其余分类数」，模板用 +N 徽章跟在值后面，不把三个信号拼成一句话。
func categoryCellLabel(flat []*productdto.CategoryResp, attached []string, primaryID string) (label string, others int) {
	if name := categoryNameByID(flat, primaryID); name != "" {
		return name, maxInt(len(attached)-1, 0)
	}
	for _, node := range flat {
		if node == nil || !containsString(attached, node.ID) {
			continue
		}
		return node.Name, maxInt(len(attached)-1, 0)
	}
	return "", 0
}

// tagCellLabel 商品行「标签」列要显示的单个值（一列回答一个问题：这个商品挂什么标签）。
//
// 列的是**标签名**（手工挂载的 + 命中规则的自动标签，合并成一个名字列表），
// 不做「手工 N · 自动 M」那种来源分解 —— 读这一列的人要知道「挂了哪些标签」，
// 而不是「这些标签里有几个是手工建的」（那是标签管理页的问题）；两个数都是 0 时，
// 那种分解更是纯噪声。名字多于 maxTagNames 时退化成数量，避免单元格被撑成一屏。
func tagCellLabel(tr func(key, fallback string) string, tagChecks, autoTags []gin.H) string {
	const maxTagNames = 3
	names := make([]string, 0, len(tagChecks)+len(autoTags))
	for _, t := range tagChecks {
		checked, _ := t["Checked"].(bool)
		if !checked {
			continue
		}
		if name, ok := t["Name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	for _, t := range autoTags {
		if name, ok := t["Name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) > maxTagNames:
		// 数量后缀是文案（中文「N 个」/ 英文「N tags」），不是数据：走词条而不是拼中文。
		return i18n.FillTranslate(tr, productenums.ProductsListTagCount, "{n} 个",
			map[string]string{"n": strconv.Itoa(len(names))})
	default:
		return strings.Join(names, "、")
	}
}

// maxInt 取两者较大值（其余分类数不能为负：主分类不在挂载列表里时 len-1 会是 -1）。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ProductDetailPage GET /admin/products/detail：单个商品的详情页（**只读**）。
//
// 为什么是独立页，而不是列表页里的第二、三张表（admin-ui-logic §1）：列表页只该回答
// 「有哪些商品」；变体与评分是**某个商品的子资源**，属于该商品的详情。原来三张表平铺在
// 列表页上，等于让列表页承载实体详情 —— 商品一多，变体表与评分表就是两份与商品表错位的
// 长表，改一个商品要跨三处找入口。
//
// 本页只**看**：基本信息 / 属性引用 / 分类与品牌 / 标签 / 捆绑构成 / 变体清单 / 评分 /
// 详情页模板的绑定状态。全部写动作（含变体与评分）在 ProductEditPage ——
// 读与写混在同一页时用户分不清「我在看还是在改」，而且每个写表单都要带 CSRF、
// 错误回显与回跳地址，那是编辑页的职责。
//
// 商品不存在（含没给 product 参数）渲染 .empty-state + 返回列表链接，不 500：
// 手输 URL、书签失效、商品刚被删都会走到这里，500 什么也说明不了。
func (h *productPageHandle) ProductDetailPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	// 仓库清单：只读页不用下拉，但行数据组装（productRow 的库存列）要按工程仓库清单
	// 补齐未入库的仓 —— 与编辑页 / 列表页共用同一份组装，取数口径不能分叉。
	warehouseOptions, werr := h.warehouseOptions(ctx, selected, shell.TranslateFor(c))
	if werr != nil {
		shell.PageError(c, "product", werr)
		return
	}
	productID := strings.TrimSpace(c.Query("product"))
	hasProduct := false
	product := gin.H{}
	if productID != "" {
		// 只取目标商品那一条：详情页与列表页不同，不需要为整页商品各读一次评分 / 分类。
		if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID}); derr == nil && detail != nil {
			flat, ferr := h.flatCategories(ctx, selected)
			if ferr != nil {
				shell.PageError(c, "product", ferr)
				return
			}
			brands, berr := h.listBrands(ctx, selected)
			if berr != nil {
				shell.PageError(c, "product", berr)
				return
			}
			tags, terr := h.listTags(ctx, selected)
			if terr != nil {
				shell.PageError(c, "product", terr)
				return
			}
			hasProduct = true
			product = h.productRow(ctx, selected, flat, brands, tags, warehouseOptions, detail, shell.TranslateFor(c))
			// 捆绑容器（type=bundle）在详情页多一块「捆绑构成」编辑区：成员只作为选项与
			// 履约明细，容器价才是套餐价。type=variant 时**不给这个键**，模板据 isset 整块跳过
			// —— 区块的入口就在详情页，不该再要求运营去另一个菜单页找它。
			if isBundleProduct(detail.Type) {
				product["Bundle"] = h.bundlePanel(c, selected, detail.ID)
			}
		}
	}
	data := gin.H{
		"title": shell.TranslateFor(c)(productenums.ProductDetailTitle, "商品详情"), "menu": "products",
		"Projects": projects, "SelectedProject": selected,
		"ProductID": productID, "HasProduct": hasProduct, "Product": product,
		"WarehouseOptions": warehouseOptions,
		// 返回列表带上工程上下文：回到列表时不会掉回默认工程。
		"BackURL": "/admin/products?project=" + selected,
		// 只读页没有写操作，也没有任何提示位：写动作的结论走 shell.RenderJump 渲染提示页
		//（见 product_jump.go），本页的 ?err= 读侧与提示条已整批删除。
	}
	// 详情页模板面板：详情页只读展示绑定状态与预览入口，编辑动作（进入自定义 / 编辑模板 /
	// 重新套用预设 / 回滚）在编辑页 —— 两页共用 detailTemplatePanel 这一份组装。
	data["TplPanel"] = h.detailTemplatePanel(ctx, selected, productID)
	c.HTML(http.StatusOK, "admin/product/product_detail.html", shell.Prepare(c, data))
}

// variantSelectionFromForm 收「生成组合」表单里按前缀提交的勾选（attr:<属性组 id> → 值 id）。
//
// 生成（落库路径）、预览（不落库）两条 handler 共用它：字段名与收拢规则只有一份，
// 两处各写一遍必然分叉（一个认 attr: 前缀、另一个漏读，表现为「预览有行、保存却没数据」）。
func variantSelectionFromForm(c *gin.Context) []productdto.VariantSelectionReq {
	_ = c.Request.ParseForm()
	keys := make([]string, 0, len(c.Request.PostForm))
	for key := range c.Request.PostForm {
		if strings.HasPrefix(key, variantSelectionPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]productdto.VariantSelectionReq, 0, len(keys))
	for _, key := range keys {
		attrID := strings.TrimPrefix(key, variantSelectionPrefix)
		if attrID == "" {
			continue
		}
		out = append(out, productdto.VariantSelectionReq{
			AttributeID: attrID, ValueIDs: c.Request.PostForm[key],
		})
	}
	return out
}

// ProductsVariantGenerate 按勾选的属性值批量生成变体组合（issue #8）。
//
// mode=all：不勾选任何值，按商品全部参与变体的属性组 × 全部启用值生成
// （service 的无表单路径，导入 / 接口走同一条）。
// 其它情况按勾选生成；一个都没勾选时直接退回并提示 —— 不静默退化成「全部生成」，
// 那是最容易一次误造出上百个变体的路径。
func (h *productPageHandle) ProductsVariantGenerate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	req := &productdto.GenerateVariantsReq{
		ProductID:   productID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	if strings.TrimSpace(c.PostForm("mode")) != "all" {
		req.Selections = variantSelectionFromForm(c)
		if len(req.Selections) == 0 {
			// 裸 enums key 铺到页面上只会显示 ErrVariationSelectionEmpty —— 走取词助手拿中文。
			productEditJump(c, false, projectID, productID,
				productErrText(c, errors.New(productenums.ErrVariationSelectionEmpty)))
			return
		}
	}
	if _, err := h.products.GenerateVariants(c.Request.Context(), req); err != nil {
		// 非业务错误的原文（PG / 构建器）只进日志，对外给归口文案。
		productEditJump(c, false, projectID, productID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneText(c))
}

// variantPreviewRow 预览行的 JSON 形状：前端拿它渲染清单里的一行。
//
// Spec 是**服务端**拼的可读规格文本（与详情页表格同一份 specLabel），
// 前端不自己拼属性名 —— 属性值改名后前端那份就会显示旧名字。
type variantPreviewRow struct {
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`
	OptionKey    string          `json:"optionKey"`
	Spec         string          `json:"spec"`
}

// ProductsVariantPreview 组合生成的**预览**（POST /admin/products/variant/preview，不落库）。
//
// 用户口径（docs/14 §8）：「生产只是显示，并不会存入数据库，必须保存才行」。
// 因此本端点只回答「点这次生成会往前端清单里追加哪些行」：
//   - 笛卡尔积、维度与数量上限、与既有组合的去重全在 service（与落库路径同一套规则）；
//   - SKU 由服务端按变体 SKU 规则生成（前端不复刻这套算法），运营可在清单里逐行改写；
//   - 响应是 JSON 而不是 HTMX 片段：前端要把这些行**追加**进已渲染的清单 DOM，
//     而清单的初始行由服务端渲染（库里已有变体），两者是同一种行结构。
//
// 失败回 400 + 可读文案（走 productErrText 取词，不把 enums 裸 key 或 PG 报错铺给前端）。
func (h *productPageHandle) ProductsVariantPreview(c *gin.Context) {
	ctx := c.Request.Context()
	productID := strings.TrimSpace(c.PostForm("productId"))
	req := &productdto.PreviewVariantReq{
		ProductID:   productID,
		ProjectID:   strings.TrimSpace(c.PostForm("projectId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		Selections:  variantSelectionFromForm(c),
	}
	// 清单里已有的组合原样回传（每行一条）：由服务端归一去重，前端不需要懂 optionKey。
	for _, raw := range c.PostFormArray("optionValues") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			req.ExistingOptionValues = append(req.ExistingOptionValues, json.RawMessage(trimmed))
		}
	}
	res, err := h.products.PreviewVariantCombinations(ctx, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": productErrText(c, err)})
		return
	}
	attrs := []*productdto.AttributeResp{}
	if detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: productID}); derr == nil && detail != nil {
		attrs = detail.Attributes
	}
	rows := make([]variantPreviewRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		if row == nil {
			continue
		}
		rows = append(rows, variantPreviewRow{
			SKUCode: row.SKUCode, OptionValues: row.OptionValues,
			OptionKey: row.OptionKey, Spec: specLabel(row.OptionValues, attrs),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "total": res.Total, "skipped": res.Skipped, "rows": rows,
	})
}

// ProductsVariantSave 以清单为准保存变体（POST /admin/products/variant/save）——**唯一的落库动作**。
//
// 表单里的 rows 是一段 JSON（前端清单的全部行，顺序即期望顺序）：行上只有
// variantId（既有变体；新增行为空）、可编辑的 skuCode 与 optionValues。
// 服务端逐条重算（见 service.SaveVariantList），前端提交的形状一律不作数。
//
// 结果由提示页渲染（shell.RenderJump）回编辑页：成功含「新增 / 改 SKU / 删除」与逐条跳过原因，
// 失败给白名单文案 —— 与编辑页其它写动作同一套出口，不新增渲染通道。
func (h *productPageHandle) ProductsVariantSave(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	productID := strings.TrimSpace(c.PostForm("productId"))
	req := &productdto.SaveVariantListReq{
		ProductID:   productID,
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		// 操作人取自会话，表单字段不作数（变体留痕的操作人不可伪造）。
		OperatorID: shell.CurrentUserIDText(c),
	}
	if raw := strings.TrimSpace(c.PostForm("rows")); raw != "" {
		if uerr := json.Unmarshal([]byte(raw), &req.Rows); uerr != nil {
			// 前端拼错了 JSON：这属于调用方参数错误，给可读提示而不是 PG / JSON 原始报错。
			productEditJump(c, false, projectID, productID, productErrText(c, errors.New(productenums.ErrInvalidParam)))
			return
		}
	}
	// 这里不再预取商品详情（只为拼「哪个 SKU 被跳过」）：回带文案只报原因枚举，
	// 逐行 SKU 由详情页本身呈现（见 variantSaveNotice 的说明），因此少一次 DB 往返。
	res, err := h.products.SaveVariantList(ctx, req)
	if err != nil {
		productEditJump(c, false, projectID, productID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, variantSaveNotice(c, res))
}

// variantSkipFallbacks 清单外删除被跳过时的中文兜底（key 见 productenums.VariantSkip*）。
var variantSkipFallbacks = map[string]string{
	productenums.VariantSkipHasStock:       "仍有库存，未删除（请先处理库存或改为停用）",
	productenums.VariantSkipReferenced:     "被 BOM 清单引用，未删除（请先解除引用）",
	productenums.VariantSkipDuplicated:     "与清单里前面的行重复，已忽略",
	productenums.VariantSkipVariantMissing: "这一行对应的变体已不存在（可能已被别处删除）",
	// 本批新增的两个引用面（迁移 259）：被捆绑成员引用 / 有过库存流水。
	productenums.VariantSkipBundleReferenced: "被捆绑成员引用，未删除（请先在捆绑配置里解除引用）",
	productenums.VariantSkipHasMovement:      "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

// variantSaveNotice 保存结果的回带文案：计数 + 逐条跳过原因。
//
// 「一条都没变」与「跳过若干」必须能区分：都写成「保存成功」时，用户无法判断是
// 清单本来就与库里一致，还是有行被拦下了（被拦下的行下一次还会出现在清单里）。
func variantSaveNotice(c *gin.Context, res *productdto.SaveVariantListResp) string {
	tr := shell.TranslateFor(c)
	if res == nil {
		return tr(shell.MsgInternalError, productErrInternalFallback)
	}
	if res.Created == 0 && res.Updated == 0 && res.Deleted == 0 && len(res.Skipped) == 0 {
		return productBulkTextOf(c, productVariantSaveNoChange)
	}
	// 模板取当前语言（与读侧 productNoticeTexts / productVariantNoticeMatches 同一个取法），
	// 数字一律 strconv.Itoa 成字符串后填入（词条只允许 %s）。
	parts := []string{fmt.Sprintf(productBulkTextOf(c, productVariantSaveSaved),
		strconv.Itoa(res.Created), strconv.Itoa(res.Updated), strconv.Itoa(res.Deleted))}
	if len(res.Skipped) > 0 {
		// 只报**原因**，不再把「哪个 SKU / 什么规格」拼进结论：
		// 受控形态下（每条原因都来自 variantSkipFallbacks 的枚举）文案可读且不泄漏。
		// 被跳过的行本来就留在清单里，逐行 SKU 由页面本身呈现。
		items := make([]string, 0, len(res.Skipped))
		for _, skip := range res.Skipped {
			items = append(items, tr(skip.Reason, variantSkipFallbacks[skip.Reason]))
		}
		parts = append(parts, fmt.Sprintf(productBulkTextOf(c, productVariantSaveSkipped),
			strconv.Itoa(len(res.Skipped)), strings.Join(items, "；")))
	}
	return strings.Join(parts, " ")
}

// ProductsCreate 新建商品。
//
// 两条本批确定的语义：
//
//  1. **type 必须读**（迁移 238 的商品类型）：字段一旦漏读，service 会把空串兜底成
//     variant —— 建「捆绑容器」得到的是一个带自己首个变体的常规商品，而且返回 302 成功、
//     列表页也看得见，不留任何错误线索。
//  2. **成功去商品详情页**，不是回列表：抽屉只有名称 / 类型 / URL 段 / 默认价 / 归属仓 /
//     属性组几个字段，而 CreateReq 有 40+ 个字段（描述、图片、分类、品牌、标签、SEO…），
//     商品详情页才是全量录入的地方。bundle 更明显 —— 它没有变体，回列表看不出任何进展。
//     失败仍回新建页（表单所在处）并渲染整页提示：用户就在那里重填。
func (h *productPageHandle) ProductsCreate(c *gin.Context) {
	req := &productdto.CreateReq{
		ProjectID: c.PostForm("projectId"),
		Name:      c.PostForm("name"),
		// 商品类型：variant（常规变体商品）｜bundle（捆绑容器，只有容器价）。
		Type: strings.TrimSpace(c.PostForm("type")),
		Slug: c.PostForm("slug"),
		// 主体 SKU 编码（表单 name=sku，规则见 docs/14）：留空即派生（变体商品取 URL 段、
		// 捆绑取 <商品段>_B）；填了且选了仓库 → 变体商品自动加仓库码前缀。
		// **必须透传**：漏读等于运营填了个寂寞 —— 服务端会静默按 URL 段派生另一个编码，
		// 建完看不出任何异常，直到与仓库对不上号。
		SKUCode: strings.TrimSpace(c.PostForm("sku")),
		// SKU 来源（docs/14 §1.1 的两条入口，迁移 251）：自己创建 / 从仓库选。
		// **必须透传**：漏读就等于运营在抽屉里选了「从仓库选」却拿到一个按 URL 段
		// 派生的商品，建完看不出任何异常 —— 与上面 sku 漏读是同一类静默失败。
		SKUSource:    strings.TrimSpace(c.PostForm("skuSource")),
		WarehouseSKU: strings.TrimSpace(c.PostForm("warehouseSku")),
		ExternalSKU:  strings.TrimSpace(c.PostForm("externalSku")),
		// 属性组：抽屉是勾选列表（同名多值），保留了逗号分隔单值的兼容形态。
		AttributeIDs: attributeIDsFromForm(c),
		// 归属仓：空值即兜底该工程的默认仓（单值形态保留，接口 / 脚本路径仍可用）。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		// 多仓勾选（2026-09-19 口径）：勾了哪些仓就在哪些仓各建一行（裸码），
		// 第一个仓是**认领仓**（决定主体 SKU 的仓码前缀）。同名多值 → PostFormArray，
		// 一个都没勾时浏览器不提交这个字段（服务端按「默认仓」处理）。
		WarehouseIDs: c.PostFormArray("warehouseIds"),
	}
	if price := strings.TrimSpace(c.PostForm("defaultPrice")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.DefaultPrice = &v
		}
	}
	// 数量（迁移 261 的口径）：**不填数量 = 不跟踪 = 无限**。模板是一个「跟踪数量」开关
	// 加一个数量框：开关没勾时数量框禁用、不提交 ⇒ 这里读不到值 ⇒ Quantity 保持 nil。
	// 开关勾了而数量框留空 ⇒ 写 0（0 是「明确没货」，与「没填」是两回事）。
	if strings.TrimSpace(c.PostForm("trackQuantity")) != "" {
		qty := 0
		if raw := strings.TrimSpace(c.PostForm("quantity")); raw != "" {
			v, qerr := strconv.Atoi(raw)
			if qerr != nil || v < 0 {
				// 数量非法也是**表单失败**：htmx 档留在原页（错误槽 + 回填），不能悄悄跳走 ——
				// 用户为这一张表填了十几行内容，为一个数字问题全丢是最贵的一种失败。
				h.productCreateFail(c, req.ProjectID,
					shell.TranslateFor(c)(productQuantityInvalidKey, productQuantityInvalidFallback))
				return
			}
			qty = v
		}
		req.Quantity = &qty
	}
	created, err := h.products.Create(c.Request.Context(), req)
	if err != nil {
		// 业务错误的 Error() 是 enums 常量（= i18n key），直接铺到页面上就是
		// 「列表页显示 ErrBundlePriceRequired」的来源：统一经 productErrText 取词。
		// 出口走分档：htmx 档 200 + 回填片段，原生档渲染整页提示（见 productCreateFail）。
		h.productCreateFail(c, req.ProjectID, productErrText(c, err))
		return
	}
	if created != nil && created.ID != "" {
		// 成功也走同一条出口：htmx 由 shell.RenderJump 输出 HX-Redirect（整页跳转），
		// 原生渲染提示页 —— 取代原先的 redirectWhere。
		productEditJump(c, true, req.ProjectID, created.ID, productActionDoneText(c))
		return
	}
	// 兜底：拿到商品 id 才谈得上「进详情继续编辑」，否则回列表而不是构造一个空详情页。
	productListJump(c, true, productActionDoneText(c))
}

// ProductsVariantCreate 为商品新增变体（未填字段继承商品级默认值）。
func (h *productPageHandle) ProductsVariantCreate(c *gin.Context) {
	req := &productdto.CreateVariantReq{
		ProductID: c.PostForm("productId"),
		SKUCode:   strings.TrimSpace(c.PostForm("skuCode")),
		// 归属仓（issue #15）：空值即兜底默认仓；无论选没选都会在归属仓生成库存记录。
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
	}
	if price := strings.TrimSpace(c.PostForm("price")); price != "" {
		if v, perr := parseFloat(price); perr == nil {
			req.Price = &v
		}
	}
	projectID := c.PostForm("projectId")
	if _, err := h.products.CreateVariant(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ProductID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ProductID, productActionDoneText(c))
}

// ProductsVariantDelete 删除变体。
//
// 表单里的 id 是**变体 id**，回详情的商品 id 只能取 productId 隐藏域（不给 id 回落的机会）。
func (h *productPageHandle) ProductsVariantDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if err := h.products.DeleteVariant(c.Request.Context(), &productdto.DeleteVariantReq{ID: c.PostForm("id")}); err != nil {
		productEditJump(c, false, projectID, productID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductsRatingAdd 补录一条商品评分（issue #33）。
//
// 评分是**独立明细表**（#30），所以这是「加一条记录」而不是「改商品字段」——
// 分值范围由 service 与数据库 CHECK 双重兜底，这里只把非数字提前拦下并给出可读文案。
func (h *productPageHandle) ProductsRatingAdd(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	score, perr := strconv.ParseFloat(strings.TrimSpace(c.PostForm("score")), 64)
	if perr != nil {
		productEditJump(c, false, projectID, productID, "评分必须是 0~5 的数字")
		return
	}
	if _, err := h.products.AddRating(c.Request.Context(), &productdto.AddRatingReq{
		ProductID:  productID,
		Score:      score,
		OperatorID: builtin.GetUsername(c),
	}); err != nil {
		productEditJump(c, false, projectID, productID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductsRatingDelete 删掉一条评分（issue #33）：录错了能撤掉。
//
// 表单里的 id 是**评分 id**，回详情的商品 id 只能取 productId 隐藏域（不给 id 回落的机会）。
func (h *productPageHandle) ProductsRatingDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	productID := c.PostForm("productId")
	if err := h.products.DeleteRating(c.Request.Context(), &productdto.DeleteRatingReq{
		ID: c.PostForm("id"),
		// 工程显式回传（DB-009）：product_ratings 有 FORCE 策略，删一条评分要在工程作用域里。
		ProjectID: projectID,
	}); err != nil {
		productEditJump(c, false, projectID, productID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, productID, productActionDoneText(c))
}

// ProductsAttributesSet 整体替换某商品引用的属性组（issue #7）。
//
// 引用的组必须是同一工程内真实存在的组（service 校验）；提交空数组即解绑全部。
//
// 表单的隐藏域是 id（值就是商品 id），formProductID 优先认 productId。
func (h *productPageHandle) ProductsAttributesSet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.UpdateReq{
		ID:           formProductID(c),
		AttributeIDs: splitIDs(c.PostForm("attributeIds")),
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ID, productActionDoneText(c))
}

// ProductsDelete 删除商品（连带变体）。
//
// **例外：只有这个端点成功后仍回列表页** —— 商品已经不存在了，回详情页只会看到一个
// 「商品不存在」的空态，用户还得再点一次返回列表。
func (h *productPageHandle) ProductsDelete(c *gin.Context) {
	if err := h.products.Delete(c.Request.Context(), &productdto.DeleteReq{ID: c.PostForm("id")}); err != nil {
		productListJump(c, false, productErrText(c, err))
		return
	}
	productListJump(c, true, productBulkDoneText(c, productBulkDone))
}

// —— 商品列表页的批量「按规则改价」与错误文案 ——

// bulkPricingNothingSelected 没勾选就提交的意见（与批量删除同一口径：说清怎么继续）。
//
// 与其它批量回执同一个形状（key + 中文原文）：写侧 productBulkTextOf 取当前语言，
// 提示页直接渲染成品文案（不再经 URL）。
var bulkPricingNothingSelected = productBulkText{productenums.BulkPricingNoneSelected, "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。"}

// redirectWhere 已提升到 product_page_util.go（同包共享，本文件仍可直接调用）。
// 提升理由：它是「HTMX 写表单分档」的出口之一，写表单的失败片段再走同一个口径，
// 两处各写一份头判断迟早会分叉。

// productErrInternalFallback 非业务错误的兜底文案（兼作取词兜底）。
const productErrInternalFallback = "系统内部错误，请稍后重试"

// productErrSentinels 会回带到后台页面的商品域业务错误（= 词条 key 的来源）。
//
// 商品域的业务错误是 enums 常量（string）经 errors.New 造出来的，服务端还会用
// fmt.Errorf("%s：补充说明", key) 把 key 拼进整句话 —— 两种形态都在 productErrKey 里认。
var productErrSentinels = []string{
	productenums.ErrInvalidParam,
	productenums.ErrNotFound,
	productenums.ErrSlugTaken,
	productenums.ErrSkuTaken,
	productenums.ErrNameRequired,
	productenums.ErrAttrNotFound,
	productenums.ErrAttrProjectMismatch,
	productenums.ErrProductTypeInvalid,
	productenums.ErrProductTypeImmutable,
	productenums.ErrBundlePriceRequired,
	// 主体 SKU 编码（迁移 248 / 249 配套）：三条都是**可行动**的提示（显式填一个编码 /
	// 把 URL 段改成 ASCII / 给捆绑商品填一个主体编码）。漏登记就会被当成非业务错误，
	// 用户只看到「系统内部错误」，运营拿不到任何下一步线索。
	productenums.ErrSkuContainerMissing,
	productenums.ErrContainerSkuInvalid,
	productenums.ErrBundleSKURequired,
	// 主体 SKU 在工程内被别的商品占用（迁移 246 的偏唯一索引）：预检与唯一索引兜底
	// 都映射到这一条。漏登记就会退回「系统内部错误」，运营拿不到「换一个编码」这个下一步。
	productenums.ErrContainerSKUTaken,
	// 变体清单的「保存」（迁移 253 配套）：两条都是**可行动**的提示（清单某行的 SKU 为空 /
	// 某行的规格组合不合法）。漏登记会被当成非业务错误，运营只看到「系统内部错误」，
	// 而清单里有问题的那一行根本指不出来。
	productenums.ErrVariantSKUEmpty,
	productenums.ErrVariantOptionsInvalid,
	// 变体删除守卫的四个引用面（迁移 259 配套）：三条新原因（捆绑成员引用 / 有过库存流水）
	// 与之共用的自引用判定都是**可行动**的提示（先去解绑捆绑、或改为停用）。
	// 单条删除路径经提示页回带，漏登记就只剩「系统内部错误」。
	productenums.VariantSkipBundleReferenced,
	productenums.VariantSkipHasMovement,
	productenums.ErrVariantHasStock,
	// 捆绑成员来源解析（迁移 259 配套）：两个请求级错误 + 自引用 ——
	// 解析端点是 JSON 接口，错误文案经 productErrText 取词后回给前端。
	productenums.ErrBundleSourceInvalid,
	productenums.ErrBundleSourceProductRequired,
	productenums.ErrBundleSourceWarehouseRequired,
	productenums.ErrBundleSelfReference,
	// 仓库 SKU 与外部编码（迁移 251 / 词条 252）：新建抽屉的「从仓库选」入口住在商品列表页，
	// 它抛出的错误必须在**这一页**变成可读中文，否则用户只看到「系统内部错误」。
	inventoryenums.ErrSKUSourceInvalid,
	inventoryenums.ErrWarehouseSKURequired,
	inventoryenums.ErrWarehouseSKUNotFound,
	inventoryenums.ErrWarehouseSKUBundleNotAllowed,
	inventoryenums.ErrWarehouseSKUCodeTaken,
	inventoryenums.ErrExternalSKUInvalid,
	inventoryenums.ErrExternalSKUProductConflict,
	// 归属仓解析（既有 key / 词条 242）：「从仓库选」要先解析出仓库，仓库不存在 / 已停用 /
	// 不属于本工程都是运营能自己修的（换一个仓），不该被当成内部错误。
	inventoryenums.ErrWarehouseNotFound,
	inventoryenums.ErrWarehouseDisabled,
	inventoryenums.ErrWarehouseProjectMismatch,
	inventoryenums.ErrWarehouseDefaultMissing,
	// 定价（批量改价抽屉逐商品回带的原因都出自这里）
	productenums.ErrPricingRuleTypeInvalid,
	productenums.ErrPricingRuleParamsInvalid,
	productenums.ErrPricingRoundingInvalid,
	productenums.ErrPricingScopeInvalid,
	productenums.ErrPricingTargetRequired,
	productenums.ErrPricingTargetNotFound,
	productenums.ErrPricingFilterEmpty,
	productenums.ErrPricingNothingChanged,
	productenums.ErrPricingTargetTooMany,
	productenums.ErrPricingAdjustmentNotFound,
	// —— 后台页全部写操作改走 productErrText 后的**完整性补齐**（第三波 CQ-009）——
	//
	// 上面那批是「按需登记」（某个入口先暴露了才补一条）。一旦所有页面写操作的
	// err.Error() 都改走 productErrText，白名单就从「补充」变成了**唯一出口**：
	// 漏登记一条 = 运营看到「系统内部错误」而原来（裸 key）至少还能对上号。
	// 因此这里把本模块 enums 里全部业务错误一次补齐（属性 / 分类 / 品牌 / 标签 /
	// 捆绑配置与选择 / 变体组合），与 internal/module/product/enums/product_enums.go 对齐。
	productenums.ErrAttrInUse,
	productenums.ErrAttrKeyRequired,
	productenums.ErrAttrKeyTaken,
	productenums.ErrAttrNameRequired,
	productenums.ErrBrandInUse,
	productenums.ErrBrandNameRequired,
	productenums.ErrBrandNotFound,
	productenums.ErrBrandProjectMismatch,
	productenums.ErrBrandSlugTaken,
	productenums.ErrBundleMaxOptionsInvalid,
	productenums.ErrBundleNotConfigured,
	productenums.ErrBundleOptionRequired,
	productenums.ErrBundleOptionsExceeded,
	productenums.ErrBundleQtyAboveMax,
	productenums.ErrBundleQtyAboveStock,
	productenums.ErrBundleQtyBelowMin,
	productenums.ErrBundleQtyInvalid,
	productenums.ErrBundleQtyRangeInvalid,
	productenums.ErrBundleShapeInvalid,
	productenums.ErrBundleStockUnavailable,
	productenums.ErrBundleTotalAboveMax,
	productenums.ErrBundleTotalBelowMin,
	productenums.ErrBundleTotalRangeInvalid,
	productenums.ErrBundleTotalUnreachable,
	productenums.ErrBundleVariantDuplicated,
	productenums.ErrBundleVariantNotFound,
	productenums.ErrBundleVariantNotInConfig,
	productenums.ErrBundleVariantProjectMismatch,
	productenums.ErrBundleVariantRequired,
	productenums.ErrCategoryCycle,
	productenums.ErrCategoryHasChildren,
	productenums.ErrCategoryInUse,
	productenums.ErrCategoryNameRequired,
	productenums.ErrCategoryNotFound,
	productenums.ErrCategoryParentMismatch,
	productenums.ErrCategoryProjectMismatch,
	productenums.ErrCategorySlugTaken,
	productenums.ErrCollectionFilterInvalid,
	// 双轨写动作（迁移 282）：四条都是 presentation 侧的业务错误，会经提示页回带编辑页。
	// 漏登记就只剩「系统内部错误」—— 用户看不到「回滚目标不在位」「快照不属于该商品」
	// 这些**可行动**的区别（该换一个版本 / 该重新发布一次）。
	presentationenums.ErrDetachConfirmRequired,
	presentationenums.ErrRollbackTargetMiss,
	presentationenums.ErrRollbackFailed,
	presentationenums.ErrSnapshotMismatch,
	productenums.ErrCollectionSourceInvalid,
	productenums.ErrInvalidField,
	productenums.ErrInvalidType,
	// 标签的跨工程引用（审计 DB-03 §5.1 第 2 条 / PROD-02）：错误 tail 里带引用面、
	// 涉及的工程与商品 id，漏登记就会让这条**可行动**的提示退化成「系统内部错误」。
	productenums.ErrTagCrossProject,
	productenums.ErrTagKindInvalid,
	productenums.ErrTagNameRequired,
	productenums.ErrTagNotFound,
	productenums.ErrTagNotManual,
	productenums.ErrTagProjectMismatch,
	// 相关商品引用（审计 DB-03 / PROD-01）：非法引用（不存在 / 跨工程 / 指向自己）在
	// tail 里带上 id 与数量，漏登记就会让这条**可行动**的提示退化成「系统内部错误」。
	productenums.ErrRelatedInvalid,
	productenums.ErrTagRuleNotAllowed,
	productenums.ErrTagRuleParamsInvalid,
	productenums.ErrTagRuleTypeInvalid,
	productenums.ErrTagSlugTaken,
	productenums.ErrVariationAttributeInvalid,
	productenums.ErrVariationCountLimit,
	productenums.ErrVariationDimensionLimit,
	productenums.ErrVariationNoDimension,
	productenums.ErrVariationValueInvalid,
	// ErrVariationSelectionEmpty 只在 handler 侧产生（「一个属性值都没勾选就别生成」），
	// service 里看不到它 —— 按「service 用到的 key」做完整性对账时会漏掉这一条，
	// 漏了它页面上就从「裸 key」直接掉成「系统内部错误」。
	productenums.ErrVariationSelectionEmpty,
	productenums.VariantSkipDuplicated,
	productenums.VariantSkipHasStock,
	productenums.VariantSkipReferenced,
	productenums.VariantSkipVariantMissing,
}

// productErrFallbacks 上述业务错误的中文兜底（i18n 未初始化或该 key 没有词条时用）。
//
// 已 seed 的 key（如 ErrBundlePriceRequired 见迁移 239）在真实请求里取 sys_i18n 的文案，
// 这里的兜底与 seed 保持一致，避免「同一句话在测试与线上不一样」。
var productErrFallbacks = map[string]string{
	productenums.ErrInvalidParam:                   "请求参数无效",
	productenums.ErrNotFound:                       "商品或变体不存在",
	productenums.ErrSlugTaken:                      "该 URL 段在本工程已被占用",
	productenums.ErrSkuTaken:                       "该 SKU 编码在本商品下已被占用",
	productenums.ErrNameRequired:                   "商品名称必填",
	productenums.ErrAttrNotFound:                   "属性组不存在",
	productenums.ErrAttrProjectMismatch:            "属性组不属于该商品所在工程",
	productenums.ErrProductTypeInvalid:             "商品类型不合法（仅支持变体商品或捆绑商品）",
	productenums.ErrProductTypeImmutable:           "商品类型在创建后不可更改",
	productenums.ErrBundlePriceRequired:            "捆绑商品必须自定价：容器价需大于 0",
	productenums.ErrSkuContainerMissing:            "SKU 编码无法生成：商品 URL 段不含 ASCII 字符，请显式填写 SKU 编码",
	productenums.ErrContainerSkuInvalid:            "主体 SKU 不合法：必须是非空字符串",
	productenums.ErrBundleSKURequired:              "捆绑商品必须填写主体 SKU 编码（新建抽屉已给出建议值，可直接修改）",
	productenums.ErrContainerSKUTaken:              "该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）",
	productenums.ErrVariantSKUEmpty:                "变体的 SKU 编码不能为空：请填写一个编码（或改回系统生成的编码）",
	productenums.ErrVariantOptionsInvalid:          "变体的规格组合不合法：属性组或属性值不属于该商品",
	productenums.ErrVariantHasStock:                "变体仍有库存，不能删除（请改为停用）",
	productenums.VariantSkipBundleReferenced:       "该变体被捆绑成员引用，不能删除（请先在捆绑配置里解除引用）",
	productenums.VariantSkipHasMovement:            "该变体有过库存流水（已被订单或库存变动用过），不能删除（请改为停用）",
	productenums.ErrBundleSourceInvalid:            "成员来源不合法：只支持「从商品导入」「从仓库选」「自选属性值组合」",
	productenums.ErrBundleSourceProductRequired:    "该来源必须先选一个来源商品",
	productenums.ErrBundleSourceWarehouseRequired:  "从仓库选时必须指定仓库并至少勾选一条仓库 SKU",
	productenums.ErrBundleSelfReference:            "捆绑成员不能引用捆绑主体自己（自引用）",
	inventoryenums.ErrSKUSourceInvalid:             "SKU 来源取值不合法：只支持「自己创建」与「从仓库选」",
	inventoryenums.ErrWarehouseSKURequired:         "从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU",
	inventoryenums.ErrWarehouseSKUNotFound:         "该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录",
	inventoryenums.ErrWarehouseSKUBundleNotAllowed: "捆绑商品不存在于仓库，不能用「从仓库选」建主体 SKU，请改用自己的编码",
	inventoryenums.ErrWarehouseSKUCodeTaken:        "该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU",
	inventoryenums.ErrExternalSKUInvalid:           "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	inventoryenums.ErrExternalSKUProductConflict:   "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	inventoryenums.ErrWarehouseNotFound:            "仓库不存在",
	inventoryenums.ErrWarehouseDisabled:            "已停用的仓库不能作为归属仓",
	inventoryenums.ErrWarehouseProjectMismatch:     "仓库不属于该工程",
	inventoryenums.ErrWarehouseDefaultMissing:      "该工程没有默认仓可兜底，请先建一个仓库并设为默认仓",
	presentationenums.ErrDetachConfirmRequired:     "该操作会放弃模板同步（商品页转为独立文档），需要先确认",
	presentationenums.ErrRollbackTargetMiss:        "回滚目标不存在：该版本不属于这个商品，或产物文件已缺失",
	presentationenums.ErrRollbackFailed:            "回滚失败：线上版本保持不变",
	presentationenums.ErrSnapshotMismatch:          "该版本不属于这个商品，不能用来回滚",
	productenums.ErrPricingRuleTypeInvalid:         "定价规则类型不是内置类型",
	productenums.ErrPricingRuleParamsInvalid:       "定价规则参数不合法（参数键 / 类型 / 取值范围）",
	productenums.ErrPricingRoundingInvalid:         "尾数处理不是内置选项",
	productenums.ErrPricingScopeInvalid:            "定价作用范围不合法",
	productenums.ErrPricingTargetRequired:          "该作用范围必须指定目标（变体 id 或商品 id）",
	productenums.ErrPricingTargetNotFound:          "商品或变体不存在、没有可改价的变体（捆绑容器只有容器价，不能按规则改价）",
	productenums.ErrPricingFilterEmpty:             "筛选集至少要给一个筛选条件",
	productenums.ErrPricingNothingChanged:          "按该规则算下来没有任何价格变化",
	productenums.ErrPricingTargetTooMany:           "命中的商品数超过单批上限，请收紧筛选条件",
	productenums.ErrPricingAdjustmentNotFound:      "调价批次不存在",
	// 同上：完整性补齐后的中文兜底（i18n 未初始化或该 key 没有词条时用）。
	productenums.ErrAttrInUse:                    "属性组已被商品引用，不能删除",
	productenums.ErrAttrKeyRequired:              "属性组标识不能改为空",
	productenums.ErrAttrKeyTaken:                 "该属性组标识在本工程已被占用",
	productenums.ErrAttrNameRequired:             "属性组名称必填",
	productenums.ErrBrandInUse:                   "品牌已被商品引用，不能删除",
	productenums.ErrBrandNameRequired:            "品牌名称必填",
	productenums.ErrBrandNotFound:                "品牌不存在",
	productenums.ErrBrandProjectMismatch:         "品牌不属于该商品所在工程",
	productenums.ErrBrandSlugTaken:               "该品牌 URL 段在本工程已被占用",
	productenums.ErrBundleMaxOptionsInvalid:      "捆绑选项数量上限不合法（允许范围 1~20）",
	productenums.ErrBundleNotConfigured:          "该商品没有配置捆绑选项（不是捆绑商品）",
	productenums.ErrBundleOptionRequired:         "漏填了捆绑的必选项",
	productenums.ErrBundleOptionsExceeded:        "捆绑选项数超过上限",
	productenums.ErrBundleQtyAboveMax:            "某一项的数量超过该项最大数量",
	productenums.ErrBundleQtyAboveStock:          "某一项的数量超过该仓可用库存",
	productenums.ErrBundleQtyBelowMin:            "某一项的数量低于该项最小数量",
	productenums.ErrBundleQtyInvalid:             "数量不合法（负数 / 非整数 / 超过硬上限）",
	productenums.ErrBundleQtyRangeInvalid:        "某一项的数量区间自相矛盾（最小 > 默认、最大 < 最小等）",
	productenums.ErrBundleShapeInvalid:           "捆绑配置形状非法（既不是空值，也不是 JSON 对象）",
	productenums.ErrBundleStockUnavailable:       "读不到库存可用量（库存能力未装配），捆绑校验拒绝通过",
	productenums.ErrBundleTotalAboveMax:          "整单总件数超过最大总件数",
	productenums.ErrBundleTotalBelowMin:          "整单总件数低于最小总件数",
	productenums.ErrBundleTotalRangeInvalid:      "整单件数区间自相矛盾（最大 < 最小，或低于必选项最小量之和）",
	productenums.ErrBundleTotalUnreachable:       "整单下限高于所有选项能加到的上限（永远无法满足）",
	productenums.ErrBundleVariantDuplicated:      "同一个 SKU 在配置里或同一次选择里出现多次",
	productenums.ErrBundleVariantNotFound:        "选项引用的 SKU 不存在（可能已被删除）",
	productenums.ErrBundleVariantNotInConfig:     "选择里出现了配置之外的 SKU",
	productenums.ErrBundleVariantProjectMismatch: "选项引用的 SKU 不属于该商品所在工程",
	productenums.ErrBundleVariantRequired:        "捆绑选项缺少 SKU",
	productenums.ErrCategoryCycle:                "不能把分类挂到自身或自己的后代下",
	productenums.ErrCategoryHasChildren:          "分类仍有子级，不能删除",
	productenums.ErrCategoryInUse:                "分类已被商品引用，不能删除",
	productenums.ErrCategoryNameRequired:         "分类名称必填",
	productenums.ErrCategoryNotFound:             "分类不存在",
	productenums.ErrCategoryParentMismatch:       "父分类不属于该分类所在工程",
	productenums.ErrCategoryProjectMismatch:      "分类不属于该商品所在工程",
	productenums.ErrCategorySlugTaken:            "该分类 URL 段在本工程已被占用",
	productenums.ErrCollectionFilterInvalid:      "过滤维度不在集合源白名单内",
	productenums.ErrCollectionSourceInvalid:      "集合源标识不合法（不是本模块实现的源）",
	productenums.ErrInvalidField:                 "提交了不支持的字段（不在商品字段白名单内）",
	productenums.ErrInvalidType:                  "实体类型不合法",
	productenums.ErrRelatedInvalid:               "相关商品引用不合法：引用的商品必须存在、属于本工程，且不能指向自己",
	productenums.ErrTagCrossProject:              "标签仍被其它工程的商品引用，不能删除",
	productenums.ErrTagKindInvalid:               "标签类型不是 manual / rule",
	productenums.ErrTagNameRequired:              "标签名称必填",
	productenums.ErrTagNotFound:                  "标签不存在",
	productenums.ErrTagNotManual:                 "自动标签的归属由重算维护，不能手工挂载",
	productenums.ErrTagProjectMismatch:           "标签不属于该商品所在工程",
	productenums.ErrTagRuleNotAllowed:            "手工标签不能带自动规则",
	productenums.ErrTagRuleParamsInvalid:         "规则参数不合法（键 / 类型 / 取值范围）",
	productenums.ErrTagRuleTypeInvalid:           "规则类型不是内置类型（不接受自由表达式）",
	productenums.ErrTagSlugTaken:                 "该标签 URL 段在本工程已被占用",
	productenums.ErrVariationAttributeInvalid:    "勾选的属性组未参与该商品的变体",
	productenums.ErrVariationCountLimit:          "变体组合数超过上限",
	productenums.ErrVariationDimensionLimit:      "参与变体的属性维度超过上限",
	productenums.ErrVariationNoDimension:         "没有可参与变体的属性值",
	productenums.ErrVariationValueInvalid:        "勾选的属性值不属于该属性组或已停用",
	productenums.ErrVariationSelectionEmpty:      "未勾选任何属性值：请至少勾选一个属性值，或改用「生成全部组合」",
	productenums.VariantSkipDuplicated:           "清单里重复的规格组合（只保留第一行）",
	productenums.VariantSkipHasStock:             "该变体仍有非零库存",
	productenums.VariantSkipReferenced:           "该变体被 BOM 清单引用",
	productenums.VariantSkipVariantMissing:       "清单引用的既有变体已不存在（可能已被并发删除）",
}

// productErrKey 业务错误 → 词条 key（非业务错误返回空串）。
//
// 不能用 errors.Is：商品域的业务错误是字符串常量，且服务端会用
// fmt.Errorf("%s：补充说明", key) 把 key 拼进整句话。两种形态都按「整串等于 key」
// 或「以 key：开头」识别，取 key 部分去查词条，补充说明原样保留。
func productErrKey(msg string) (key, tail string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", ""
	}
	for _, sentinel := range productErrSentinels {
		if msg == sentinel {
			return sentinel, ""
		}
		if strings.HasPrefix(msg, sentinel+"：") {
			return sentinel, strings.TrimSpace(strings.TrimPrefix(msg, sentinel+"："))
		}
	}
	return "", ""
}

// productErrScene 日志场景名（与 product 模块其它 logger.Scene("product") 一致）。
const productErrScene = "product"

// productErrControlledMessages 受控提示的前缀白名单。
//
// 它们**不是** enums key（因此进不了 productErrSentinels），但整句都由本仓库自己拼出：
// 不含表名 / SQLSTATE / 文件路径，且带着运营照着做的数字。目前只有一条 ——
// shell.BulkIDs 的上限拒绝「一次最多操作 N 项，当前 M 项，请分批进行」（internal/shell/bulk.go）。
//
// 为什么按**前缀**判而不是「调用点知道来源就直接透出」：来源受控这件事会随上游改变。
// 前缀命中不了（BulkIDs 将来改成上抛别的错误）时这里自动退回「记日志 + 归口文案」，
// 不会把不认识的原文顺出去。
var productErrControlledMessages = []string{
	fmt.Sprintf("一次最多操作 %d 项", shell.MaxBulkIDs),
}

// productErrControlled 受控提示 → 原样透出（保留可行动信息）；未命中返回空串。
func productErrControlled(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, prefix := range productErrControlledMessages {
		if strings.HasPrefix(raw, prefix) {
			return raw
		}
	}
	return ""
}

// productInternalText 未命中任何白名单时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外只给归口文案。
//
// 页面上出现 "pq: duplicate key value violates unique constraint" 既看不懂，
// 也把库表结构泄了出去 —— 响应（含提示页文案与模板数据）不是可信边界。
func productInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(productErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "商品后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return shell.TranslateFor(c)(shell.MsgInternalError, productErrInternalFallback)
}

// productErrText 业务错误 → 当前语言文案（非业务错误只给通用提示并留日志）。
//
// 与 block 模块的 blockErrText 同一形状：页面上的错误必须是能读的一句话，
// 而不是 ErrBundlePriceRequired 这样的裸 key，也不是 GORM / PG 的原始报错。
func productErrText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	// 受控提示（shell.BulkIDs 的上限拒绝）优先：它本身就是可读中文，查词条只会把它吞掉。
	if msg := productErrControlled(raw); msg != "" {
		return msg
	}
	key, tail := productErrKey(raw)
	if key == "" {
		return productInternalText(c, err)
	}
	text := shell.TranslateFor(c)(key, productErrFallbacks[key])
	if tail != "" {
		if detail := productErrDetailText(shell.TranslateFor(c), tail); detail != "" {
			text += "：" + detail
		}
	}
	return text
}

// productErrDetailText 业务错误的**补充说明** → 当前语言文案。
//
// 补充说明有两种形态：
//
//  1. i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，可多段）——
//     service 用它把「为什么被拒」的上下文（哪个属性组 / 超了多少 / 被哪个商品占用）
//     也词条化，于是整句按当前语言取词并填 {name} 占位符；
//  2. 其余（受控提示、历史形态的纯文本）—— 原样透出，行为与本通道引入前一致。
//
// 未登记的明细 key（拼错 / 新加漏登记）跳过该段并记一条日志：少一句补充说明，
// 好过把编码串或半截占位符摆到页面上。
func productErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := productenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(productErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
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

// attributeOptions 新建抽屉的属性组勾选列表（本批：不再让人手打 UUID）。
//
// 与归属仓下拉同一手法：可选值由服务端给出，模板只做展示。名字后带上标识 key ——
// 商品详情页与属性页都按 key 指代属性组，只给名字会在同名属性组之间选错。
func (h *productPageHandle) attributeOptions(ctx context.Context, projectID string) (out []gin.H, err error) {
	out = []gin.H{}
	if strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	list, lerr := h.products.ListAttributes(ctx, &productdto.ListAttributeReq{ProjectID: projectID, Size: 200})
	if lerr != nil {
		return nil, lerr
	}
	for _, a := range list {
		if a == nil {
			continue
		}
		label := a.Name
		if key := strings.TrimSpace(a.Key); key != "" {
			label = a.Name + "（" + key + "）"
		}
		out = append(out, gin.H{"ID": a.ID, "Label": label, "IsVariation": a.IsVariation})
	}
	return out, nil
}

// ProductsBulkPricing 批量「按规则改价」（POST /admin/products/bulk-pricing）。
//
// 入口为什么搬到商品列表：定价工具的作用对象本来就是「筛选出的商品集 / 标签 / SKU」，
// 而运营是在列表上圈出要改的那几个的；独立页 /admin/product-pricing 保留可用
// （它能对单个 SKU / 筛选集改价，也是唯一能试算的地方），本端点补上「按勾选批量应用」。
//
// 语义与其它批量操作一致（见 internal/module/CLAUDE.md §inbound/http）：
//
//	· id 一律经 shell.BulkIDs（去空白 / 去重 / 上限），超限**整批拒绝**并说明原因；
//	· 逐条走**单商品的应用路径**（scope=product），一条失败不中断整批；
//	· 结论按「改价 N 个变体 / 跳过 M 个商品」回带列表页，不做静默的部分成功。
//
// 规则解析与校验完全复用定价工具那一份（pricingRuleReqFromForm → readPricingForm /
// pricingParamsFromForm）：复制一份就会出现「抽屉填的 amount 被当成 multiplier」这类
// 只在一个入口发生的错。
func (h *productPageHandle) ProductsBulkPricing(c *gin.Context) {
	ctx := c.Request.Context()
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		productListJump(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	if len(ids) == 0 {
		productListJump(c, false, productBulkTextOf(c, bulkPricingNothingSelected))
		return
	}
	note := strings.TrimSpace(c.PostForm("note"))
	// 操作人取自会话，表单字段不作数（调价留痕的操作人不可伪造）。
	operator := shell.CurrentUserIDText(c)
	changedVariants, unchangedProducts, skipped := 0, 0, 0
	skipReason := ""
	for _, id := range ids {
		rule := pricingRuleReqFromForm(c, productenums.PricingScopeProduct, id)
		res, aerr := h.products.ApplyPricing(ctx, &productdto.PricingApplyReq{
			PricingRuleReq: *rule, Note: note, OperatorID: operator,
		})
		switch {
		case aerr == nil:
			if res != nil {
				changedVariants += res.ChangedCount
			}
		case strings.TrimSpace(aerr.Error()) == productenums.ErrPricingNothingChanged:
			// 这个商品本来就在目标价上：不是失败（service 不写空台账），单独计数。
			unchangedProducts++
		default:
			skipped++
			if skipReason == "" {
				skipReason = productErrText(c, aerr)
			}
		}
	}
	mark := true
	if skipped > 0 || changedVariants == 0 {
		// 有跳过或一个变体都没改：走失败态（更显眼），用户下次会去看剩下的那些。
		mark = false
	}
	productListJump(c, mark, bulkPricingResultMsg(c, changedVariants, unchangedProducts, skipped, skipReason))
}

// bulkPricingResultMsg 批量改价的结论文案（四类结果各一句话）。
//
// 「一个都没改」与「跳过若干」必须能区分：两者都写成「没有变化」时，用户无法判断是
// 规则填错了、商品本来就在目标价上，还是这批商品根本没有变体。
func bulkPricingResultMsg(c *gin.Context, changedVariants, unchangedProducts, skipped int, reason string) string {
	if strings.TrimSpace(reason) == "" {
		reason = productErrFallbacks[productenums.ErrPricingTargetNotFound]
	}
	// 模板取自 product_err.go：写读共用 productBulkTextOf 这一个取法（提示页直接渲染成品文案）。
	switch {
	case skipped == 0 && changedVariants == 0:
		return fmt.Sprintf(productBulkTextOf(c, productPricingNoChange), strconv.Itoa(unchangedProducts))
	case skipped == 0:
		return fmt.Sprintf(productBulkTextOf(c, productPricingApplied),
			strconv.Itoa(changedVariants), strconv.Itoa(unchangedProducts))
	case changedVariants == 0:
		return fmt.Sprintf(productBulkTextOf(c, productPricingAllSkip), strconv.Itoa(skipped), reason)
	default:
		return fmt.Sprintf(productBulkTextOf(c, productPricingPartial),
			strconv.Itoa(changedVariants), strconv.Itoa(skipped), reason)
	}
}

// ProductsBulkDelete 批量删除商品（连同其全部变体，由 service 保证）。
//
// 逐条走同一条删除路径：失败的那条（已不存在 / 被其它数据引用）由服务端拒绝，其余照常删除
// —— 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染提示页，避免静默的部分成功。
func (h *productPageHandle) ProductsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productListJump(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.Delete(c.Request.Context(), &productdto.DeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(c, deleted, skipped, productBulkPartial, productBulkDone)
	productListJump(c, ok, msg)
}

// ProductPreviewFrame 商品详情页的真实预览帧（GET /admin/products/preview-frame?productId=&project=）。
//
// 与文章编辑页的同类帧同构：用**详情页模板**渲染一份不写库、不激活的预览（PreviewInstance），
// 看到的就是访客看到的样子。拼字段 HTML 那种回显照不出模板的任何东西 —— 商品详情页有图集、
// 变体分组、关联分区，靠拼字符串还原不了。
//
// 代价与文章页一样：只反映**已保存**的数据。改完先保存，再点「刷新预览」。
//
// 为什么 iframe 直接 src：详情页是完整文档（自带样式与脚本），塞进 srcdoc 会与后台页面同源、
// 脚本可能互扰；指一个页面组路由（Session 鉴权、不需 CSRF）最省事也最隔离。
func (h *productPageHandle) ProductPreviewFrame(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	tr := shell.TranslateFor(c)
	id := strings.TrimSpace(c.Query("productId"))
	projectID := strings.TrimSpace(c.Query("project"))
	if id == "" {
		productPreviewFrameNotice(c, tr("admin.products.preview.needSave", "保存这个商品后，这里会显示它的详情页真实形态。"))
		return
	}
	if h == nil || h.instances == nil {
		productPreviewFrameNotice(c, tr("admin.products.preview.unavailable", "预览不可用：发布能力未装配。"))
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationcontract.PreviewInstanceReq{
		EntityType: entityTypeProduct, EntityID: id, ProjectID: projectID,
	})
	if err != nil {
		// 原文只进日志（后台页面不得直出内部错误）；页面上给一句能行动的话。
		logger.Scene("product").With("product_id", id).Error(err, "商品详情页预览渲染失败")
		productPreviewFrameNotice(c, tr("admin.products.preview.failed", "预览渲染失败（多半是这个商品还没有可用的详情页模板）。"))
		return
	}
	if res == nil || strings.TrimSpace(res.HTML) == "" {
		productPreviewFrameNotice(c, tr("admin.products.preview.empty", "还没有可渲染的详情页：先给 product 类型建一套内容模板。"))
		return
	}
	// 直出的是**构建器渲染出来的详情页 HTML**（预览产物），不是错误信息。
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// productPreviewFrameNotice 预览不可用时的替代页（iframe 里的一句人话）。
//
// 与 content 模块那个同形但是两份：各自在自己的模块包里（跨模块共用要走契约，而这是纯展示装配）。
// 不用 5xx：iframe 对 5xx 显示浏览器自带错误页，读的人只会以为后台坏了 —— 而这里大多数情况是
// 「还没保存」或「还没建模板」，都是正常状态。
func productPreviewFrameNotice(c *gin.Context, text string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(
		`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>`+
			`body{margin:0;padding:24px;font:14px/1.7 -apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;`+
			`color:#57606a;background:#fff;}p{margin:0;}</style></head><body><p>`+html.EscapeString(text)+`</p></body></html>`))
}

const (
	MsgProductsTitle          = "MsgProductsTitle"
	MsgTranslationSaveFailed  = "MsgTranslationSaveFailed"
	MsgTranslationInvalid     = "MsgTranslationInvalid"
	MsgTranslationStale       = "MsgTranslationStale"
	MsgTranslationLangInvalid = "MsgTranslationLangInvalid"
)

// scoreView SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type scoreView struct {
	OK       bool
	Total    int
	Grade    string
	Sections []scoreSectionView
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重。
	ProfileType   string
	ProfileReason string
	// Duplicates 与本页标题重复的其它页面（审计 SEO-018 编辑期轻量版）。
	// **列出冲突页面本身**而不是只报数量：只报「有重复」运营不知道该去改哪一页。
	Duplicates    []string
	DuplicateNote string
	// Empty 空态文案（读不到实体时给一句可读的话，而不是让面板整体不可用）。
	Empty     string
	SerpTitle string
	SerpURL   string
	SerpDesc  string
}

// scoreSectionView 单个评分维度。
type scoreSectionView struct {
	Label      string
	Color      string
	ColorLabel string
	Score      int
	Max        int
	Issues     []scoreIssueView
}

// scoreIssueView 单条未达标检查（Target 非空表示可点击定位）。
type scoreIssueView struct {
	Text   string
	Target string
}

// 评分等级文案（颜色 → 当前语言）的**唯一映射**在 seoscore.ScoreGradeText
//（internal/seo/score_grade.go），调用点在 product_seo_score_page.go 的
// entityScoreView。本文件此前自带一份 key + 中文兜底表（与本模块 enums 里的
// admin.seo.grade.* 常量配套），已按「同一批展示文案只能有一份定义」收编 ——
// 收编后 product 取到的是 content / project 用的同一批词条 admin.seo.score.grade.*。

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// productTranslationMsgFallback 商品翻译页提示的中文兜底（dashboard enums 常量是
// sys_i18n 的 key，缺词条时用这里的原文；只列本页真正会产生的那几条）。
var productTranslationMsgFallback = map[string]string{
	MsgTranslationSaveFailed:  "译文保存失败，请稍后重试",
	MsgTranslationInvalid:     "提交数据不完整，请刷新页面后重试",
	MsgTranslationStale:       "原文已变更，请刷新页面后重新翻译",
	MsgTranslationLangInvalid: "目标语言未启用，请先在站点设置里启用",
	// 行级校验结论（validateProductTarget 的返回值，见 product_translation_page_data.go）：
	// 它们是页面上最常出现的几句话，同样必须能按语言取词。
	msgProductTargetContextInvalid: "语境非法：不是商品域的可翻译字段",
	msgProductTargetSourceSkipped:  "原文不参与翻译（空串、纯数字或纯符号）",
	msgProductTargetEmpty:          "译文不能为空",
	msgProductTargetShapeMismatch:  "译文形态与原文不一致：原文含 HTML 标签时译文也必须含标签",
}

// translationMsg 把 enums key 翻成当前语言；非 key（如 service 校验的原始中文）原样返回。
func translationMsg(c *gin.Context, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	return shell.TranslateFor(c)(msg, productTranslationMsgFallback[msg])
}

// translationMsgs 批量翻译提示文案。
func translationMsgs(c *gin.Context, msgs []string) []string {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if text := translationMsg(c, m); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// containsString 判断切片是否含目标值。
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// translationLangOption 工作台语言下拉项。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

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

// —— HTMX 写表单的失败分档（各写表单共用）——
//
// 表单保留原生 `method="post" action=…`，另加 `hx-post`（htmx 优先拦住 submit）；
// 服务端按 `HX-Request` **分档**：
//
//	· htmx 请求：失败返 **200 + 片段**（错误槽 + 回填后的表单）—— 抽屉 / 页面原地留住输入；
//	· 原生请求：整页提示（shell.RenderJump）回来源页（取代原先的 302 + ?err=）。
//
// 分档判据只有 isHXRequest 一处实现：两个出口必须认同同一个信号，否则会出现
// 「片段按 htmx 走、跳转按原生走」这种自相矛盾的档位。
//
// 原 redirectWhere / hxFragment 两个 helper 已删除：写动作的结论统一由 shell.RenderJump
// 渲染（它对 htmx 自己发 HX-Redirect），回填片段分支由各 handler 就地 `c.HTML` 渲染。

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 大小写不敏感、去首尾空白：与 shell.IsHXRequest 同口径（RenderJump 的分档依赖它）。
func isHXRequest(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
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

// product_view.go - 商品页的视图构造（变体行、规格标签、金额/评分格式化、仓库下拉
// 与捆绑构成面板）。

// variantListRows 变体「清单」表的初始行 —— 直接来自**库里已有的变体**。
//
// 关键语义（docs/14 §8「预览—保存」）：清单的种子是既有变体，**不是重算笛卡尔积** ——
// 重算会让上次删掉的组合自己冒出来。只有运营再点一次「生成组合」（显式动作）才会
// 把新组合追加进清单；保存之前库里一个字节都不变，所以行上的「移除」只是移出清单。
//
// OptionValues 是**规范化后的 JSON 文本**（逐行原样回传服务端做组合去重与跳过提示），
// 规格列的可读文本由服务端拼（specLabel），前端不做属性名解析。
func variantListRows(detail *productdto.ProductResp) []gin.H {
	rows := make([]gin.H, 0, len(detail.Variants))
	for _, v := range detail.Variants {
		rows = append(rows, gin.H{
			"ID": v.ID, "SKUCode": v.SKUCode, "Spec": specLabel(v.OptionValues, detail.Attributes),
			"OptionValues": optionValuesJSON(v.OptionValues),
			"Price":        formatAmount(v.Price),
			"ComparePrice": formatNullableAmount(v.ComparePrice),
			"CostPrice":    formatNullableAmount(v.CostPrice),
			"Enabled":      v.Enabled, "StockTotal": v.StockTotal,
		})
	}
	return rows
}

// optionValuesJSON 变体组合的规范化 JSON 文本（空 / null 归一到 {}）。
//
// 它同时是清单行的 data 属性值与预览去重的入参：组合为空的对象表示「无规格变体」
// （商品创建时的首个变体），归一成 {} 而不是空串，前端 JSON.parse 才不会炸。
func optionValuesJSON(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "{}"
	}
	return s
}

// variationAttributes 参与变体的属性组（组合生成面板只勾选这些组）。
func variationAttributes(attrs []*productdto.AttributeResp) []*productdto.AttributeResp {
	out := make([]*productdto.AttributeResp, 0, len(attrs))
	for _, a := range attrs {
		if a != nil && a.IsVariation {
			out = append(out, a)
		}
	}
	return out
}

// specLabel 规格组合的可读文本：按属性组定义顺序把「组名 值名」拼起来。
//
// 组已被删除或 key 改过的历史组合用原始 key→值兜底显示，不让后台丢信息。
func specLabel(raw json.RawMessage, attrs []*productdto.AttributeResp) string {
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return "—"
	}
	used := map[string]bool{}
	parts := make([]string, 0, len(m))
	for _, a := range attrs {
		if a == nil {
			continue
		}
		value, ok := m[a.Key]
		if !ok {
			continue
		}
		used[a.Key] = true
		label := value
		for _, v := range a.Values {
			if v.Key == value {
				label = v.Label
				break
			}
		}
		parts = append(parts, a.Name+" "+label)
	}
	rest := make([]string, 0, len(m))
	for k, v := range m {
		if !used[k] {
			rest = append(rest, k+" "+v)
		}
	}
	sort.Strings(rest)
	parts = append(parts, rest...)
	return strings.Join(parts, " · ")
}

// formatAmount 数值 → 后台展示文本（整数不带小数尾巴）。
//
// 唯一实现在 pkg/money.FormatYuan（审计 CQ-013：此前与商品构建期的 formatPrice
// 逐字节重复）。保留本名字是因为同包多个后台页面文件共用它。
func formatAmount(v float64) string { return money.FormatYuan(v) }

// formatNullableAmount 可空数值 → 展示文本（空显示为 —）。
func formatNullableAmount(v *float64) string {
	if v == nil {
		return "—"
	}
	return formatAmount(*v)
}

// formatScore 评分统一两位小数（4.5 → 4.50）。
//
// 与集合源给前台的 rating 字段同一口径：评分是 0~5 的小数，
// 用最短表示会得到 4.5 / 4.25 混排，两位小数更符合评分展示习惯。
func formatScore(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// ratingOf 读某商品的评分明细与投影值（读不到就按「没有评分」处理，页面照常渲染）。
//
// projectID 由调用方给出（DB-009）：product_ratings 有 FORCE 策略，明细查询要工程作用域。
// 兜底解析（resolveProjectID 取唯一工程）在多工程下会直接判参数错，所以后台页这里把
// 自己在用的那个工程显式传下去，不走兜底。
func ratingOf(svc productcontract.ProductService, ctx context.Context, projectID, productID string) *productdto.RatingResp {
	res, err := svc.ListRatings(ctx, &productdto.ListRatingsReq{ProductID: productID, ProjectID: projectID})
	if err != nil || res == nil {
		return &productdto.RatingResp{}
	}
	return res
}

// —— 捆绑构成（商品详情页按 type=bundle 渲染的编辑区）——

// bundlePanelReadFailedKey 捆绑配置读取失败的词条 key。
//
// 与捆绑配置页同一句：两处读的是同一份配置，失败原因也同一批（商品不存在 /
// 库存真源端口未接入，后者拿不到可用量）。复用词条而不是另起一句，
// 免得同一件事在两个页面上给出两种说法。
const bundlePanelReadFailedKey = "admin.product_bundle.detailFailed"

// bundlePanelReadFailedFallback 上述 key 的中文兜底（i18n 未初始化或词条缺失时用）。
const bundlePanelReadFailedFallback = "配置读取失败：商品不存在，或库存真源端口未接入（无法给出可用量）。"

// isBundleProduct 商品类型是否为捆绑容器（详情页据此决定要不要渲染「捆绑构成」）。
//
// 判据只认类型本身，不看「bundle_items 是否非空」——后者是迁移 238 之前的旧口径，
// 分不清「主体卖自己的 SKU」与「容器卖组合」（bundle 商品的价 0 变体就是这么来的）。
func isBundleProduct(productType string) bool {
	return productType == productmodel.TypeBundle
}

// bundleSourceKindLabel 成员来源 kind → 当前语言标签（空 = 历史配置 / 手工指定）。
func bundleSourceKindLabel(tr func(key, fallback string) string, kind string) string {
	switch kind {
	case productenums.BundleSourceProduct:
		return tr(productenums.ProductBundleSourceProduct, "从商品导入")
	case productenums.BundleSourceWarehouse:
		return tr(productenums.ProductBundleSourceWarehouse, "从仓库选")
	case productenums.BundleSourceAttributes:
		return tr(productenums.ProductBundleSourceAttributes, "自选属性组合")
	default:
		return tr(productenums.ProductBundleSourceNone, "手工指定")
	}
}

// bundleSourceLabel 成员来源快照的可读文本（仅用于展示 —— 来源不是身份）。
//
// 「从仓库选」再多带两个字段：仓库 SKU 与外部编码 —— 运营对账时手上可能是其中任意一个；
// 仓库 id 留在 JSON 里做溯源，不搬上表格（uuid 对读的人没有含义）。
func bundleSourceLabel(tr func(key, fallback string) string, o productdto.BundleOption) string {
	label := bundleSourceKindLabel(tr, strings.TrimSpace(o.SourceKind))
	parts := make([]string, 0, 2)
	if sku := strings.TrimSpace(o.WarehouseSKU); sku != "" {
		parts = append(parts, tr(productenums.ProductBundleSourceWarehouseSKULabel, "仓库 SKU")+" "+sku)
	}
	if ext := strings.TrimSpace(o.ExternalSKU); ext != "" {
		parts = append(parts, tr(productenums.ProductBundleSourceExternalSKULabel, "外部编码")+" "+ext)
	}
	if len(parts) == 0 {
		return label
	}
	return label + " · " + strings.Join(parts, " · ")
}

// bundlePanelRows 捆绑构成表的行：成员 SKU 的规则、可用量与后台价格口径。
//
// 成员挂牌价（ItemPrice）在这里只作**参考**：套餐金额只由容器价得出，成员价不参与
// 任何计价与前台展示（迁移 238 的类型不变量）；成员成本只进后台口径。
//
// SKU 已被删除时配置里只剩 variantId（SKUCode 为空），照常出一行并标注 ——
// 静默丢项会让人以为配置里本来就没有这一项，也就不会去修它。
func bundlePanelRows(tr func(key, fallback string) string, options []*productdto.BundleOptionDetail) []gin.H {
	rows := make([]gin.H, 0, len(options))
	for _, o := range options {
		if o == nil {
			continue
		}
		// Missing 表示配置指向的变体已经不存在（与「存在但被停用」是两回事，
		// 状态文案与可行动作都不同，故不合并成一个 bool）。
		missing := o.SKUCode == ""
		itemPrice, costPrice := formatAmount(o.ItemPrice), formatNullableAmount(o.CostPrice)
		if missing {
			// 变体没了，价格与成本都无从谈起：这里的 0 会被读成「这个成员不要钱」。
			itemPrice, costPrice = "—", "—"
		}
		rows = append(rows, gin.H{
			"VariantID":   o.VariantID,
			"SKUCode":     o.SKUCode,
			"ProductName": o.ProductName,
			"Required":    o.Required,
			"DefaultQty":  o.DefaultQty,
			"MinQty":      o.MinQty,
			"MaxQty":      o.MaxQty,
			"Available":   o.Available,
			"ItemPrice":   itemPrice,
			"CostPrice":   costPrice,
			"Enabled":     o.Enabled,
			"Missing":     missing,
			// 来源快照（仅展示）：空 = 手工指定 / 历史配置。
			"SourceLabel": bundleSourceLabel(tr, o.BundleOption),
		})
	}
	return rows
}

// bundlePanel 商品详情页「捆绑构成」区块的数据（仅 type=bundle 调用）。
//
// 读不出配置时**不静默**：区块照常渲染并带一条可读原因（区块本身就是运维发现
// 「配置读不出来」的地方，整块消失会让人以为这个商品不是捆绑品）。
// 错误原因只给词条文案，不铺 err.Error()（内部包装文案对运营不可行动）。
func (h *productPageHandle) bundlePanel(c *gin.Context, projectID, productID string) gin.H {
	panel := gin.H{
		"Loaded":    false,
		"ErrorText": "",
		// 容器价是套餐金额的唯一来源，读不出来时给 —（不拿成员价兜底：那是错的口径）。
		"BasePrice":   "—",
		"Options":     []gin.H{},
		"OptionCount": 0,
	}
	detail, err := h.products.GetBundleConfig(c.Request.Context(), &productdto.GetBundleConfigReq{
		ProductID: productID,
		// 工程显式传下去（DB-009）：配置详情含库存可用量，走真源查询要在工程作用域里。
		// 后台页不走 resolveProjectID 的唯一工程兜底（多工程下它会直接判参数错）。
		ProjectID: projectID,
	})
	if err != nil || detail == nil {
		panel["ErrorText"] = shell.TranslateFor(c)(bundlePanelReadFailedKey, bundlePanelReadFailedFallback)
		return panel
	}
	rows := bundlePanelRows(shell.TranslateFor(c), detail.Options)
	panel["Loaded"] = true
	panel["BasePrice"] = formatAmount(detail.BasePrice)
	panel["Options"] = rows
	panel["OptionCount"] = len(rows)
	return panel
}

// warehouseOptions 某工程的仓库下拉项（issue #15；默认仓在最前并标注）。
//
// 未注入仓库契约时返回空列表：模板此时不渲染下拉，变体创建按「不指定仓库」处理。
//
// 除展示用的 Label 外还带上 Code 与 Name：列表「库存」列的分仓明细要按**工程仓库清单**
// 逐仓显示三态（服务端只返回有库存行的仓，「这个仓没有这一行」= 未入库，是页面才知道的事实）。
func (h *productPageHandle) warehouseOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []gin.H, err error) {
	out = []gin.H{}
	if h.inventories == nil || projectID == "" {
		return out, nil
	}
	rows, lerr := h.inventories.ListWarehouses(ctx, &inventorycontract.ListWarehouseReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, w := range rows {
		label := w.Name + "（" + w.Code + "）"
		if w.IsDefault {
			// 默认仓后缀是**跨模块共用的词条**：真源在库存模块（enums 与词条都只有一条，
			// 见 inventoryenums.InventoryChangeWarehouseDefaultSuffix）—— 两处显示必须同步改。
			label += " " + tr(inventoryenums.InventoryChangeWarehouseDefaultSuffix, "· 默认仓")
		}
		out = append(out, gin.H{
			"ID": w.ID, "Label": label, "Code": w.Code, "Name": w.Name,
			"IsDefault": w.IsDefault,
		})
	}
	return out, nil
}

// productStockCell 商品列表「库存」列的数据（三态 + 分仓明细）。
//
// 三态口径（docs/14 §1.4）：任一仓不跟踪 → ∞（**绝不求和**，求和等于把无限当 0）；
// 全部跟踪 → 各仓数量之和（0 就显示 0）；一个仓都没有这一行 → 未入库。
//
// 分仓明细按**工程仓库清单**逐仓列出：服务端返回的 StockWarehouses 只含有库存行的仓，
// 「某仓没有行」同样是一条信息（该仓未入库）—— 只列有行的仓会让运营以为这个商品只在那几个仓。
//
// 文案（∞ / 未入库）留在模板侧用 tr() 取词，这里只给出状态与数字：
// 一种语言一个值，别把中文拼进数据层（接口调用方拿到的会是中文）。
func productStockCell(detail *productdto.ProductResp, warehouses []gin.H) gin.H {
	cell := gin.H{
		"State": productenums.StockStateNone,
		"Total": 0,
		"Rows":  []gin.H{},
	}
	if detail == nil {
		return cell
	}
	if detail.StockState != "" {
		cell["State"] = detail.StockState
	}
	cell["Total"] = detail.StockTotal
	byWarehouse := make(map[string]*productdto.ProductWarehouseStockResp, len(detail.StockWarehouses))
	for _, row := range detail.StockWarehouses {
		if row != nil {
			byWarehouse[row.WarehouseID] = row
		}
	}
	rows := make([]gin.H, 0, len(warehouses))
	for _, w := range warehouses {
		id, _ := w["ID"].(string)
		label, _ := w["Label"].(string)
		item := gin.H{"WarehouseID": id, "WarehouseLabel": label,
			"State": productenums.StockStateNone, "Quantity": 0}
		if row, ok := byWarehouse[id]; ok {
			item["State"] = row.State
			item["Quantity"] = row.Quantity
			if row.SKUCode != "" {
				item["SKUCode"] = row.SKUCode
			}
		}
		rows = append(rows, item)
	}
	cell["Rows"] = rows
	return cell
}

// pricingHistoryLimit 后台留痕台账展示的批次数（每批再取一次明细）。
const pricingHistoryLimit = 10

// pricingForm 定价表单的取值（试算后要原样回填，用户接着点应用）。
type pricingForm struct {
	ProjectID  string
	RuleType   string
	Multiplier string
	Amount     string
	Margin     string
	Rounding   string
	Scope      string
	TargetID   string
	Status     string
	Keyword    string
	CategoryID string
	BrandID    string
	TagID      string
	Note       string
}

// ProductPricingPage 定价工具页：规则参考表 + 改价表单 + 留痕台账。
func (h *productPageHandle) ProductPricingPage(c *gin.Context) {
	h.renderPricingPageWith(c, nil, nil, "")
}

// ProductPricingPreview 试算（不落库、不留痕），结果渲染回同一页。
func (h *productPageHandle) ProductPricingPreview(c *gin.Context) {
	form, req := readPricingForm(c)
	preview, err := h.products.PreviewPricing(c.Request.Context(), &productdto.PricingPreviewReq{PricingRuleReq: *req})
	if err != nil {
		// 试算失败不重定向：用户填的表单要留在眼前，否则「哪一项填错了」无从改起。
		// 试算失败的原文（含 PG 原文）只进日志，页面上给可读文案。
		h.renderPricingPageWith(c, &form, nil, productErrText(c, err))
		return
	}
	h.renderPricingPageWith(c, &form, preview, "")
}

// ProductPricingApply 应用调价（落库 + 留痕 + 顺带重算自动标签）。
func (h *productPageHandle) ProductPricingApply(c *gin.Context) {
	_, req := readPricingForm(c)
	res, err := h.products.ApplyPricing(c.Request.Context(), &productdto.PricingApplyReq{
		PricingRuleReq: *req,
		Note:           strings.TrimSpace(c.PostForm("note")),
		// 操作人取自会话，客户端的表单字段不作数（留痕不可伪造）。
		OperatorID: shell.CurrentUserIDText(c),
	})
	if err != nil {
		productPricingJump(c, false, productErrText(c, err))
		return
	}
	// 成功回执：保留原先页面上那句话（「本次改动 N 个变体，明细见下方留痕台账」），
	// 但改由提示页渲染（取代 ?applied= 计数 + 页内提示条）。
	tr := shell.TranslateFor(c)
	productPricingJump(c, true, tr("admin.product_pricing.applied.lead", "已应用调价：本次改动 ")+
		strconv.Itoa(res.ChangedCount)+tr("admin.product_pricing.applied.tail", " 个变体，明细见下方留痕台账。"))
}

// renderPricingPageWith 定价页的统一渲染入口（form / preview / errMsg 三者可空）。
func (h *productPageHandle) renderPricingPageWith(c *gin.Context, form *pricingForm, preview *productdto.PricingPreviewResp, errMsg string) {
	ctx := c.Request.Context()
	projects := h.pricingProjects(ctx)
	selected := strings.TrimSpace(c.Query("project"))
	if form != nil && form.ProjectID != "" {
		selected = form.ProjectID
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	rules := h.products.ListPricingRuleTypes(ctx)
	roundings := h.products.ListPricingRoundingOptions(ctx)
	view := defaultPricingForm(rules, roundings)
	if form != nil {
		view = *form
	}
	if view.ProjectID == "" {
		view.ProjectID = selected
	}
	c.HTML(http.StatusOK, "admin/product/product_pricing.html", shell.Prepare(c, gin.H{
		"title":           shell.TranslateFor(c)(productenums.ProductPricingTitle, "定价工具"),
		"menu":            "product-pricing",
		"Projects":        projects,
		"SelectedProject": selected,
		"Rules":           rules,
		"Roundings":       roundings,
		"Form":            view,
		"HasPreview":      preview != nil,
		"PreviewSummary":  pricingPreviewSummary(preview),
		"PreviewRows":     pricingLineRows(previewLines(preview)),
		"History":         h.pricingHistory(ctx, selected),
		// Err 只剩**试算失败原地重渲染**这一条来源（errMsg 是本页 handler 的产物，
		// 已过 productErrText）；写动作（应用调价）的结论走 shell.RenderJump 渲染提示页，
		// 原先 ?err= / ?applied= 的读侧已整批删除（见 product_jump.go）。
		"Err": errMsg,
		// 页面上下文（表单 action 的 query）：应用调价的提示页由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productPricingBackKeys...),
	}))
}

// pricingProjects 工程列表（读失败时返回空列表：定价页不该因为一个下拉整页打不开）。
func (h *productPageHandle) pricingProjects(ctx context.Context) (projects []projectdto.ProjectResp) {
	list, err := h.projects.List(ctx)
	if err != nil {
		return []projectdto.ProjectResp{}
	}
	return list
}

// pricingHistory 调价留痕（批次 + 逐变体明细）。
func (h *productPageHandle) pricingHistory(ctx context.Context, projectID string) (rows []gin.H) {
	rows = []gin.H{}
	if strings.TrimSpace(projectID) == "" {
		return rows
	}
	list, err := h.products.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{
		ProjectID: projectID, Limit: pricingHistoryLimit,
	})
	if err != nil {
		return rows
	}
	for _, a := range list {
		detail, derr := h.products.GetPriceAdjustment(ctx, &productdto.GetPriceAdjustmentReq{ID: a.ID})
		if derr != nil {
			// 单条明细读失败不该让整页打不开：退回批次本身的汇总。
			rows = append(rows, pricingHistoryRow(a))
			continue
		}
		rows = append(rows, pricingHistoryRow(detail))
	}
	return rows
}

// pricingHistoryRow 批次 → 模板行（时间压成可读文本，明细逐条列出原价与新价）。
func pricingHistoryRow(a *productdto.PriceAdjustmentResp) gin.H {
	items := make([]gin.H, 0, len(a.Items))
	for _, it := range a.Items {
		items = append(items, gin.H{
			"ProductName": it.ProductName, "SKUCode": it.SKUCode,
			"OldPrice": formatAmount(it.OldPrice), "NewPrice": formatAmount(it.NewPrice),
			"Diff": formatSignedAmount(it.Diff),
		})
	}
	return gin.H{
		"ID": a.ID, "RuleLabel": a.RuleLabel, "ScopeLabel": a.ScopeLabel,
		"RoundingLabel": a.RoundingLabel, "FilterLabel": a.FilterLabel,
		"ChangedCount": a.ChangedCount, "VariantCount": a.VariantCount,
		"Note": a.Note, "OperatorID": a.OperatorID,
		"CreatedAtText": pricingTimeLabel(a.CreatedAt),
		"Items":         items,
	}
}

// pricingLineRows 试算行 → 模板行（金额与状态标签由服务端算好）。
func pricingLineRows(lines []*productdto.PricingLineResp) []gin.H {
	rows := make([]gin.H, 0, len(lines))
	for _, l := range lines {
		if l == nil {
			continue
		}
		rows = append(rows, gin.H{
			"ProductName": l.ProductName, "SKUCode": l.SKUCode,
			"CostPrice": formatNullableAmount(l.CostPrice),
			"OldPrice":  formatAmount(l.OldPrice), "NewPrice": formatAmount(l.NewPrice),
			"Diff":   formatSignedAmount(l.NewPrice - l.OldPrice),
			"Status": l.Status, "StatusLabel": l.StatusLabel,
			"IsChanged":   l.Status == productenums.PricingLineChanged,
			"IsSkipped":   l.Status == productenums.PricingLineSkipped,
			"ReasonLabel": l.ReasonLabel,
		})
	}
	return rows
}

// pricingPreviewSummary 试算结果的汇总行（模板只做展示，不做计算 —— 也避免模板解指针）。
func pricingPreviewSummary(p *productdto.PricingPreviewResp) gin.H {
	if p == nil {
		return gin.H{}
	}
	return gin.H{
		"RuleLabel":      p.RuleLabel,
		"RoundingLabel":  p.RoundingLabel,
		"ScopeLabel":     p.ScopeLabel,
		"TargetCount":    p.TargetCount,
		"ChangedCount":   p.ChangedCount,
		"UnchangedCount": p.UnchangedCount,
		"SkippedCount":   p.SkippedCount,
	}
}

// previewLines 试算结果的明细（nil 安全）。
func previewLines(p *productdto.PricingPreviewResp) []*productdto.PricingLineResp {
	if p == nil {
		return nil
	}
	return p.Lines
}

// readPricingForm 读定价表单并归一为 service 入参。
//
// 规则参数只在规则类型对应的输入框里取（与标签页同一手法）：
// 「只接受内置参数」在后台这一侧同样成立，多余输入不会进 params。
// 数字解析失败时给空对象，由 service 给出统一的可读错误，这里不重复一套校验。
func readPricingForm(c *gin.Context) (form pricingForm, req *productdto.PricingRuleReq) {
	form = pricingForm{
		ProjectID:  strings.TrimSpace(c.PostForm("projectId")),
		RuleType:   strings.TrimSpace(c.PostForm("ruleType")),
		Multiplier: strings.TrimSpace(c.PostForm("multiplier")),
		Amount:     strings.TrimSpace(c.PostForm("amount")),
		Margin:     strings.TrimSpace(c.PostForm("margin")),
		Rounding:   strings.TrimSpace(c.PostForm("rounding")),
		Scope:      strings.TrimSpace(c.PostForm("scope")),
		TargetID:   strings.TrimSpace(c.PostForm("targetId")),
		Status:     strings.TrimSpace(c.PostForm("status")),
		Keyword:    strings.TrimSpace(c.PostForm("keyword")),
		CategoryID: strings.TrimSpace(c.PostForm("categoryId")),
		BrandID:    strings.TrimSpace(c.PostForm("brandId")),
		TagID:      strings.TrimSpace(c.PostForm("tagId")),
		Note:       strings.TrimSpace(c.PostForm("note")),
	}
	req = &productdto.PricingRuleReq{
		ProjectID:  form.ProjectID,
		RuleType:   form.RuleType,
		Rounding:   form.Rounding,
		Scope:      form.Scope,
		TargetID:   form.TargetID,
		Status:     form.Status,
		Keyword:    form.Keyword,
		CategoryID: form.CategoryID,
		BrandID:    form.BrandID,
		TagID:      form.TagID,
		RuleParams: pricingParamsFromForm(form),
	}
	return form, req
}

// pricingRuleReqFromForm 读「按规则改价」表单并套上指定的作用范围。
//
// 两个入口共用：独立定价页（/admin/product-pricing/preview|apply，范围由表单的 scope /
// targetId 决定）与商品列表的批量改价抽屉（逐个商品套 scope=product + 该商品 id）。
// 规则类型、规则参数、尾数的解析只有一份（readPricingForm + pricingParamsFromForm）——
// 抽屉若另抄一份，最先出问题的是「填了 amount 却被当成 multiplier」这类静默错配。
func pricingRuleReqFromForm(c *gin.Context, scope, targetID string) (req *productdto.PricingRuleReq) {
	_, req = readPricingForm(c)
	req.Scope = scope
	req.TargetID = targetID
	return req
}

// pricingParamsFromForm 按规则类型从表单拼规则参数（只取本类型用得到的键）。
func pricingParamsFromForm(form pricingForm) json.RawMessage {
	params := map[string]any{}
	switch form.RuleType {
	case productenums.PricingRuleCostMultiple:
		if form.Multiplier != "" {
			if v, err := parseFloat(form.Multiplier); err == nil {
				params["multiplier"] = v
			}
		}
	case productenums.PricingRuleCostMarkup, productenums.PricingRuleFixedPrice:
		if form.Amount != "" {
			if v, err := parseFloat(form.Amount); err == nil {
				params["amount"] = v
			}
		}
	case productenums.PricingRuleTargetMargin:
		if form.Margin != "" {
			if v, err := parseFloat(form.Margin); err == nil {
				params["margin"] = v
			}
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}

// defaultPricingForm 表单初值：第一条规则 + 第一种尾数处理 + 筛选集范围（批量改价的典型用法）。
func defaultPricingForm(rules []*productdto.PricingRuleTypeResp, roundings []*productdto.PricingRoundingOptionResp) pricingForm {
	form := pricingForm{
		Rounding: productenums.PricingRoundingNone,
		Scope:    productenums.PricingScopeFilter,
	}
	if len(rules) > 0 && rules[0] != nil {
		form.RuleType = rules[0].Type
	}
	if len(roundings) > 0 && roundings[0] != nil {
		form.Rounding = roundings[0].Value
	}
	return form
}

// pricingTimeLabel RFC3339 → 后台展示文本（同样的压轴规则见标签页的 recalcLabel）。
func pricingTimeLabel(at string) string {
	if strings.TrimSpace(at) == "" {
		return "—"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// formatSignedAmount 差值文本（正数带 + 号）。
func formatSignedAmount(v float64) string {
	if v > 0 {
		return "+" + formatAmount(v)
	}
	return formatAmount(v)
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// entityTypeProduct 发布实例的实体类型标识（与 presentation 的实例登记一致）。
const entityTypeProduct = "product"

// seoTitleIndexSize 标题索引每类实体的取样上限。
//
// 「编辑期轻量版」的重点是点一下就有结果（一次列表查询，不是遍历产物文件）。
// 超过上限时商品一侧由 service 按自己的分页上限收敛 —— 这点在注释里写明，
// 因为「索引不完整」会让冲突清单漏报，而漏报是检查类功能最不能有的失效模式。
const seoTitleIndexSize = 100

// SetSeoTitleSources 注入全站标题索引所需的另外两份只读契约（装配期调用，可空）。
//
// 页面与文章是同一个站点的其它页面，做 title 唯一性时必须在同一份索引里 ——
// 只比商品域的话，商品标题与文章标题撞车照样检不出来。两份契约都可空：
// 未注入时索引退化成商品 / 分类 / 品牌三域，评分本身照常（缺依赖不该变成 500）。
func (h *productPageHandle) SetSeoTitleSources(pages pagecontract.PageService, contents contentcontract.ContentService) {
	h.seoPages = pages
	h.seoContents = contents
}

// ProductScorePanel 商品详情页的编辑期评分（POST /admin/products/seo-score）。
func (h *productPageHandle) ProductScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindProduct,
		strings.TrimSpace(c.PostForm("productId")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 scoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// ProductCategoryScorePanel 商品分类页的编辑期评分（POST /admin/product-categories/seo-score）。
func (h *productPageHandle) ProductCategoryScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindCategory,
		strings.TrimSpace(c.PostForm("id")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 scoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// ProductBrandScorePanel 品牌页的编辑期评分（POST /admin/product-brands/seo-score）。
func (h *productPageHandle) ProductBrandScorePanel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindBrand,
		strings.TrimSpace(c.PostForm("id")), projectID)
	if in == nil {
		// 判据是 in == nil（「没取到实体」），**不是** emptyView.OK ——
		// 成功路径返回的也是零值 scoreView（OK=false），用它分流会把每次成功都判成空态。
		renderScoreView(c, emptyView)
		return
	}
	h.renderEntityScore(c, in, projectID, selfID)
}

// entityScoreViewOf 算分 + 查 title 重复 → 片段视图（三个 POST 端点与抽屉共用同一段流程）。
func (h *productPageHandle) entityScoreViewOf(ctx context.Context, c *gin.Context,
	in *scoring.EntityPageInput, projectID, selfID string) scoreView {
	res := scoring.ScoreEntityPage(in)
	tr := shell.TranslateFor(c)
	dups := scoring.DuplicateTitles(scoring.EntityTitle(in), h.seoTitleIndex(ctx, projectID, tr), selfID)
	return entityScoreView(tr, in, res, dups)
}

// renderEntityScore 算分 + 查 title 重复 + 渲染片段（三个 POST 入口共用）。
func (h *productPageHandle) renderEntityScore(c *gin.Context, in *scoring.EntityPageInput, projectID, selfID string) {
	tr := shell.TranslateFor(c)
	// t 是片段模板的取词函数：seo_score 片段不经 shell.Prepare，缺 t 时 Jet 把取词调用
	// 求值成空串（不报错、不 500、不记日志）—— 整片提示会变成空白。
	c.HTML(http.StatusOK, "fragments/seo_score",
		gin.H{"Score": h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID), "t": tr})
}

// renderScoreView 渲染评分片段（空态与结果共用同一份模板：片段按 Score.OK 分流）。
func renderScoreView(c *gin.Context, sv scoreView) {
	tr := shell.TranslateFor(c)
	c.HTML(http.StatusOK, "fragments/seo_score", gin.H{"Score": sv, "t": tr})
}

// entityScoreView 评分结果 → 片段视图（含页型回显与 title 冲突清单）。
func entityScoreView(tr func(key, fallback string) string, in *scoring.EntityPageInput, res *scoring.Result,
	dups []scoring.TitleEntry) scoreView {
	sv := scoreView{OK: true}
	if res != nil {
		sv.Total = res.Total
		sv.Grade = res.Grade
		// 页型与调权理由回显（docs/02-E1 §5）：编辑者要能看出「这份分数是按商品页的
		// 尺子量的」，否则同一段描述在商品页比文章页高几分会被当成评分器不稳。
		if res.Profile != nil {
			sv.ProfileType = res.Profile.Type
			sv.ProfileReason = res.Profile.Reason
		}
		// 未达标行的整句模板：标签 / 实测 / 基准 / 建议四段都是数据，句子的语序与标点
		// 由词条决定（英文的冒号与括号与中文不同），命名占位符 + FillTranslate 负责填充
		//（词条被写坏时自动回落下面那句中文兜底）。
		for _, sec := range res.Sections {
			item := scoreSectionView{
				Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
				ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
			}
			for _, ck := range sec.Checks {
				if ck.Score >= ck.Max {
					continue
				}
				item.Issues = append(item.Issues, scoreIssueView{
					Text: i18n.FillTranslate(tr, productenums.SEOIssueLine,
						"{label}：{actual}（基准 {benchmark}）→ {hint}",
						map[string]string{
							"label": ck.Label, "actual": ck.Actual,
							"benchmark": ck.Benchmark, "hint": ck.Hint,
						}),
					Target: ck.Target,
				})
			}
			sv.Sections = append(sv.Sections, item)
		}
	}
	title := scoring.EntityTitle(in)
	sv.SerpTitle = title
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(productenums.SEOSerpTitleEmpty, "（未填写标题）")
	} else {
		sv.SerpTitle = seoscore.TruncateDisplayWidth(sv.SerpTitle, 60)
	}
	sv.SerpURL = in.URL
	if sv.SerpURL == "" {
		sv.SerpURL = tr(productenums.SEOSerpURLEmpty, "（线上路径未知）")
	}
	sv.SerpDesc = strings.TrimSpace(in.SEODescription)
	if sv.SerpDesc == "" {
		sv.SerpDesc = strings.TrimSpace(in.Description)
	}
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(productenums.SEOSerpDescEmpty, "（未填写描述）")
	}
	// title 唯一性（SEO-018 编辑期轻量版）：结论形状与发布侧体检一致 —— 列出全部冲突页面。
	if len(dups) > 0 {
		sv.DuplicateNote = scoring.DuplicateTitleMessage(title, dups)
		for _, d := range dups {
			sv.Duplicates = append(sv.Duplicates, d.Page)
		}
	}
	return sv
}

// seoTitleIndex 同工程已发布 / 已有内容的标题索引（审计 SEO-018 编辑期轻量版）。
//
// 每类实体各一次列表查询（商品 / 分类 / 品牌 / 页面 / 文章），不遍历产物文件 ——
// 那是发布侧体检（SEO-019）的活，编辑期要的是点一下就能出结果。
//
// 与发布侧对齐的是**结论形状**（列出全部冲突页面），不是数据来源：发布侧读的是真正
// 服务出去的 <title>，这里读的是实体字段（写之前就得能提示）。两者的差异写在
// scoring.DuplicateTitles 的注释里，改任一处都要一起看。
//
// 「已发布」的判据按实体能力各自取：
//   - 商品：status=published（草稿商品还没有线上页面，拿它判重复会让运营被一堆
//     没上架的东西挡住）；
//   - 分类 / 品牌：它们是分类法，没有发布状态，随商品上线 —— 全量参与比对；
//   - 页面：草稿文档里看不出发布与否，按全部页面参与（宁可多提示一次，
//     也不要漏掉一个即将上线的重复标题）。
func (h *productPageHandle) seoTitleIndex(ctx context.Context, projectID string,
	tr func(key, fallback string) string) []scoring.TitleEntry {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var out []scoring.TitleEntry
	if list, err := h.products.List(ctx, &productdto.ListReq{
		ProjectID: projectID, Status: productenums.StatusPublished, Size: seoTitleIndexSize,
	}); err == nil {
		for _, p := range list {
			if p == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				// 标题就是商品名（2026-09-30 合并后口径）：唯一性检查必须拿**会发布出去
				// 的那个标题**比对，再读 seo_title 只会拿库里的残留旧值去比。
				Title: p.Name,
				Page:  slashSlug(p.Slug),
				ID:    p.ID,
			})
		}
	}
	if cats, err := h.flatCategories(ctx, projectID); err == nil {
		for _, cat := range cats {
			if cat == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: cat.Name,
				Page:  slashSlug(cat.Slug),
				ID:    cat.ID,
			})
		}
	}
	if brands, err := h.listBrands(ctx, projectID); err == nil {
		for _, b := range brands {
			if b == nil {
				continue
			}
			out = append(out, scoring.TitleEntry{
				Title: b.Name,
				Page:  slashSlug(b.Slug),
				ID:    b.ID,
			})
		}
	}
	// 页面与文章（可空注入）：两者都是「同一个站点的其它页面」，缺了它们
	// 跨内容的重复标题检不出来 —— 但缺依赖不该让评分不可用，故按可选处理。
	if h.seoPages != nil {
		if drafts, err := h.seoPages.ListDrafts(ctx); err == nil {
			for _, d := range drafts {
				if d.ProjectID != projectID {
					continue
				}
				if title := pageDraftTitle(d.DraftDocument); title != "" {
					out = append(out, scoring.TitleEntry{Title: title, Page: slashSlug(d.DraftPath), ID: d.ID})
				}
			}
		}
	}
	if h.seoContents != nil {
		if rows, err := h.seoContents.List(ctx, &contentdto.ListReq{EntityType: "article", Limit: seoTitleIndexSize}); err == nil {
			for _, r := range rows {
				if r == nil || r.Data == nil {
					continue
				}
				// 文章的 SEO 标题即标题（2026-09-30 合并）：不再优先读 seoTitle。
				title := contentText(r.Data, "title")
				if title == "" {
					continue
				}
				// contents 列表接口没有工程过滤参数，标题索引据此标注来源：
				// 多工程部署时别的工程的文章也会进这份索引，宁可报一次重复让人核对，
				// 也好过因为查不到而假报「没有重复」。
				out = append(out, scoring.TitleEntry{
					Title: title,
					Page: i18n.FillTranslate(tr, productenums.SEOIndexArticle, "文章 {slug}",
						map[string]string{"slug": r.Slug}),
					ID: r.ID,
				})
			}
		}
	}
	return out
}

// instancePath 取实体详情页实例的线上路径（未注入实例契约 / 未发布 → 空串）。
func (h *productPageHandle) instancePath(ctx context.Context, entityType, entityID string) string {
	if h.instances == nil || strings.TrimSpace(entityID) == "" {
		return ""
	}
	inst, err := h.instances.GetByEntity(ctx, &presentationcontract.GetByEntityReq{
		EntityType: entityType, EntityID: entityID,
	})
	if err != nil || inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.URLPath)
}

// 评分实体类型（取数分派共用同一组取值）。
const (
	scoreKindProduct  = "product"
	scoreKindCategory = "category"
	scoreKindBrand    = "brand"
)

// entityScoreInputOf 按实体类型取数并构造评分输入。
//
// 三个 POST 端点共用这一份：同一个实体不该有两种取数口径（抽屉与行内按钮各写一份，
// 下场是「同一个商品在两处分数不同」）。
//
// 第三个返回值是**空态视图**（OK=false，Empty 已取好词）：非 OK 时调用方直接渲染它 ——
// 三个端点因此共用同一条失败路径，不必各自拼一句文案。
func (h *productPageHandle) entityScoreInputOf(ctx context.Context, c *gin.Context,
	kind, id, projectID string) (in *scoring.EntityPageInput, selfID string, emptyView scoreView) {
	tr := shell.TranslateFor(c)
	emptyOf := func(key, fallback string) scoreView { return scoreView{Empty: tr(key, fallback)} }

	if id == "" {
		switch kind {
		case scoreKindCategory:
			return nil, "", emptyOf(productenums.SEOEmptyMissingCategory, "缺少分类，无法评分")
		case scoreKindBrand:
			return nil, "", emptyOf(productenums.SEOEmptyMissingBrand, "缺少品牌，无法评分")
		default:
			return nil, "", emptyOf(productenums.SEOEmptyMissingProduct, "缺少商品，无法评分")
		}
	}

	switch kind {
	case scoreKindCategory:
		node, err := h.products.GetCategory(ctx, &productdto.GetCategoryReq{ID: id})
		if err != nil || node == nil {
			return nil, "", emptyOf(productenums.SEOEmptyCategoryUnreadable, "读不到这个分类，无法评分")
		}
		// SEO 标题 / 描述与「分类名 / 分类描述」合并（2026-09-30，与商品同口径）：
		// 分类名即 <title>、分类描述即 meta description。描述是富文本，先去掉标签
		// 再喂给评分器 —— 否则 <p> 会被算进 meta 描述长度。
		instPath := h.instancePath(ctx, "product_category", node.ID)
		return &scoring.EntityPageInput{
			Kind:           scoring.KindCategory,
			Name:           node.Name,
			Description:    stripEntityTags(node.Description),
			SEOTitle:       node.Name,
			SEODescription: stripEntityTags(node.Description),
			Slug:           node.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(node.Slug)),
			Images:         singleImage(node.Image, "hero"),
			ChildNames:     categoryChildNames(ctx, h, projectID, node.ID),
			Locale:         requestScoreLang(c),
			HasCanonical:   instPath != "",
			HasSchema:      instPath != "",
		}, node.ID, scoreView{}

	case scoreKindBrand:
		brand, err := h.products.GetBrand(ctx, &productdto.GetBrandReq{ID: id})
		if err != nil || brand == nil {
			return nil, "", emptyOf(productenums.SEOEmptyBrandUnreadable, "读不到这个品牌，无法评分")
		}
		// 同分类：品牌名即 <title>、品牌描述即 meta description（2026-09-30 合并）。
		instPath := h.instancePath(ctx, "product_brand", brand.ID)
		return &scoring.EntityPageInput{
			Kind:           scoring.KindBrand,
			Name:           brand.Name,
			Description:    stripEntityTags(brand.Description),
			SEOTitle:       brand.Name,
			SEODescription: stripEntityTags(brand.Description),
			Slug:           brand.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(brand.Slug)),
			Images:         singleImage(brand.Logo, "hero"),
			Locale:         requestScoreLang(c),
			HasCanonical:   instPath != "",
			HasSchema:      instPath != "",
		}, brand.ID, scoreView{}

	default:
		detail, err := h.products.Get(ctx, &productdto.GetReq{ID: id})
		if err != nil || detail == nil {
			return nil, "", emptyOf(productenums.SEOEmptyProductUnreadable, "读不到这个商品，无法评分")
		}
		// 线上路径与「产物是否已带 canonical / JSON-LD」是同一个问题的两面：
		// 实例有线上路径 = 详情页真的发布过 = 构建期已注入这两样（presentation 的 applyEntitySEO）。
		instPath := h.instancePath(ctx, entityTypeProduct, detail.ID)
		return &scoring.EntityPageInput{
			Kind:        scoring.KindProduct,
			Name:        detail.Name,
			Subtitle:    detail.Subtitle,
			Description: entityPlainText(detail.Description),
			// 评分与前台对齐（2026-09-30 合并）：商品名即 <title>、副标题即 meta description。
			// 若这里仍读 seo_title/seo_description，评分看到的标题会和实际发布出去的**不是同一个**——
			// 编辑改商品名、分数不动，是最难解释的一种不一致。
			SEOTitle:       detail.Name,
			SEODescription: detail.Subtitle,
			Slug:           detail.Slug,
			URL:            firstNonEmptyString(instPath, slashSlug(detail.Slug)),
			Images:         productPageImages(detail),
			SpecNames:      variationSpecNames(detail),
			ChildNames:     productCategoryNames(detail),
			Locale:         requestScoreLang(c),
			// 未发布时判假：评分器只该显示编辑者能改的东西，而这两项要发布一次才会出现。
			HasCanonical: instPath != "",
			HasSchema:    instPath != "",
		}, detail.ID, scoreView{}
	}
}

// EntitySeoDrawer 商品 SEO 评分抽屉片段（GET /admin/products/seo/drawer?productId=&project=）。
//
// 为什么只有商品走抽屉：抽屉基座（ui/drawer.js）是**单例** —— 打开新抽屉会替换当前那个。
// 而分类 / 品牌的评分按钮活在**行内编辑抽屉**里（列表页的编辑模板 include 了表单片段），
// 把那里改成跳转式抽屉，用户在分类编辑到一半点「SEO 评分」，正在填的表单会被顶掉且无路返回。
// 那两处保持原有的「htmx 就地展开分数」形态 —— 它们本来就不占常驻版面，没有要修的问题。
//
// 商品编辑页是**独立整页**，不存在这个冲突，且它原来有一张常驻折叠卡占着版面底部。
//
// 打开即算：抽屉本来就是「我要看分」这个动作的落点，不该再要用户先点一次「开始评分」
// （行内按钮需要那一步，是因为它没有「打开」这个动作）。
func (h *productPageHandle) EntitySeoDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("productId"))
	projectID := strings.TrimSpace(c.Query("project"))
	if h == nil || h.products == nil {
		c.Status(http.StatusNotFound)
		return
	}
	data := gin.H{
		"ScoreURL":     "/admin/products/seo-score",
		"TargetID":     "product-seo-score-" + id,
		"HiddenFields": productScoreHiddenFields(id, projectID),
	}
	in, selfID, emptyView := h.entityScoreInputOf(c.Request.Context(), c, scoreKindProduct, id, projectID)
	if in == nil {
		// 读不到实体：仍给 200 + 片段（里面就是那句空态文案）—— 这不是「加载失败」，
		// 抽屉的失败重试界面会把「这个商品读不到」伪装成网络问题。
		data["Score"] = emptyView
		c.HTML(http.StatusOK, "admin/product/entity_seo_drawer.html", shell.Prepare(c, data))
		return
	}
	data["Score"] = h.entityScoreViewOf(c.Request.Context(), c, in, projectID, selfID)
	c.HTML(http.StatusOK, "admin/product/entity_seo_drawer.html", shell.Prepare(c, data))
}

// productScoreHiddenFields 商品评分端点的隐藏字段（与页面上那个 POST 端点同一口径）。
func productScoreHiddenFields(id, projectID string) []seoHiddenField {
	return []seoHiddenField{{"projectId", projectID}, {"productId", id}}
}

// seoHiddenField 抽屉里「评分」表单的一个隐藏字段。
type seoHiddenField struct{ Name, Value string }

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
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productTagsBackKeys...),
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
		"ID":        "", "Name": "", "Slug": "", "IsRule": false, "Sort": 0,
		"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
		// ListQuery：表单 action 的筛选上下文（失败回跳时 shell.BackPath 读回）。
		"ListQuery": productQueryFromRequest(c, productTagsBackKeys...),
	}
	if row != nil {
		for _, key := range []string{"ID", "Name", "Slug", "IsRule", "Sort", "RuleType", "RuleDays", "RuleMinPrice", "RuleMaxPrice"} {
			data[key] = row[key]
		}
	}
	return data
}

// tagFormFail 写失败分档：htmx 请求 200 + 片段自身（错误槽 + 原值回填），
// 原生提交渲染整页提示（取代原先的 302 + ?err= 回本页）—— 用户输入比错误文案贵，
// 两种档都不丢字段。
func (h *productPageHandle) tagFormFail(c *gin.Context, mode string, err error) {
	projectID := c.PostForm("projectId")
	msg := productErrText(c, err)
	if !isHXRequest(c) {
		productTagsJump(c, false, msg)
		return
	}
	data := h.tagDrawerData(c, mode, projectID, nil, h.products.ListTagRuleTypes(c.Request.Context()))
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "kind", "sort", "ruleType", "days", "minPrice", "maxPrice"})
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_tag_form.html", data)
}

// tagFormSuccess 写成功出口：整页提示（htmx 由 shell.RenderJump 输出 HX-Redirect）。
func tagFormSuccess(c *gin.Context, projectID string) {
	productTagsJump(c, true, productActionDoneText(c))
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
		productTagsJump(c, false, productErrText(c, err))
		return
	}
	productTagsJump(c, true, productBulkDoneText(c, productTagBulkDone))
}

// ProductTagsBulkDelete 批量删除标签（手工 / 自动一视同仁：删除即解绑商品上的该标签）。
//
// 逐条走同一条删除路径：失败的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带列表页，避免静默的部分成功。
func (h *productPageHandle) ProductTagsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经归口助手判定来源。
		productTagsJump(c, false, shell.BulkIDsFacingText(c, berr))
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
	ok, msg := productBulkDeleteResult(c, deleted, skipped, productTagBulkPartial, productTagBulkDone)
	productTagsJump(c, ok, msg)
}

// ProductTagsRecalc 手动重算（tagId 为空即重算该工程全部自动标签）。
func (h *productPageHandle) ProductTagsRecalc(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &productdto.RecalcTagsReq{
		ProjectID: projectID,
		TagID:     strings.TrimSpace(c.PostForm("tagId")),
	}
	if _, err := h.products.RecalcTags(c.Request.Context(), req); err != nil {
		productTagsJump(c, false, productErrText(c, err))
		return
	}
	productTagsJump(c, true, productActionDoneText(c))
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
		productEditJump(c, false, projectID, req.ID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ID, productActionDoneText(c))
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

// ProductCategoriesPage 分类管理页：工程切换 + 筛选栏 + 分类树 + 内联新建表单。
func (h *productPageHandle) ProductCategoriesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_taxonomy", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	if selected != "" {
		found := false
		for _, project := range projects {
			if project.ID == selected {
				found = true
				break
			}
		}
		if !found {
			shell.PageError(c, "product_taxonomy", fmt.Errorf("unknown project"))
			return
		}
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	page := productPageNumber(c.Query("page"))
	pageRows := []*productdto.CategoryResp{}
	pickFlat := []*productdto.CategoryResp{}
	total, matchTotal := int64(0), int64(0)
	if selected != "" {
		query := &productdto.ListCategoryPageReq{ProjectID: selected, Keyword: keyword, Page: page, Size: productSubListPageSize}
		result, lerr := h.products.ListCategoryPage(ctx, query)
		if lerr != nil {
			shell.PageError(c, "product_taxonomy", lerr)
			return
		}
		total, matchTotal = result.Total, result.MatchTotal
		page = clampPageToTotal(page, productSubListPageSize, total)
		if page != query.Page {
			query.Page = page
			result, lerr = h.products.ListCategoryPage(ctx, query)
			if lerr != nil {
				shell.PageError(c, "product_taxonomy", lerr)
				return
			}
		}
		// 一页的树 → DFS 扁平行（父在前、子紧随，模板按 Depth 缩进）。
		pageRows = flattenCategoryTree(result.Items)
		// 父级下拉要的是**工程内全部分类**，不是当前这一页 ——
		// 树状列表之后没有面包屑兜底，缺项会直接表现为「新建子分类时选不到父级」。
		all, aerr := h.products.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: selected})
		if aerr != nil {
			shell.PageError(c, "product_taxonomy", aerr)
			return
		}
		pickFlat = flattenCategoryTree(all)
	}
	// 候选列表**只收本工程**的分类：ListCategories 的跨工程可见性由 RLS 决定
	//（未切非超级角色时策略不生效，别的工程的分类会一起回来），而这里的选择动作是工程内行为 ——
	// 混进来会让「父级在别的工程」这种坏数据在界面上看起来完全正常。
	options := categoryPickOptions(filterCategoriesInProject(pickFlat, selected))
	createForm := categoryDrawerData(c, "create", selected, nil, options, nil)
	filterQuery := listFilterQuery(selected, keyword)
	// 父级不在候选列表里（跨工程遗留 / 父级行已不存在）时，抽屉仍要渲染「当前选中的父级」
	// 那一项，并说明它为什么不在候选里 —— 那是操作者唯一的可见信号。
	rows := categoryTreeRows(c, selected, pageRows, options, keyword != "", h.missingParentLabels(c, selected, pageRows, options))
	// withCSRF：注入 csrf_token（POST 表单隐藏域）+ 导航树 + 权限码 + 多语言。
	data := gin.H{
		"title":           shell.TranslateFor(c)(productenums.ProductCategoriesTitle, "商品分类"),
		"menu":            "product-categories",
		"Projects":        projects,
		"SelectedProject": selected,
		"Categories":      rows,
		// 父级下拉选项：扁平列表 + 缩进标签（模板里排除自身，避免明显的自环提交）。
		"Options":    options,
		"SearchMode": keyword != "",
		// MatchTotal 是**命中条数**（提示文案用）；分页条按 Total（= 命中所属的根分类数）。
		"MatchTotal":         matchTotal,
		"CategoryCreateForm": createForm,
		// 筛选回显（GET 表单的 value）：提交后条件留在控件上，
		// 否则用户看不出「现在到底筛了什么」；Filtered 让空态能区分
		// 「筛出来是空的」与「这个工程还没有分类」。
		"FilterKeyword": keyword,
		"Filtered":      keyword != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productCategoriesBackKeys...),
	}
	// 分页条（shell 组件，服务端渲染）：基地址带当前筛选条件，翻页不丢条件。
	// 单页或空数据时 BuildPagination 返回 nil，TemplateKeys 给空 map，模板自然不渲染。
	for k, v := range shell.BuildPagination(total, page, productSubListPageSize,
		productListBaseURL("/admin/product-categories", filterQuery),
		shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/product_categories.html", shell.Prepare(c, data))
}

// ProductCategoryParents is a bounded, project-scoped parent picker search.
func (h *productPageHandle) ProductCategoryParents(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project"))
	if !h.categoryProjectExists(c, projectID) {
		c.Status(http.StatusNotFound)
		return
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	options := []gin.H{}
	if keyword != "" {
		page, err := h.products.ListCategoryPage(c.Request.Context(), &productdto.ListCategoryPageReq{
			ProjectID: projectID, Keyword: keyword, Page: productPageNumber(c.Query("page")), Size: productSubListPageSize,
		})
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		for _, item := range flattenCategoryTree(page.Items) {
			if item.Matched {
				options = append(options, gin.H{"ID": item.ID, "Label": categoryLabel(item)})
			}
		}
	}
	c.HTML(http.StatusOK, "admin/product/product_category_parent_options.html", gin.H{"Options": options, "t": shell.TranslateFor(c)})
}

func (h *productPageHandle) categoryProjectExists(c *gin.Context, projectID string) bool {
	if projectID == "" {
		return false
	}
	projects, err := h.projects.List(c.Request.Context())
	if err != nil {
		return false
	}
	for _, project := range projects {
		if project.ID == projectID {
			return true
		}
	}
	return false
}

// categoryTreeRows 分类树 → 列表行；每行带自己的编辑抽屉数据。
//
// parentLabels 是「父级不在候选列表里」的分类 → 父级名称（见 missingParentLabels），
// 只用于抽屉里那一项的回显。传 nil 表示没有这类行（新建表单就走这条路）。
//
// ParentOutOfScope 是列表行上的徽章：父级属于别的工程（或父级行已不存在）时置真 ——
// 这类行的 ParentID 指向本工程之外，操作者只看分类名看不出来，得有个可见标记。
func categoryTreeRows(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []gin.H, searching bool, parentLabels map[string]string) []gin.H {
	// 判据取 missingParentIDs（父级不在候选列表）而**不是** parentLabels：后者只收
	// 「名称读得到」的那些父级，RLS 切非超级角色后跨工程父级读不到名称会被它漏掉，
	// 而那时恰恰最该让操作者看见异常。两者共用同一个判定函数，不另起一份候选集合逻辑。
	outOfScope := make(map[string]struct{})
	for _, parentID := range missingParentIDs(nodes, options) {
		outOfScope[parentID] = struct{}{}
	}
	rows := make([]gin.H, 0, len(nodes))
	for _, node := range nodes {
		_, flagged := outOfScope[node.ParentID]
		rows = append(rows, gin.H{
			"ID": node.ID, "Name": node.Name, "Slug": node.Slug, "Label": node.Name,
			"ParentID": node.ParentID, "Sort": node.Sort, "Depth": node.Depth,
			"HasChildren":      node.HasChildren,
			"Matched":          node.Matched,
			"SearchMode":       searching,
			"ParentOutOfScope": flagged,
			"EditForm":         categoryDrawerData(c, "update", projectID, node, options, parentLabels),
		})
	}
	return rows
}

// ProductCategoriesCreate 新建分类。
func (h *productPageHandle) ProductCategoriesCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// SEO 标题 / 描述不再从表单读（2026-09-30 字段合并）：分类名即网页标题、
	// 分类描述即 meta description，读侧统一映射（service 的 categoryValues、
	// SEO 评分、构建期 SEO 头）。列保留但不再是任何 UI 的来源。
	req := &productdto.CreateCategoryReq{
		ProjectID:   projectID,
		ParentID:    strings.TrimSpace(c.PostForm("parentId")),
		Name:        strings.TrimSpace(c.PostForm("name")),
		Slug:        strings.TrimSpace(c.PostForm("slug")),
		Description: c.PostForm("description"),
		Image:       strings.TrimSpace(c.PostForm("image")),
		Sort:        parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.products.CreateCategory(c.Request.Context(), req); err != nil {
		h.categoryFormFail(c, "create", productErrText(c, err))
		return
	}
	categoryFormSuccess(c, projectID)
}

// ProductCategoriesUpdate 修改分类（改名 / 换父级 / 排序 / 描述与图）。
func (h *productPageHandle) ProductCategoriesUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	parentID := strings.TrimSpace(c.PostForm("parentId"))
	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.TrimSpace(c.PostForm("slug"))
	description := c.PostForm("description")
	image := strings.TrimSpace(c.PostForm("image"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	// SEO 标题 / 描述**不提交**（req 里保持 nil = 不改）：库里可能有编辑者写过的
	// 历史值，不动它就不会丢数据；而它们已经不是读侧口径。
	req := &productdto.UpdateCategoryReq{
		ProjectID: projectID,
		ID:        c.PostForm("id"), ParentID: &parentID, Name: &name, Slug: &slug,
		Description: &description, Image: &image, Sort: &sortValue,
	}
	if _, err := h.products.UpdateCategory(c.Request.Context(), req); err != nil {
		h.categoryFormFail(c, "update", productErrText(c, err))
		return
	}
	categoryFormSuccess(c, projectID)
}

// ProductCategoriesDelete 删除分类（有子级或被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductCategoriesDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteCategory(c.Request.Context(), &productdto.DeleteCategoryReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		productCategoriesJump(c, false, productErrText(c, err))
		return
	}
	productCategoriesJump(c, true, productBulkDoneText(c, productCategoryBulkDone))
}

// ProductCategoriesBulkDelete 批量删除分类。
//
// 逐条走同一条删除路径：有子分类或被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染提示页，避免静默的部分成功。
func (h *productPageHandle) ProductCategoriesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productCategoriesJump(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteCategory(c.Request.Context(), &productdto.DeleteCategoryReq{ProjectID: projectID, ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(c, deleted, skipped, productCategoryBulkPartial, productCategoryBulkDone)
	productCategoriesJump(c, ok, msg)
}

// ProductBrandsPage 品牌管理页：工程切换 + 筛选栏 + 品牌列表 + 内联新建表单。
func (h *productPageHandle) ProductBrandsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "product_taxonomy", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	// 品牌列表**分页下推到 service**（审计 D12 收口）：请求类型自带 Page/Size，总数由契约的
	// CountBrands 给出 —— handler 不再「全量取回再切片」，翻到第 N 页也只从库里取那一页。
	// 关键词过滤仍在查询里（与计数同一份过滤条件）。
	//
	// 顺序是**先计数再取页**：反过来（先取第 N 页再数总数）时越界页码会让 service 返回空页，
	// 而分页条按收敛后的页码渲染 —— 「表格为空、分页条却显示第 2 页」正是
	// product_list_paging_test.go 要挡的那种自相矛盾组合。两次查询的条数一样，不额外付代价。
	page := productPageNumber(c.Query("page"))
	total := int64(0)
	pageRows := []*productdto.BrandResp{}
	if selected != "" {
		// 过滤条件只构造一次：计数与列表各自复制、只给列表那份填 Page/Size，
		// 两处口径分叉（关键词只归一在一侧）在这里是不可能的 —— 与属性页同一手法。
		filterReq := &productdto.ListBrandReq{ProjectID: selected, Keyword: keyword}
		n, cerr := h.products.CountBrands(ctx, filterReq)
		if cerr != nil {
			shell.PageError(c, "product_taxonomy", cerr)
			return
		}
		total = n
		page = clampPageToTotal(page, productSubListPageSize, total)
		listReq := *filterReq
		listReq.Page, listReq.Size = page, productSubListPageSize
		list, lerr := h.products.ListBrands(ctx, &listReq)
		if lerr != nil {
			shell.PageError(c, "product_taxonomy", lerr)
			return
		}
		pageRows = list
	}
	brands := make([]gin.H, 0, len(pageRows))
	for _, b := range pageRows {
		brands = append(brands, gin.H{
			"ID": b.ID, "Name": b.Name, "Slug": b.Slug, "Logo": b.Logo,
			"Sort": b.Sort, "Description": b.Description,
			"UpdatedAt": b.UpdatedAt,
			"EditForm":  brandDrawerData(c, "update", selected, b),
		})
	}
	data := gin.H{
		"title":           shell.TranslateFor(c)(productenums.ProductBrandsTitle, "商品品牌"),
		"menu":            "product-brands",
		"Projects":        projects,
		"SelectedProject": selected,
		"Brands":          brands,
		"BrandCreateForm": brandDrawerData(c, "create", selected, nil),
		"FilterKeyword":   keyword,
		"Filtered":        keyword != "",
		// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 product_jump.go）；
		// ListQuery 是筛选上下文（表单 action 的 query）：写动作失败时由 shell.BackPath 读回。
		"ListQuery": productQueryFromRequest(c, productBrandsBackKeys...),
	}
	for k, v := range shell.BuildPagination(total, page, productSubListPageSize,
		productListBaseURL("/admin/product-brands", listFilterQuery(selected, keyword)),
		shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/product/product_brands.html", shell.Prepare(c, data))
}

// ProductBrandsCreate 新建品牌。
func (h *productPageHandle) ProductBrandsCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// SEO 标题 / 描述不再从表单读（2026-09-30 字段合并）：品牌名即网页标题、
	// 品牌描述即 meta description（读侧统一映射，见 service 的 brandValues）。
	req := &productdto.CreateBrandReq{
		ProjectID:   projectID,
		Name:        strings.TrimSpace(c.PostForm("name")),
		Slug:        strings.TrimSpace(c.PostForm("slug")),
		Logo:        strings.TrimSpace(c.PostForm("logo")),
		Description: c.PostForm("description"),
		Sort:        parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.products.CreateBrand(c.Request.Context(), req); err != nil {
		h.brandFormFail(c, "create", productErrText(c, err))
		return
	}
	brandFormSuccess(c, projectID)
}

// ProductBrandsUpdate 修改品牌。
func (h *productPageHandle) ProductBrandsUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	name := strings.TrimSpace(c.PostForm("name"))
	slug := strings.TrimSpace(c.PostForm("slug"))
	logo := strings.TrimSpace(c.PostForm("logo"))
	description := c.PostForm("description")
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	// SEO 标题 / 描述**不提交**（nil = 不改），理由同分类。
	req := &productdto.UpdateBrandReq{
		ProjectID: projectID,
		ID:        c.PostForm("id"), Name: &name, Slug: &slug, Logo: &logo,
		Description: &description, Sort: &sortValue,
	}
	if _, err := h.products.UpdateBrand(c.Request.Context(), req); err != nil {
		h.brandFormFail(c, "update", productErrText(c, err))
		return
	}
	brandFormSuccess(c, projectID)
}

// ProductBrandsDelete 删除品牌（被商品引用时服务端拒绝）。
func (h *productPageHandle) ProductBrandsDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.products.DeleteBrand(c.Request.Context(), &productdto.DeleteBrandReq{ProjectID: projectID, ID: c.PostForm("id")}); err != nil {
		productBrandsJump(c, false, productErrText(c, err))
		return
	}
	productBrandsJump(c, true, productBulkDoneText(c, productBrandBulkDone))
}

// ProductBrandsBulkDelete 批量删除品牌。
//
// 逐条走同一条删除路径：被商品引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染提示页，避免静默的部分成功。
func (h *productPageHandle) ProductBrandsBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		productBrandsJump(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.products.DeleteBrand(c.Request.Context(), &productdto.DeleteBrandReq{ProjectID: projectID, ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	ok, msg := productBulkDeleteResult(c, deleted, skipped, productBrandBulkPartial, productBrandBulkDone)
	productBrandsJump(c, ok, msg)
}

// ProductsTaxonomySet 整体替换某商品挂的分类与品牌（issue #10）。
//
// 分类勾选框一个都没勾时浏览器不发该字段，而「一个都不勾」在这里是明确的
// 「解绑全部分类」，故把 nil 归一成空切片 —— 与其它表单的「整体替换」语义一致。
// 主分类由 select 提交（空值 = 不指定）；它在列表里时服务端自动纳入附属分类。
func (h *productPageHandle) ProductsTaxonomySet(c *gin.Context) {
	projectID := c.PostForm("projectId")
	categoryIDs := c.PostFormArray("categoryIds")
	if categoryIDs == nil {
		categoryIDs = []string{}
	}
	primary := strings.TrimSpace(c.PostForm("primaryCategoryId"))
	brand := strings.TrimSpace(c.PostForm("brandId"))
	req := &productdto.UpdateReq{
		ProjectID:         projectID,
		ID:                formProductID(c),
		CategoryIDs:       categoryIDs,
		PrimaryCategoryID: &primary,
		BrandID:           &brand,
	}
	if _, err := h.products.Update(c.Request.Context(), req); err != nil {
		productEditJump(c, false, projectID, req.ID, productErrText(c, err))
		return
	}
	productEditJump(c, true, projectID, req.ID, productActionDoneText(c))
}

// flatCategories 取某工程的分类树并摊平成 DFS 前序列表（工程为空时返回空列表）。
func (h *productPageHandle) flatCategories(ctx context.Context, projectID string) (out []*productdto.CategoryResp, err error) {
	out = []*productdto.CategoryResp{}
	if projectID == "" {
		return out, nil
	}
	tree, err := h.products.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return flattenCategoryTree(tree), nil
}

// listBrands 取某工程的品牌列表。
func (h *productPageHandle) listBrands(ctx context.Context, projectID string) (out []*productdto.BrandResp, err error) {
	if projectID == "" {
		return []*productdto.BrandResp{}, nil
	}
	return h.products.ListBrands(ctx, &productdto.ListBrandReq{ProjectID: projectID})
}

// flattenCategoryTree 分类树 → DFS 前序扁平行（父在前、子紧随，模板按 Depth 缩进）。
func flattenCategoryTree(nodes []*productdto.CategoryResp) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(nodes))
	var walk func(list []*productdto.CategoryResp)
	walk = func(list []*productdto.CategoryResp) {
		for _, n := range list {
			out = append(out, n)
			walk(n.Children)
		}
	}
	walk(nodes)
	return out
}

// categoryLabel 分类的展示标签（按层级缩进；全角空格在 <option> 与列表里都成立）。
func categoryLabel(node *productdto.CategoryResp) string {
	if node.Depth <= 0 {
		return node.Name
	}
	return strings.Repeat("　", node.Depth) + node.Name
}

// categoryPickOptions 父级下拉选项（Selected 由模板按行的 ParentID 比对得出）。
func categoryPickOptions(flat []*productdto.CategoryResp) []gin.H {
	out := make([]gin.H, 0, len(flat))
	for _, node := range flat {
		out = append(out, gin.H{"ID": node.ID, "Label": categoryLabel(node)})
	}
	return out
}

// checkedCategoryOptions 商品页的分类勾选框（勾选态由服务端算好，模板不做集合运算）。
func checkedCategoryOptions(flat []*productdto.CategoryResp, checked []string) []gin.H {
	picked := make(map[string]bool, len(checked))
	for _, id := range checked {
		picked[id] = true
	}
	out := make([]gin.H, 0, len(flat))
	for _, node := range flat {
		out = append(out, gin.H{
			"ID": node.ID, "Label": categoryLabel(node), "Checked": picked[node.ID],
		})
	}
	return out
}

// primaryCategoryOptions 商品页的主分类下拉（含「不指定」空项）。
func primaryCategoryOptions(tr func(key, fallback string) string, flat []*productdto.CategoryResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(flat)+1)
	out = append(out, gin.H{"ID": "", "Label": tr(productenums.ProductsOptionNoPrimaryCategory, "（不指定主分类）"), "Selected": selected == ""})
	for _, node := range flat {
		out = append(out, gin.H{
			"ID": node.ID, "Label": categoryLabel(node), "Selected": node.ID == selected,
		})
	}
	return out
}

// categoryNameByID 按 id 取分类名（找不到返回空串，不让悬空引用把页面打崩）。
func categoryNameByID(flat []*productdto.CategoryResp, id string) string {
	for _, node := range flat {
		if node.ID == id {
			return node.Name
		}
	}
	return ""
}

// brandNameByID 按 id 取品牌名（找不到返回空串）。
func brandNameByID(brands []*productdto.BrandResp, id string) string {
	for _, b := range brands {
		if b != nil && b.ID == id {
			return b.Name
		}
	}
	return ""
}

// brandPickOptions 商品页的品牌下拉（含「不指定」空项）。
func brandPickOptions(tr func(key, fallback string) string, brands []*productdto.BrandResp, selected string) []gin.H {
	out := make([]gin.H, 0, len(brands)+1)
	out = append(out, gin.H{"ID": "", "Label": tr(productenums.ProductsOptionNoBrand, "（不指定品牌）"), "Selected": selected == ""})
	for _, b := range brands {
		if b == nil {
			continue
		}
		out = append(out, gin.H{"ID": b.ID, "Label": b.Name, "Selected": b.ID == selected})
	}
	return out
}

// ---------------------------------------------------------------------------
// 商品域后台子列表（属性 / 分类 / 品牌 / 标签）共用的分页与取址助手。
//
// 为什么落在这个文件里：本批的独占文件清单只含三个 handler 与四个模板，共享助手所在的
// product_page_util.go 不在其中（有并行任务在同包改别的页面，动它必然冲突）；同包内位置
// 不影响可用性，四页都能直接调用。
// ---------------------------------------------------------------------------

// productSubListPageSize 商品域后台子列表的每页条数。
//
// 20 与商品列表（productListPageSize）同一量级：这四页的表格都带行内抽屉与批量勾选，
// 一页塞太多等于把「翻页」换成「滚动回去找刚才那一行」。
const productSubListPageSize = 20

// clampPageToTotal 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 为什么在取数**之前**收敛：越界页码（手输 URL、书签失效、上一次筛选后的页码）传给
// 取数层时，service 会老老实实返回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样产生的
// （product_list_paging_test.go 的 TestListPageSlice 钉的是同一条判据）。
//
// 与 shell.BuildPagination 内部的收敛同一条规则（总页数由 total 与 size 算出），
// 差别只在于这里发生在取数之前。
func clampPageToTotal(page, size int, total int64) int {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(size) - 1) / int64(size))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

// listFilterQuery 列表页筛选条件的查询串（筛选表单与分页基地址共用同一份口径）。
func listFilterQuery(projectID, keyword string) url.Values {
	q := url.Values{}
	if v := strings.TrimSpace(projectID); v != "" {
		q.Set("project", v)
	}
	if v := strings.TrimSpace(keyword); v != "" {
		q.Set("keyword", v)
	}
	return q
}

// productListBaseURL 列表页的分页基地址（**不含** page/limit：分页组件自己拼）。
//
// 把筛选条件拼进基地址，翻页时关键词才不会丢；反过来说，基地址里塞了 page 就会出现两个
// page 参数（浏览器取第一个），翻页看起来「点了没反应」—— 与 productListFilterURL
// （商品列表专用，参数固定为 keyword + status）同一条理由。
func productListBaseURL(path string, q url.Values) string {
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// listPageSlice 切出「第 page 页」，并返回**收敛后**的页码。
//
// 保留旧分页助手供同包既有测试使用；分类页现走受限分类读。
//
// 页码收敛与 BuildPagination 同一条规则：page=999 时若不先收敛，会出现「表格为空、
// 分页条却显示第 999 页」这种自相矛盾的组合（BuildPagination 拿到的 total 与 page
// 不同源时就会这样）。
func listPageSlice[T any](all []T, page, size int) (rows []T, current int) {
	if size < 1 {
		size = productSubListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := (len(all) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	from := (page - 1) * size
	if from > len(all) {
		from = len(all)
	}
	to := from + size
	if to > len(all) {
		to = len(all)
	}
	return all[from:to], page
}

// categoryParentOutOfScopeKey 「上级不在本工程」的文案 key。
//
// 一处定义、两处消费：抽屉里拼在父级名称后面（categoryParentText），列表行上的徽章
// （product_categories.html）用同一个 key 取词 —— 两处是同一句话，不能各写一份常量。
// 词条门禁（scripts/check-i18n-keys-seeded.sh）要求「用了 key 就必须同批 seed」，
// 迁移 508 已补中英成对；基线只降不升，所以 key 与词条必须同批落地。
const categoryParentOutOfScopeKey = "admin.product_categories.parent.out_of_scope"

// categoryParentOutOfScopeFallback key 未命中时的兜底原文（与迁移 508 的 zh-CN 值一致）。
const categoryParentOutOfScopeFallback = "（不属于本工程）"

// filterCategoriesInProject 只保留属于 projectID 的分类。
//
// 判据是行自己的 ProjectID，**不依赖 RLS**：策略未切非超级角色时 ListCategories 会把别的
// 工程的分类一起返回，混进「本工程的父级候选」里 —— 分类是工程内实体，不该串门。
func filterCategoriesInProject(flat []*productdto.CategoryResp, projectID string) []*productdto.CategoryResp {
	out := make([]*productdto.CategoryResp, 0, len(flat))
	for _, node := range flat {
		if node.ProjectID == projectID {
			out = append(out, node)
		}
	}
	return out
}

// missingParentIDs 挑出本页里「父级不在候选列表」的那些父级 id（去重、保持出现顺序）。
//
// 纯逻辑部分单独成函数：候选集合的判定（不是 service 读）才是「谁会走模板那条额外分支」
// 的判据，单测在没有数据库时也能把它钉住。
func missingParentIDs(nodes []*productdto.CategoryResp, options []gin.H) []string {
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		if id, ok := option["ID"].(string); ok {
			known[id] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(nodes))
	var ids []string
	for _, node := range nodes {
		parentID := strings.TrimSpace(node.ParentID)
		if parentID == "" {
			continue
		}
		if _, ok := known[parentID]; ok {
			continue
		}
		if _, ok := seen[parentID]; ok {
			continue
		}
		seen[parentID] = struct{}{}
		ids = append(ids, parentID)
	}
	return ids
}

// missingParentLabels 为「父级不在候选列表里」的分类补上父级文案（id → 文案）。
//
// 候选列表是**本工程**分类的全集（见 filterCategoriesInProject），父级不在其中的情形有两种：
// 父级行已不存在，或父级属于别的工程（历史搬迁留下的坏数据）。两种都要让操作者看出来 ——
// 否则下拉里要么是一串裸 ID、要么看起来像个正常选项，用户以为层级没问题。
//
// 文案形态：本工程的父级只给名称；跨工程的补一句说明（categoryParentOutOfScopeKey）。
// 名称都读不到（父级行没了 / RLS 切角色后跨工程行不可见）时留空，模板退回显示原始 ID ——
// 显示裸 ID 也比编一个父级名安全。逐个父级查一次库：只对本页里真正缺失的那几个发生。
func (h *productPageHandle) missingParentLabels(c *gin.Context, projectID string, nodes []*productdto.CategoryResp, options []gin.H) map[string]string {
	if h.products == nil {
		return nil
	}
	ids := missingParentIDs(nodes, options)
	if len(ids) == 0 {
		return nil
	}
	labels := make(map[string]string, len(ids))
	for _, parentID := range ids {
		parent, err := h.products.GetCategory(c.Request.Context(), &productdto.GetCategoryReq{ProjectID: projectID, ID: parentID})
		if err != nil || parent == nil {
			continue
		}
		labels[parentID] = categoryParentText(c, parent, projectID)
	}
	return labels
}

// categoryParentText 父级那一项的文案：属本工程只给名称，跨工程补一句说明。
//
// 说明文字走 i18n（categoryParentOutOfScopeKey）：这条文案在抽屉下拉与列表行徽章上
// 各出现一次，en-US 后台下两处都该是英文。
func categoryParentText(c *gin.Context, parent *productdto.CategoryResp, projectID string) string {
	if parent.ProjectID == projectID {
		return parent.Name
	}
	return parent.Name + shell.TranslateFor(c)(categoryParentOutOfScopeKey, categoryParentOutOfScopeFallback)
}

func categoryDrawerData(c *gin.Context, mode, projectID string, row *productdto.CategoryResp, options []gin.H, parentLabels map[string]string) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Options": options, "Csrf": shell.Prepare(c, gin.H{})["csrf_token"], "t": shell.TranslateFor(c),
		// ListQuery：表单 action 的筛选上下文（失败回跳时 shell.BackPath 读回）。
		"ListQuery": productQueryFromRequest(c, productCategoriesBackKeys...)}
	if row != nil {
		data["ID"], data["Name"], data["Slug"], data["ParentID"] = row.ID, row.Name, row.Slug, row.ParentID
		if row.ParentID != "" {
			parentMissing := true
			for _, option := range options {
				if option["ID"] == row.ParentID {
					parentMissing = false
					break
				}
			}
			data["ParentMissing"] = parentMissing
			// 父级在候选里时模板走 range 那一项；缺失时模板额外渲染一行，
			// 它的文案**必须**是这个键（模板写 isset(.ParentLabel) 才用，否则退回裸 ID）。
			//
			// 可达性（2026-10 实测，别按「死代码」删）：修「孤儿分类丢行」之前这条分支**不可达** ——
			// 悬空 parent_id 的分类既不是根（根条件是 parent_id IS NULL）也不在任何可见父节点的子树里，
			// 整行不进列表；跨工程父级则被 ListCategories 一起带进候选，于是 ParentMissing 恒为 false。
			// 现在两侧都收口了：根集合兜底读孤儿（model 的 categoryRootFilter）、候选只收本工程分类
			// （filterCategoriesInProject）—— 分支因此真正走到，文案里还带上「不属于本工程」。
			//
			// 保留理由：RLS 切到非超级角色后（docs/rls-role-cutover.md 的顺序），GetCategory 会因
			// 作用域读不到跨工程父级 → 名称取不到 → 这里退回模板的「原始 ID」出口。那是兜底，
			// 不是新功能；不要因为「本地构造不出名称」就把整段删掉。
			if parentMissing {
				if label := strings.TrimSpace(parentLabels[row.ParentID]); label != "" {
					data["ParentLabel"] = label
				}
			}
		}
		data["Sort"], data["Image"], data["Description"] = row.Sort, row.Image, row.Description
		data["SEOTitle"], data["SEODescription"] = row.SEOTitle, row.SEODescription
	} else {
		data["ID"], data["Name"], data["Slug"], data["ParentID"] = "", "", "", ""
		data["Sort"], data["Image"], data["Description"] = 0, "", ""
		data["SEOTitle"], data["SEODescription"] = "", ""
	}
	return data
}

func (h *productPageHandle) categoryFormFail(c *gin.Context, mode, msg string) {
	if !isHXRequest(c) {
		productCategoriesJump(c, false, msg)
		return
	}
	data := categoryDrawerData(c, mode, c.PostForm("projectId"), nil, nil, nil)
	if h.products != nil {
		projectID := c.PostForm("projectId")
		if page, err := h.products.ListCategoryPage(c.Request.Context(), &productdto.ListCategoryPageReq{ProjectID: projectID, Page: 1, Size: 100}); err == nil {
			// 列表读的是树，父级下拉要的是扁平行 —— 不摊平的话下拉里只剩顶级分类。
			options := flattenCategoryTree(page.Items)
			parentID := strings.TrimSpace(c.PostForm("parentId"))
			if parentID != "" {
				if parent, perr := h.products.GetCategory(c.Request.Context(), &productdto.GetCategoryReq{ProjectID: projectID, ID: parentID}); perr == nil {
					options = append(options, parent)
				}
			}
			data["Options"] = categoryPickOptions(options)
		}
	}
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "parentId", "sort", "image", "description", "seoTitle", "seoDescription"})
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_category_form.html", data)
}

func categoryFormSuccess(c *gin.Context, projectID string) {
	productCategoriesJump(c, true, productActionDoneText(c))
}

func brandDrawerData(c *gin.Context, mode, projectID string, row *productdto.BrandResp) gin.H {
	data := gin.H{"Mode": mode, "Project": projectID, "Csrf": shell.Prepare(c, gin.H{})["csrf_token"], "t": shell.TranslateFor(c),
		// ListQuery：表单 action 的筛选上下文（失败回跳时 shell.BackPath 读回）。
		"ListQuery": productQueryFromRequest(c, productBrandsBackKeys...)}
	if row != nil {
		data["ID"], data["Name"], data["Slug"], data["Logo"] = row.ID, row.Name, row.Slug, row.Logo
		data["Sort"], data["Description"], data["SEOTitle"], data["SEODescription"] = row.Sort, row.Description, row.SEOTitle, row.SEODescription
	} else {
		data["ID"], data["Name"], data["Slug"], data["Logo"] = "", "", "", ""
		data["Sort"], data["Description"], data["SEOTitle"], data["SEODescription"] = 0, "", "", ""
	}
	return data
}

func (h *productPageHandle) brandFormFail(c *gin.Context, mode, msg string) {
	if !isHXRequest(c) {
		productBrandsJump(c, false, msg)
		return
	}
	data := brandDrawerData(c, mode, c.PostForm("projectId"), nil)
	data["FormEcho"] = rawDrawerEcho(c, []string{"projectId", "id", "name", "slug", "sort", "logo", "description", "seoTitle", "seoDescription"})
	data["SubmitErr"] = msg
	c.HTML(http.StatusOK, "admin/product/product_brand_form.html", data)
}

func brandFormSuccess(c *gin.Context, projectID string) {
	productBrandsJump(c, true, productActionDoneText(c))
}

func rawDrawerEcho(c *gin.Context, fields []string) gin.H {
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	out := gin.H{}
	for _, key := range fields {
		out[key] = ""
		if values := c.Request.PostForm[key]; len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

// productTranslationPort 商品翻译工作台使用的译文读写端口。
//
// 生产实现 = pkg/i18n.ContentWriter（sys_translation 表 + 默认数据库）。
//
// 工程作用域（审计 I18N-009）：商品译文与页面译文同一张表的同一套隔离规则，
// 工作台按本工程读写 —— 工作台本来就是按工程选商品的（project 查询参数）。
type productTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

// ProductTranslationInstancePort 商品译文变更后需要失效的自动发布实例端口。
//
// 消费者侧最窄接口：编排层把 presentation 契约实现传进来即可（跨模块只依赖契约）。
type ProductTranslationInstancePort interface {
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
}

// productTranslationHandle 商品域翻译页处理器。
type productTranslationHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
	// instances 自动发布实例失效端口（可空：为空时只做 page 侧标记）。
	instances ProductTranslationInstancePort
	// writer 译文读写端口（为 nil 时按默认库惰性构造；测试注入隔离 schema 的写入器）。
	writer productTranslationPort
}

// NewProductTranslationHandle 构造商品域翻译页处理器（instances 可为 nil）。
func NewProductTranslationHandle(products productcontract.ProductService, projects projectcontract.ProjectService,
	pages pagecontract.PageService, instances ProductTranslationInstancePort) *productTranslationHandle {
	return &productTranslationHandle{products: products, projects: projects, pages: pages, instances: instances}
}

// SetContentTranslationStore 注入译文读写端口（测试用；生产走默认库）。
func (h *productTranslationHandle) SetContentTranslationStore(port productTranslationPort) {
	h.writer = port
}

// port 返回译文读写端口（未注入时用默认库）。
func (h *productTranslationHandle) port() (productTranslationPort, error) {
	if h.writer != nil {
		return h.writer, nil
	}
	return i18n.NewContentWriterDefault()
}

// ProductTranslations GET /admin/products/translations：商品域翻译工作台。
func (h *productTranslationHandle) ProductTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.Query("project"))
	productID := strings.TrimSpace(c.Query("product"))
	lang := strings.TrimSpace(c.Query("lang"))

	data, err := h.build(ctx, projectID, productID, lang, shell.TranslateFor(c))
	if err != nil {
		logger.Scene("product").With("project", projectID).With("product", productID).
			Error(err, "打开商品翻译工作台失败")
		productTranslationsJump(c, false, shell.PageInternalText(c))
		return
	}
	// 保存成功的回执不再经 ?saved=1&n=N 回带（那条读侧已整批删除）：结论由提示页渲染。
	c.HTML(http.StatusOK, "admin/product/product_translations.html", shell.Prepare(c, data.templateMap()))
}

// SaveProductTranslations POST /admin/products/translations/save：保存译文（整表提交）。
func (h *productTranslationHandle) SaveProductTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("project"))
	productID := strings.TrimSpace(c.PostForm("product"))
	lang := strings.TrimSpace(c.PostForm("lang"))

	data, err := h.build(ctx, projectID, productID, lang, shell.TranslateFor(c))
	if err != nil {
		logger.Scene("product").With("project", projectID).Error(err, "商品翻译工作台保存前重建数据失败")
		productTranslationsJump(c, false, shell.PageInternalText(c))
		return
	}
	if !h.langAllowed(ctx, data.ProjectID, lang) {
		data.Errors = translationMsgs(c, []string{MsgTranslationLangInvalid})
		c.HTML(http.StatusOK, "admin/product/product_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = translationMsgs(c, []string{MsgTranslationInvalid})
		c.HTML(http.StatusOK, "admin/product/product_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, contextName+"："+translationMsg(c, MsgTranslationStale))
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue // 空输入 = 本行不写入（不删除库中已有译文）
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if verr := validateProductTarget(contextName, source, target); verr != "" {
			rowErrors = append(rowErrors, contextName+"："+verr)
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			// 工程作用域（审计 I18N-009）：写入本工程自己的译文行。
			ProjectID:  projectID,
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.SourceText, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		writeKeys = append(writeKeys, key)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/product/product_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		productTranslationsJump(c, true, productTranslationsSavedText(c, 0))
		return
	}

	port, perr := h.port()
	if perr != nil {
		logger.Scene("product").With("project", projectID).Error(perr, "内容译文存储不可用")
		data.Errors = translationMsgs(c, []string{MsgTranslationSaveFailed})
		c.HTML(http.StatusOK, "admin/product/product_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	// 第二步：变更判定。只有译文文本确实变化才写库并触发重建（幂等，重复保存零写入）。
	// 变更判定按**本工程**读现有译文（工程行优先、回落全局行）。
	before, berr := port.LoadDetailsForProject(ctx, projectID, lang, productWriteHashes(items))
	if berr != nil {
		logger.Scene("product").With("project", projectID).Error(berr, "读取现有译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changedKeys := make([]string, 0, len(items))
	targetChanged := false
	for i, item := range items {
		prev := before[writeKeys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		if prev.TargetText != item.TargetText {
			targetChanged = true
		}
		pending = append(pending, item)
		changedKeys = append(changedKeys, writeKeys[i])
	}

	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = port.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("product").With("project", projectID).With("lang", lang).Error(uerr, "写入商品译文失败")
			data.Errors = translationMsgs(c, []string{MsgTranslationSaveFailed})
			c.HTML(http.StatusOK, "admin/product/product_translations.html", shell.Prepare(c, data.templateMap()))
			return
		}
	}

	// 第三步：译文变化 → 标记待重建。
	//   1) 手工页面：i18n:content 依赖条目配套的全站标记（与页面翻译工作台同一链路）；
	//   2) 自动发布实例：按 direct_content 键精确标记受影响实体（商品页面属这类）。
	if targetChanged {
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("product").With("project", projectID).Error(merr, "商品译文保存后标记页面待重建失败")
			}
		}
		h.markInstancesStale(ctx, data, changedKeys)
	}
	productTranslationsJump(c, true, productTranslationsSavedText(c, written))
}

// productTranslationsSavedText 译文保存成功的回执（0 条 = 没有需要写入的变化）。
//
// 复用工作台原有的两条词条（ProductTranslationsSavedNote / SavedNone），措辞与改造前一致；
// 只是改由提示页在响应体里渲染（取代 ?saved=1&n=N 的读侧判定）。
func productTranslationsSavedText(c *gin.Context, written int) string {
	if written > 0 {
		return i18n.FillTranslate(shell.TranslateFor(c), productenums.ProductTranslationsSavedNote,
			"已保存 {n} 条译文；译文变更已标记待重建（下次构建生效）。",
			map[string]string{"n": strconv.Itoa(written)})
	}
	return shell.TranslateFor(c)(productenums.ProductTranslationsSavedNone, "没有需要写入的变化。")
}

// markInstancesStale 按 direct_content 键标记受影响实体的自动发布实例待重建。
//
// 译文按 (原文 hash, 语境) 寻址，实体身份只在候选里；同一段文本可能来自多个实体，
// 因此按「语境 + 指纹」回查本次渲染出的候选行，命中即视为该实体受影响
// （保守超集：宁可多标记，也不漏标记 —— 与页面侧「引用块即登记依赖」同一口径）。
//
// 未注入实例端口 / 无变化 / 标记失败：只记日志，不影响保存结果（译文已落库）。
func (h *productTranslationHandle) markInstancesStale(ctx context.Context, data *productTranslationsData, changedKeys []string) {
	if h.instances == nil || data == nil || len(changedKeys) == 0 {
		return
	}
	for _, ref := range data.translationAffectedEntities(changedKeys) {
		dep := pipeline.DirectContentKey(ref.EntityType, ref.EntityID)
		if _, err := h.instances.MarkStaleByDependency(ctx, dep.Kind, dep.Key); err != nil {
			logger.Scene("product").With("entity_type", ref.EntityType).With("entity_id", ref.EntityID).
				Error(err, "商品译文保存后标记自动发布实例待重建失败")
		}
	}
}

// translationEntityRef 受影响的商品域实体（自动发布实例标记用）。
type translationEntityRef struct {
	EntityType string
	EntityID   string
}

// product_translation_data.go - 商品译文工作台的数据装配（候选分组、目标校验、哈希与工程/语言选项）。

// projectTranslationOption 工程下拉项。
type projectTranslationOption struct {
	ID       string
	Name     string
	Selected bool
}

// productTranslationRow 工作台一行（一个可翻译取值）。
type productTranslationRow struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	// Rich 富文本 / 多段文本字段（描述、属性值数组）：译文需与原文同形。
	Rich bool
}

// productTranslationGroup 按实体分组的行集合。
type productTranslationGroup struct {
	Key string
	// EntityType / EntityID 该组对应的实体（译文保存后据此精确标记自动发布实例）。
	EntityType string
	EntityID   string
	Title      string
	Subtitle   string
	Rows       []productTranslationRow
}

// productTranslationsData 工作台页面数据。
type productTranslationsData struct {
	Title     string
	Menu      string
	ProjectID string
	ProductID string
	Lang      string
	Langs     []translationLangOption
	// SourceLang 站点源语言（= 站点默认语言）；判定依据见 sourceLangOf。
	SourceLang string
	// NoTargetLang 没有任何「非源语言」的启用语言：没有可翻译的目标，页面给可行动空态。
	NoTargetLang   bool
	Groups         []productTranslationGroup
	RowCount       int
	Done           int
	Total          int
	Errors         []string
	ProjectOptions []projectTranslationOption
}

// templateMap 转为模板所需的小写键 map（layout 以 {{.title}}/{{.menu}} 取值）。
func (d *productTranslationsData) templateMap() gin.H {
	return gin.H{
		"title": d.Title, "menu": d.Menu,
		"ProjectID": d.ProjectID, "ProductID": d.ProductID,
		"Lang": d.Lang, "Langs": d.Langs, "Groups": d.Groups,
		"SourceLang": d.SourceLang, "NoTargetLang": d.NoTargetLang,
		"RowCount": d.RowCount, "Done": d.Done, "Total": d.Total,
		"Errors":         d.Errors,
		"ProjectOptions": d.ProjectOptions,
	}
}

// translationAffectedEntities 本次写入的译文对应的实体（按 (指纹, 语境) 回查候选行）。
func (d *productTranslationsData) translationAffectedEntities(changedKeys []string) []translationEntityRef {
	byCandidate := map[string][]translationEntityRef{}
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			key := i18n.ContentIndexKey(r.SourceHash, r.Context)
			byCandidate[key] = append(byCandidate[key], translationEntityRef{EntityType: g.EntityType, EntityID: g.EntityID})
		}
	}
	seen := map[string]bool{}
	out := make([]translationEntityRef, 0, len(changedKeys))
	for _, key := range changedKeys {
		for _, ref := range byCandidate[key] {
			id := ref.EntityType + "/" + ref.EntityID
			if ref.EntityID == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, ref)
		}
	}
	return out
}

// sourceCandidate 工作台校验用的一行原文。
type sourceCandidate struct {
	SourceText string
	SourceHash string
	Rich       bool
}

// sourceOf 按语境 + hash 反查本次提交对应的原文（防表单被裁剪/篡改）。
func (d *productTranslationsData) sourceOf(contextName, hash string) (sourceCandidate, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.Context == contextName && r.SourceHash == hash {
				return sourceCandidate{SourceText: r.Source, SourceHash: r.SourceHash, Rich: r.Rich}, true
			}
		}
	}
	return sourceCandidate{}, false
}

// validateProductTarget 校验一条商品域译文的可写性（与构建期同源）。
//
// 三项（全部与构建期一致）：
//  1. 语境必须是本模块契约声明的可翻译字段（"实体类型.字段名"）；
//  2. 原文参与翻译（i18n.ShouldTranslateContent，纯数字/纯符号/空白被拒）；
//  3. 富文本字段的译文形态与原文一致（是否含 HTML 标签必须相同）。
//
// 返回值是**词条 key**（不是中文句子）：它们经 translationMsg 取词后进页面的错误列表，
// 中文兜底登记在 product_page_shared.go 的 productTranslationMsgFallback 里 ——
// 写死中文的话英文工作台上永远是中文，而这几条恰好是运营最常撞上的行级结论。
func validateProductTarget(contextName string, source sourceCandidate, target string) string {
	entityType, field, ok := parseContextField(contextName)
	if !ok || !productcontract.IsTranslatableField(entityType, field) {
		return msgProductTargetContextInvalid
	}
	if !i18n.ShouldTranslateContent(source.SourceText) {
		return msgProductTargetSourceSkipped
	}
	if strings.TrimSpace(target) == "" {
		return msgProductTargetEmpty
	}
	if source.Rich && hasMarkup(source.SourceText) != hasMarkup(target) {
		return msgProductTargetShapeMismatch
	}
	return ""
}

// hasMarkup 是否含 HTML 标签（与构建期 core.HasRichMarkup 同一判据的轻量版）。
func hasMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

// parseContextField 拆语境 "实体类型.字段名"（按最后一个点拆）。
func parseContextField(contextName string) (entityType, field string, ok bool) {
	contextName = strings.TrimSpace(contextName)
	i := strings.LastIndex(contextName, ".")
	if i <= 0 || i == len(contextName)-1 {
		return "", "", false
	}
	return contextName[:i], contextName[i+1:], true
}

// 商品翻译工作台的行级校验结论：值是 i18n key（中文兜底见 productTranslationMsgFallback）。
const (
	msgProductTargetContextInvalid = "admin.product_translations.err.contextInvalid"
	msgProductTargetSourceSkipped  = "admin.product_translations.err.sourceSkipped"
	msgProductTargetEmpty          = "admin.product_translations.err.targetEmpty"
	msgProductTargetShapeMismatch  = "admin.product_translations.err.shapeMismatch"
)

// build 组装工作台数据（工程 + 商品 + 语言 + 候选 + 现有译文）。
//
// tr 由调用点给（页面标题等文案按请求语言渲染）。
func (h *productTranslationHandle) build(ctx context.Context, projectID, productID, wantLang string,
	tr func(key, fallback string) string) (data *productTranslationsData, err error) {
	data = &productTranslationsData{
		Title: tr(productenums.ProductTranslationsTitle, "商品多语言"),
		Menu:  "products", ProjectID: projectID, ProductID: productID,
	}

	options, projectIDs, oerr := h.projectOptions(ctx, projectID)
	if oerr != nil {
		return nil, oerr
	}
	data.ProjectOptions = options
	if data.ProjectID == "" && len(projectIDs) > 0 {
		data.ProjectID = projectIDs[0]
	}

	// 目标语言 = 站点启用语言剔除源语言（同语言互译没有意义）。
	// 一个都不剩时 data.Langs 为空 = NoTargetLang，页面走可行动空态；这里**不能**再
	// 退回「默认选中源语言」——那等于把 langs[0] 选中、翻了个寂寞，也看不出问题在哪。
	sourceLang := h.sourceLangOf(ctx, data.ProjectID)
	targetLangs := targetLangsOf(h.enabledLangsOf(ctx, data.ProjectID), sourceLang)
	data.SourceLang = sourceLang
	data.NoTargetLang = len(targetLangs) == 0
	data.Lang = strings.TrimSpace(wantLang)
	if !containsString(targetLangs, data.Lang) {
		data.Lang = ""
		if len(targetLangs) > 0 {
			data.Lang = targetLangs[0]
		}
	}
	data.Langs = make([]translationLangOption, 0, len(targetLangs))
	for _, code := range targetLangs {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == data.Lang})
	}

	// 商品域候选：指定商品时只列该商品的（含引用实体），否则列整个工程。
	//
	// 单商品入口也要带工程 id（DB-009 收口）：products 的 RLS 策略使非超级角色下
	// 不带作用域的读静默 0 行，表现为「点开某个商品的翻译说商品不存在」。
	// 工程 id 用 data.ProjectID，与下面整工程入口、以及本页其它取数（语言 / 译文）同源。
	var cands []productcontract.TranslationCandidate
	if productID != "" {
		if cands, err = h.products.ProductTranslationCandidates(ctx, data.ProjectID, productID); err != nil {
			return nil, err
		}
	} else if data.ProjectID != "" {
		if cands, err = h.products.ProjectTranslationCandidates(ctx, data.ProjectID); err != nil {
			return nil, err
		}
	}

	// 现有译文（含 engine，用于徽章与「是否变化」判定）。
	// 没有目标语言时不查译文：空语言码查不出任何东西，白跑一次库。
	targets := map[string]i18n.ContentTargetInfo{}
	if data.Lang != "" && len(cands) > 0 {
		if port, perr := h.port(); perr == nil {
			if got, lerr := port.LoadDetailsForProject(ctx, data.ProjectID, data.Lang, candidateHashesOf(cands)); lerr == nil {
				targets = got
			}
		}
	}

	data.Groups = groupCandidates(tr, cands, targets)
	for _, g := range data.Groups {
		for _, r := range g.Rows {
			data.Total++
			if r.Translated {
				data.Done++
			}
		}
		data.RowCount += len(g.Rows)
	}
	return data, nil
}

// groupCandidates 按实体分组并挂上现有译文。
//
// tr 由调用点传：分组标题里的实体类型名是展示文案（后台翻译页），按请求语言取词。
func groupCandidates(tr func(key, fallback string) string, cands []productcontract.TranslationCandidate, targets map[string]i18n.ContentTargetInfo) []productTranslationGroup {
	order := make([]string, 0, len(cands))
	index := map[string]*productTranslationGroup{}
	for _, c := range cands {
		key := c.EntityType + "/" + c.EntityID
		g, ok := index[key]
		if !ok {
			g = &productTranslationGroup{
				Key:        key,
				EntityType: c.EntityType,
				EntityID:   c.EntityID,
				Title:      productcontract.EntityTypeLabel(tr, c.EntityType) + " · " + c.EntityName,
				Subtitle:   c.EntityType,
			}
			index[key] = g
			order = append(order, key)
		}
		info := targets[i18n.ContentIndexKey(c.SourceHash, c.Context)]
		g.Rows = append(g.Rows, productTranslationRow{
			Context: c.Context, Field: c.Field, FieldLabel: c.FieldLabel,
			Source: c.SourceText, SourceHash: c.SourceHash,
			Target: info.TargetText, Engine: info.Engine,
			Translated: info.TargetText != "",
			Rich:       c.Field == "description",
		})
	}
	out := make([]productTranslationGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *index[key])
	}
	return out
}

// candidateHashesOf 候选的 hash 去重集合（现有译文查询用）。
func candidateHashesOf(cands []productcontract.TranslationCandidate) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if seen[c.SourceHash] {
			continue
		}
		seen[c.SourceHash] = true
		out = append(out, c.SourceHash)
	}
	sort.Strings(out)
	return out
}

// productWriteHashes 写入项的 hash 去重集合（变更判定用；与页面工作台的同名
// 辅助函数区分：本函数只服务商品域写入路径）。
func productWriteHashes(items []i18n.ContentWriteItem) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item.SourceHash] {
			continue
		}
		seen[item.SourceHash] = true
		out = append(out, item.SourceHash)
	}
	sort.Strings(out)
	return out
}

// projectOptions 工程下拉（返回选项与工程 id 顺序；未指定时回退第一个工程）。
func (h *productTranslationHandle) projectOptions(ctx context.Context, want string) (options []projectTranslationOption, ids []string, err error) {
	if h.projects == nil {
		return nil, nil, nil
	}
	list, lerr := h.projects.List(ctx)
	if lerr != nil {
		return nil, nil, lerr
	}
	selected := strings.TrimSpace(want)
	if selected == "" && len(list) > 0 {
		selected = list[0].ID
	}
	for _, p := range list {
		ids = append(ids, p.ID)
		options = append(options, projectTranslationOption{ID: p.ID, Name: p.Name, Selected: p.ID == selected})
	}
	return options, ids, nil
}

// sourceLangOf 站点源语言。
//
// 判定依据（与构建期同一来源，见 pipeline.DefaultLocale）：project_locales 里 is_default
// 的那一行；清单缺失、没有默认标记或表不可读时，project 契约自身回退全局默认语言
// （sys_config 的 i18n 组 default_lang，见 internal/module/project/service/locale_service.go
// 的 DefaultLocale），这里再加一层兜底保证源语言永远非空。
// 商品文案（商品名 / 描述 / 分类名 / 品牌名 …）存的都是源语言原文，译文只对**非源语言**
// 有意义，所以翻译目标一律从启用语言里剔掉它。
func (h *productTranslationHandle) sourceLangOf(ctx context.Context, projectID string) string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if lang, err := h.projects.DefaultLocale(ctx, projectID); err == nil && strings.TrimSpace(lang) != "" {
			return strings.TrimSpace(lang)
		}
	}
	return i18n.GetDefaultLang()
}

// targetLangsOf 启用语言剔除源语言后剩下的翻译目标（保持清单原顺序，
// 因此「默认语言在前」时第一个元素就是默认选中的目标语言）。
func targetLangsOf(langs []string, sourceLang string) []string {
	out := make([]string, 0, len(langs))
	for _, code := range langs {
		if code == sourceLang {
			continue
		}
		out = append(out, code)
	}
	return out
}

// enabledLangsOf 站点启用语言（默认语言在前；清单不可读时回退站点默认语言一种）。
func (h *productTranslationHandle) enabledLangsOf(ctx context.Context, projectID string) []string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := h.projects.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// langAllowed 目标语言是否属于站点启用语言。
//
// 判据只要求「在启用清单里」，**不额外排除源语言**：页面下拉已经不列源语言、默认目标也
// 不会落在它上面（见 build），但写入侧保留按源语言落库的兼容路径 —— 自动发布实例是逐
// 启用语言构建的（presentation.publishAllLangs），源语言那一份产物同样读 sys_translation，
// 收紧判据会让这条链路静默失效。
func (h *productTranslationHandle) langAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsString(h.enabledLangsOf(ctx, projectID), lang)
}

// warehouseSKUOptionSize 抽屉里每个仓最多列多少条货。
//
// 抽屉是**快捷入口**：选不到的去库存页核对（那里有分页与关键字）。把整张库存表拉进内存，
// 在仓库多 / 货多时是纯粹的浪费，而这里只需要「最近常用的一屏」。
const warehouseSKUOptionSize = 100

// warehouseSKUOptions 新建商品抽屉「从仓库选」的候选：(仓库 → 该仓的货) 分组（迁移 251）。
//
// 逐仓取：一次取全库再在内存分组会把整张表拉进内存；每仓上限见 warehouseSKUOptionSize。
// 未注入库存契约（装配缺陷 / 直接渲染模板的单测）时返回空 —— 抽屉照常渲染，
// 只是没有「从仓库选」这条入口（接口路径传来的 skuSource=warehouse 仍由 service 校验）。
func (h *productPageHandle) warehouseSKUOptions(ctx context.Context, projectID string, warehouses []gin.H) (out []gin.H, err error) {
	out = []gin.H{}
	if h.inventories == nil || strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	for _, w := range warehouses {
		id, _ := w["ID"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		rows, lerr := h.inventories.ListWarehouseSKUs(ctx, &inventorycontract.ListWarehouseSKUReq{
			ProjectID: projectID, WarehouseID: id, Size: warehouseSKUOptionSize,
		})
		if lerr != nil {
			return nil, lerr
		}
		if len(rows) == 0 {
			continue
		}
		items := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			// 候选一律取**裸码**（仓库侧的名字，不带仓码前缀）：前端把它填进「SKU 编码」框时
			// 服务端按 (仓, 裸码) 复核并**复用**那一行 —— 带上前缀会被当成一条新货去建行。
			// 历史行可能还带着前缀（迁移 262 只订正过一次存量），这里按同一条规则剥一次（幂等）。
			code := productservice.StripWarehousePrefix(r.SKUCode, r.WarehouseCode)
			// 候选显示「我们的 SKU」，登记的对方编码跟在后面 —— 运营手上可能是其中任意一个。
			label := code
			if r.ExternalSKU != "" && r.ExternalSKU != code {
				label += " · " + r.ExternalSKU
			}
			items = append(items, gin.H{
				"SKUCode": code, "ExternalSKU": r.ExternalSKU, "Label": label,
			})
		}
		label, _ := w["Label"].(string)
		isDefault, _ := w["IsDefault"].(bool)
		out = append(out, gin.H{
			"WarehouseID": id, "Label": label, "IsDefault": isDefault,
			// 仓短码用于前端预览「将要生成的主体 SKU」（服务端才是真源）。
			"WarehouseCode": rows[0].WarehouseCode,
			"Items":         items,
		})
	}
	return out, nil
}

// seo_entity_score_input.go - 实体评分的输入素材提取（标题、正文纯文本、图片与分类名）。

// pageDraftTitle 页面草稿的 SEO 标题（settings.seo.title）。
func pageDraftTitle(doc json.RawMessage) string {
	if len(doc) == 0 {
		return ""
	}
	var parsed struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title)
}

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// entityPlainText 商品描述（jsonb）→ 纯文本。
//
// 两种形态与构建期一致（{"html": "..."} 富文本 / JSON 字符串纯文本），
// 去标签的理由与文章侧相同：把 <p> 当正文算进字数，一篇 300 字的描述会被算成 900。
func entityPlainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if s, ok := asMap["html"].(string); ok {
			return stripEntityTags(s)
		}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return stripEntityTags(asString)
	}
	return ""
}

var entityTagRe = regexp.MustCompile("<[^>]+>")

// stripEntityTags 去 HTML 标签并压掉多余空白（只读展示与统计用，不承担清洗职责）。
func stripEntityTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(strings.Fields(entityTagRe.ReplaceAllString(s, " ")), " "))
}

// productPageImages 商品图集 → 评分图片项（首图按 hero、其余按 content，
// alt 与 Images 逐位对应 —— 与构建期 imageAltsJSON 同一份数据）。
func productPageImages(p *productdto.ProductResp) []scoring.Image {
	if p == nil {
		return nil
	}
	out := make([]scoring.Image, 0, len(p.Images))
	for i, src := range p.Images {
		if strings.TrimSpace(src) == "" {
			continue
		}
		kind := "content"
		if i == 0 {
			kind = "hero"
		}
		alt := ""
		if i < len(p.ImageAlts) {
			alt = strings.TrimSpace(p.ImageAlts[i])
		}
		out = append(out, scoring.Image{Src: strings.TrimSpace(src), Alt: alt, Kind: kind})
	}
	return out
}

// singleImage 单图实体（分类图 / 品牌 logo）→ 评分图片项。
func singleImage(src, kind string) []scoring.Image {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	return []scoring.Image{{Src: strings.TrimSpace(src), Kind: kind}}
}

// variationSpecNames 参与变体的属性组名（详情页把它们渲染成分组小标题）。
func variationSpecNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Attributes))
	for _, a := range p.Attributes {
		if a != nil && a.IsVariation && strings.TrimSpace(a.Name) != "" {
			out = append(out, strings.TrimSpace(a.Name))
		}
	}
	return out
}

// productCategoryNames 商品挂载的分类名（详情页的关联分区标题）。
func productCategoryNames(p *productdto.ProductResp) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.Categories))
	for _, c := range p.Categories {
		if c != nil && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// categoryChildNames 某分类的子分类名（列表页把子分类渲染成小节）。
//
// 取不到（分类不存在 / 列表失败）返回 nil：少一个小标题只会让标题结构项如实报缺，
// 不会让评分整体失败。
func categoryChildNames(ctx context.Context, h *productPageHandle, projectID, id string) []string {
	cats, err := h.flatCategories(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 2)
	for _, c := range cats {
		if c != nil && c.ParentID == id && strings.TrimSpace(c.Name) != "" {
			out = append(out, strings.TrimSpace(c.Name))
		}
	}
	return out
}

// slashSlug URL 段 → 路径形态（空 slug 返回空串）。
//
// 商品 / 分类 / 品牌详情页的路径由各自的 slug 构成，编辑期没有发布实例时用它做
// URL 干净度判定的近似值：slug 本身就是要检查的对象（长度、大小写、参数），
// 不是编出来的路径。
func slashSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/" + slug
}

// firstNonEmptyString 取第一个非空值。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// productTranslateMiddleware 让本模块的 service 能按请求语言取词。
//
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），而它要产出**展示文案**：内置定价 / 标签规则的展示名与描述、筛选条件的可读
// 标签、调价行的状态与原因。这里把 shell.TranslateFor(c) 放进 Request 的 Context，
// handler 里 `ctx := c.Request.Context()` 因此自带取词函数 ——
// 既不必给每个 contract 方法加 tr 参数，也不会漏掉任何一个调用点（漏一个的表现是
// 那句话在英文界面上还是中文，且没有任何测试会红）。
//
// 未挂本中间件的路径（单测直调 service）行为不变：取词函数缺失时 service 回落中文兜底
// （见 service/product_translate.go 的 translateFrom）。
func productTranslateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tr := productservice.TranslateFunc(shell.TranslateFor(c))
		c.Request = c.Request.WithContext(productservice.WithTranslate(c.Request.Context(), tr))
		c.Next()
	}
}
