package contenthttp

// 搬迁只做三件事：包名换成 contenthttp；壳层调用换成 shell.*；
// 页面标题常量由 dashboard enums 换成同值的 i18n key 字面量（词条表键不变）。
// 取数逻辑、权限点、模板名一律未动。

// 形态是「新建页面 + 预填草稿文档」，**不绑定**：文章仍在 contents.data.body 里，
// 导入出来的页面是独立的一份 Page 草稿 —— 之后改文章不会改页面，改页面也不会改文章。
// 这是当前阶段的正确形态：决策 5 第 4 步（文章 body 改存组件树、两条轨合一）没做，
// 两条轨各有各的真源，导入就是一次边界清晰的复制，而不是建立一条看不见的同步关系。
//
// 两步交互而不是一步到位：
//
//	预览（纯计算，不写库）→ 确认创建（写页面草稿并跳画布）
//
// 拆两步的理由就是决策 5 的「降级不静默」：转换是有损的（不可逆组件变占位文字、
// 白名单外标签被剥壳、列表项内的格式拿不回来），必须让使用者在**点创建之前**看到损失清单。

// 左栏标题 / 路径 / 正文（Trix 富文本，partials/rich_editor.html），右栏实时预览
// + 「可视化编辑」入口（未保存时置灰，提示先保存）。表单 POST 复用既有
// /admin/articles/create，字段名 title / slug / body 与原抽屉逐字一致，成功后
// 照旧 302 进编辑页 —— 路由注册处（article_router.go）对本页挂
// CasbinMiddlewareForPath("/api/content/create")：能建文章的人才能打开新建页。

// 它们原先散在 dashboard 包的多个文件里（product_pricing_handle.go 的 firstNonEmpty、
// translationLangOption）。跨包引私有符号不成立，页面搬回本模块后各自持有一份 ——
// 都是几行的纯函数，语义不会分叉。

// 这些结构原先定义在 dashboard 的 settings_handle.go 里（页面设置面板、文章编辑页、
// 商品/分类/品牌页三处共用）。文章页搬回 content 模块后不能跨包引用 dashboard 的私有
// 类型，这里按原定义**逐字段复制**一份：模板 fragments/seo_score 是按字段名取值的，
// 少一个键 Jet 会报错并截断整页，所以字段集必须与模板要求一致（含文章页用不到的
// ProfileType / Duplicates / Empty —— 它们由同一份模板消费）。
//
// 与 dashboard 那份的同步点是模板契约而不是 Go 类型：只要 fragments/seo_score 的键不变，
// 两份定义各自演进互不影响。

// 现象：文章保存后，依赖扇出（pipeline.Fanout）会按依赖表把引用它的**手工页面**与
// **已发布详情页**标记为 stale；同一件事也会由块内容变更（page.MarkStaleForBlock /
// MarkStaleForTheme）、主题与导航变更触发。但这一步**只发生在日志与 pages.stale 列里** ——
// 运营在后台看不到「现在有多少个页面等着重建、是哪些页面」：
//
//   · 文章编辑页只有一句静态说明「引用这篇文章的页面会被标记待重建」（没有数字，没有清单）；
//   · pages.stale 只有一个徽标，没有任何地方把它汇总成影响面。
//
// 本文件补的是**只读可见性**：不改变任何构建 / 发布 / 标记行为，也**不做自动重建**
//（本批只做可见性）。数据源是 page 契约已有的只读面（List + PageResp.Stale），
// 因此不新造巡检页面、不新增路由与权限点。
//
// 性能口径：工程数与页面数都是后台量级，逐工程 List 一次即可；清单截断到
// staleImpactPageLimit 条并在页面上显式说明「还有更多」——不给出一份看起来完整、
// 实际被静默截断的清单（那会让人以为重建范围就这么大）。

// 与商品域工作台（product_translation_handle.go）同一套机制：列表按实体分组、
// 逐行显示原文与译文、整表提交、只写真正变化的行、保存后精确标记受影响实例待重建。
//
// 差异只在两处：
//
//	· 数据源是 content 模块（文章），字段清单来自 contentcontract（title / body / excerpt /
//	  seoTitle / seoDescription）；
//	· body 是**富文本**：模板给出多行输入，且校验要求译文与原文**形态一致**
//	  （原文含标签则译文也必须含标签）—— 形态检查在这里，标签白名单在构建期
//	  （core.SanitizeRichHTML）：保存期挡住明显的错误，构建期挡住恶意内容。
//
// 验收对应（I18N-006）：文章译文在对应语言产物中生效（构建期取词）/ 改原文后旧译文按
// hash 失效并回退原文（寻址机制天然保证）/ 译文正文经过白名单清洗（构建期清洗）。

// 视图组装全部做成纯函数：渲染键名与计数口径只在这里定义一次，真实渲染测试
// 直接喂数据走同一条组装路径，而不是在测试里手抄一份键名 —— 手抄的那份会随模板
// 演进静默失配，那正是「页面上少了一块、断言却通过」的成因。

// 文章写完要能上线才算闭环，但上线的两个前提可能在装配里缺失，所以这一块全部
// 按「能力不足就明确说不」来做，不做假承诺：
//
//   - 未注入发布能力（instances == nil）→ 只显示状态位，不渲染任何表单；
//   - 没有 article 类型的内容模板 → **不渲染发布表单**，改给一句可执行的出口。
//     渲染一个必然失败的下拉比不给入口更糟：运营点一次、失败一次，最后怀疑的是系统。
//
// 权限点复用商品详情模板页那两条（/api/presentation/create 与 /api/presentation/rebuild），
// 不新增权限点：发布行为完全相同，只是实体类型不同。

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/richdoc"
	"go_wp/internal/module/block/enums"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/content/dto"
	"go_wp/internal/module/content/enums"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/contenttemplate/dto"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/presentation/dto"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/project/enums"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/internal/siteurl"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

const (
	// articleEntityType contents 表当前唯一合法类型（迁移 080 的 CHECK 约束）。
	articleEntityType = "article"
	// articleListPageSize 列表每页条数的兜底值。
	//
	// 正常路径上每页条数由 shell.PageParams 决定（默认 20、上限 100，可被 ?limit= 覆盖），
	// 这个常量只在 limit 异常（0 / 负数）时兜底。它同时是一页的**跨模块查询预算**：
	// 列表里每篇文章都要逐条查一次 presentation 实例（articlesPublished），
	// 条数直接决定查询次数 —— 所以它不是「随便取大点」的数字。
	//
	// 分页之前这里写的是 articleListLimit = 50 且一次性取满：超过 50 篇的文章
	// 在页面上根本不存在（审计 02-L P1-14）。现在按页取，总量不再有上限。
	articleListPageSize = 20
	// 页面标题：字面量就是 i18n key（迁移 163 的词条表键），字面量走 withI18n 的 fallback
	// 链路 t(标题, 标题) 回落原文，与系统页面槽位页、商品详情模板页同口径。
	// 值与 dashboard enums 的 MsgArticlesTitle / MsgArticlesEditTitle 完全相同 ——
	// i18n key 是字符串协议，页面的词条归属跟着页面走，不再跨模块引 enums。
	articlePageTitle = "MsgArticlesTitle"
	articleEditTitle = "MsgArticlesEditTitle"
	articleNewTitle  = "新建文章"
	// articleEmptyField 空字段展示占位（表格空白单元格读不出「没有值」）。
	articleEmptyField = "—"
	// articleBlogPathPrefix 文章详情页的默认线上路径前缀（可在发布区块里改）。
	articleBlogPathPrefix = "/blog/"
)

// 成功回执（常量值是 i18n key，登记在 articleFacingMessages 里 —— 提示页正文同样过白名单）。
//
// 常量值是 **i18n key**，中文兜底在同文件的 articleFacingMessages —— 两者成对，
// 取词统一走 articleFacingText（它按当前语言翻）。此前这里的常量值是中文本身、
// 被当成 key 去查库，而库内没有中文 item_key，于是永远只显示中文。
const (
	articleCreatedText = "admin.article.ok.created"
	articleSavedText   = "admin.article.ok.saved"
	articleDeletedText = "admin.article.ok.deleted"
)

// articleFacingMessages 本页可以原样展示给运营的文案（白名单）。
//
// **键是 i18n key**（两类都是）：content 模块错误常量的值就是常量名、而常量名即词条 key；
// 本页自造的成功 / 参数级文案的常量值也已经是 key。值是**中文兜底**（词条缺失时显示的那句）。
//
// 为什么必须有这份白名单：content 契约把错误当字符串返回，未命中的多半是数据库原文
// （可能带表名甚至 SQL 片段），那是给运维看的，不能直接进页面。
var articleFacingMessages = map[string]string{
	contentenums.ErrInvalidParam: "提交的信息不完整，请检查标题与路径后重试。",
	contentenums.ErrNotFound:     "这篇文章不存在，可能已被删除。",
	contentenums.ErrInvalidType:  "内容类型不受支持（本页只处理文章）。",
	contentenums.ErrInvalidField: "提交了不支持的字段，请刷新页面后重试。",
	contentenums.ErrSlugTaken:    "这个路径（slug）已被另一篇文章占用，换一个。",
	contentenums.ErrDataInvalid:  "文章数据格式异常，请联系管理员。",
	articleCreatedText:           "文章已创建。写完正文后记得保存。",
	articleSavedText:             "已保存。引用这篇文章的页面与已发布的详情页已标记待重建。",
	articleDeletedText:           "文章已删除。",
	articleMissingIDText:         "缺少文章标识，请回到列表页重试。",
	articleMissingTitleText:      "请先填写文章标题。",
	articleMissingSlugText:       "请先填写文章路径（slug）。",
}

// 参数级错误（本页自造；同样登记白名单 —— 自造文案不登记会在回显时被自己吞掉）。
// 值是 i18n key，中文兜底见 articleFacingMessages。
const (
	articleMissingIDText    = "admin.article.err.missingID"
	articleMissingTitleText = "admin.article.err.missingTitle"
	articleMissingSlugText  = "admin.article.err.missingSlug"
)

// articlePublishPort 发布区块所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本页真正用到的方法：读绑定、首次发布、重建、改 URL。不直接依赖
// presentationcontract 全量契约（它还带着删除、切换模板等本页不碰的写能力）。
type articlePublishPort interface {
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
	// UpdateURL 改 URL（发布后换路径）：新路径激活 + 旧路径 301 / 取消激活。
	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)
	// PreviewInstance 用详情页模板渲染一份**不写库不激活**的预览（编辑页右栏的预览帧）。
	// 它是「看得出发布后长什么样」的唯一来源：拼正文 HTML 那种回显照不出模板的任何东西。
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
}

// articlePageHandle 文章管理页处理器。
type articlePageHandle struct {
	contents  contentcontract.ContentService
	projects  projectcontract.ProjectService
	templates contenttemplatecontract.ContentTemplateService
	// instances 发布能力（装配期注入；为空时发布区块整体不渲染）。
	instances articlePublishPort
	// pages 页面能力（导入到画布用）：只调 Create 新建一页草稿，不碰发布/删除。
	// 为空时导入区块明确提示"能力未装配"，而不是给一个点了会 500 的按钮。
	pages pagecontract.PageService
	// linkLocator 已上线路径解析端口（SEO-015 内链建议；为空时建议端点降级为空列表）。
	linkLocator presentationcontract.PublishedEntityLocator
}

// NewArticlePageHandle 构造。
func NewArticlePageHandle(contents contentcontract.ContentService, projects projectcontract.ProjectService,
	templates contenttemplatecontract.ContentTemplateService, instances articlePublishPort,
	pages pagecontract.PageService) *articlePageHandle {
	return &articlePageHandle{
		contents: contents, projects: projects, templates: templates,
		instances: instances, pages: pages,
	}
}

// ArticlesPage 文章列表（GET /admin/articles）。
//
// 分页（审计 02-L P1-14）：页码与每页条数走 shell.PageParams，总数由内容契约的 Count 给出
// （与 List 同一份过滤条件：实体类型和关键词），列表按 offset 取当页 —— 不再「一次取 50 条、
// 超过 50 篇的文章在页面上根本不存在」。
//
// 取数顺序是**先计数 → 收敛页码 → 再取当页**（上一轮的实测教训，货源页与采购页各踩过一次）：
// 反过来（先取第 N 页再数总数）时，越界页码会让 service 返回空页，而分页条按收敛后的页码
// 渲染 ——「表格为空、分页条却显示第 2 页」正是这样产生的。
//
// 不走「全量拉取 + handler 切片」：articlesPublished 对每篇逐条查 presentation 实例，
// 全量取数意味着 N 次跨模块查询，文章量级一涨就成倍放大（这正是上一轮否决该方案的理由）。
func (h *articlePageHandle) ArticlesPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 写动作的结论走 shell.RenderJump 渲染整页提示（见 content_err.go），不再经
	// ?err= / ?ok= / ?done= 回带 —— 那条通道要求读侧再判一次「这条提示是不是本仓给的」，
	// 而查询参数不是可信边界。这里只剩一处提示来源：**列表取数失败**。
	pageErr := ""

	page, limit := shell.PageParams(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	filter := &contentdto.ListReq{EntityType: articleEntityType, Keyword: keyword}
	// 先计数。总数只用于「总页数」与页码收敛；计数失败不阻塞列表取数（列表照常按原页码取），
	// 但会把错误显示出来 —— 静默地「没有分页条」会让人以为文章本来就不多。
	total, cerr := h.contents.Count(ctx, filter)
	if cerr != nil {
		pageErr = firstNonEmpty(pageErr, articleFacingError(c, cerr))
	} else {
		page = articleClampPage(page, limit, total)
	}
	filter.Limit, filter.Offset = limit, (page-1)*limit
	list, err := h.contents.List(ctx, filter)
	if err != nil {
		pageErr = firstNonEmpty(pageErr, articleFacingError(c, err))
	}
	data := articleListPageData(list, articlesPublished(ctx, h.instances, list), pageErr, shell.TranslateFor(c))
	data["Keyword"] = keyword
	data["Limit"] = limit
	data["ClearFilterURL"] = shell.FilterBaseURL("/admin/articles", map[string]string{"limit": strconv.Itoa(limit)})
	// 写动作的回跳要带上本次筛选（shell.BackPath 从表单 action 的 query 读回，见 content_err.go）：
	// 预编码成成品 query 串（含前导 "?"）再交给模板 —— Jet 的 {{ }} 是 HTML 转义不是 URL 转义，
	// 关键词里的 & 在模板里直接拼会切断 query；带 "?" 则空串时不留孤立的 "?"。
	data["FilterQuery"] = "?" + url.Values{"keyword": {keyword}, "limit": {strconv.Itoa(limit)}}.Encode()
	// Total 覆盖为**真源总数**：articleListPageData 给的是当页行数，分页之后它不再等于
	// 文章总数 —— 不覆盖的话列表工具栏会写着「全部文章（20）」，而库里有两百篇。
	if cerr == nil {
		data["Total"] = total
	}
	// 分页条（shell 组件，服务端渲染）：单页或空数据时 BuildPagination 返回 nil，
	// TemplateKeys 给空 map，模板的 {{if .["PaginationLinks"]}} 自然跳过。
	//
	// 翻页保留关键词；写动作的结论不再经查询参数回带（走提示页），基地址上只有筛选。
	for k, v := range shell.BuildPagination(total, page, limit,
		shell.FilterBaseURL("/admin/articles", map[string]string{"keyword": keyword}), shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	// 依赖失效影响面（只读）：文章 / 块 / 主题 / 导航变更后，哪些页面正在等待重建。
	// 本批只做可见性，不做自动重建（见 article_stale_impact.go）。
	data["StaleImpact"] = articleStaleImpact(ctx, h, shell.TranslateFor(c))
	c.HTML(http.StatusOK, "admin/content/articles.html", shell.Prepare(c, data))
}

// articleClampPage 把页码收敛到实际总页数以内（total=0 时收敛到第 1 页）。
//
// 与 shell.BuildPagination 内部的收敛同一条规则（总页数由 total 与 limit 算出），
// 差别只在于这里发生在**取数之前**：越界页码（手输 URL、书签失效、上一次翻页留下的页码）
// 直接传给取数层时 service 会老实返回空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样产生的。
// 同形实现在 product 域（clampPageToTotal），两处都是「取数前收敛」这一条判据的落地。
func articleClampPage(page, limit int, total int64) int {
	if limit < 1 {
		limit = articleListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

// ArticleEditPage 文章编辑页（GET /admin/articles/edit?id=）；不带 id 即新建。
func (h *articlePageHandle) ArticleEditPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.Query("id"))
	// 写动作的结论走 shell.RenderJump（见 content_err.go），不再经 ?err= / ?ok= 回带。
	pageErr := ""

	var item *contentdto.ContentResp
	if id != "" {
		got, err := h.contents.Get(ctx, &contentdto.GetReq{ID: id})
		if err != nil {
			pageErr = articleFacingError(c, err)
		} else {
			item = got
		}
	}
	c.HTML(http.StatusOK, "admin/content/article_edit.html",
		shell.Prepare(c, articleEditPageData(ctx, h, item, id, pageErr, requestScoreLang(c), shell.TranslateFor(c))))
}

// ArticleCreate 新建文章（POST /admin/articles/create，权限点 content:create）。
func (h *articlePageHandle) ArticleCreate(c *gin.Context) {
	form := articleFormOf(c)
	if form.Title == "" {
		articleListJump(c, false, articleFacingText(c, articleMissingTitleText))
		return
	}
	if form.Slug == "" {
		articleListJump(c, false, articleFacingText(c, articleMissingSlugText))
		return
	}
	res, err := h.contents.Create(c.Request.Context(), &contentdto.CreateReq{
		EntityType: articleEntityType, Slug: form.Slug, Data: form.data(),
	})
	if err != nil {
		articleListJump(c, false, articleFacingError(c, err))
		return
	}
	// 新建后直接进编辑页：列表页的表单只有标题与路径，正文要在这里写。
	articleEditJump(c, true, res.ID, articleFacingText(c, articleCreatedText))
}

// ArticleUpdate 保存文章（POST /admin/articles/update，权限点 content:update）。
//
// 成功走整页提示（1 秒后回编辑页）；**失败仍原地重渲编辑页并回填本次提交** ——
// 这是「写表单失败时原地留住输入」那一条的落地（internal/templates/CLAUDE.md）：
// 整页提示会把用户刚写的正文整屏清掉，比错误文案贵得多。两条路径都只给已归口的文案。
func (h *articlePageHandle) ArticleUpdate(c *gin.Context) {
	form := articleFormOf(c)
	if form.ID == "" {
		h.articleUpdateFailure(c, articleMissingIDText, "id")
		return
	}
	if form.Title == "" {
		h.articleUpdateFailure(c, articleMissingTitleText, "title")
		return
	}
	if _, err := h.contents.Update(c.Request.Context(), &contentdto.UpdateReq{ID: form.ID, Data: form.data()}); err != nil {
		h.articleUpdateFailure(c, articleFacingError(c, err), "")
		return
	}
	articleEditJump(c, true, form.ID, articleFacingText(c, articleSavedText))
}

// articleUpdateFailure reads metadata for the side panels, then replaces every editable
// field with this POST's raw value (including empty strings).
func (h *articlePageHandle) articleUpdateFailure(c *gin.Context, pageErr, invalidField string) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.PostForm("id"))
	var item *contentdto.ContentResp
	if id != "" {
		got, err := h.contents.Get(ctx, &contentdto.GetReq{ID: id})
		if err != nil {
			// 元数据读取错误只记录日志，页面仍展示原本的保存错误。
			articleInternalText(c, err)
		} else {
			item = got
		}
	}
	data := articleEditPageData(ctx, h, item, id, pageErr, requestScoreLang(c), shell.TranslateFor(c))
	form := data["Form"].(gin.H)
	for key, field := range map[string]string{
		"Title": "title", "Body": "body", "Excerpt": "excerpt",
		"FeaturedImage": "featuredImage", "FocusKeyword": "focusKeyword",
	} {
		form[key] = c.PostForm(field)
	}
	// SEO 标题 / 描述与标题 / 摘要合并（2026-09-30）：表单里已没有这两个框，
	// 回填与实时评分都取同一口径（下面的评分直接读 Title / Excerpt）。
	data["IsNew"] = false // 缺 id 时仍展示编辑页，避免误落入新建动作。
	data["EditUnavailable"] = item == nil
	if item == nil {
		// 元数据不可用时禁用依赖旧版本、路径和实体标识的所有后续动作。
		for k, v := range articlePublishUnavailable("") {
			data[k] = v
		}
		for k, v := range articleImportUnavailable("") {
			data[k] = v
		}
		data["TemplateEditURL"] = ""
	}
	data["InvalidField"] = invalidField
	data["Score"] = articleScoreViewOf(map[string]any{
		"title": form["Title"], "body": form["Body"], "excerpt": form["Excerpt"],
		"focusKeyword": form["FocusKeyword"],
	}, articlePreviewURL(articleSlugOf(item)), requestScoreLang(c), shell.TranslateFor(c))
	c.HTML(http.StatusOK, "admin/content/article_edit.html", shell.Prepare(c, data))
}

// ArticleDelete 删除文章（POST /admin/articles/delete，权限点 content:delete）。
func (h *articlePageHandle) ArticleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		articleListJump(c, false, articleFacingText(c, articleMissingIDText))
		return
	}
	if err := h.contents.Delete(c.Request.Context(), &contentdto.DeleteReq{ID: id}); err != nil {
		articleListJump(c, false, articleFacingError(c, err))
		return
	}
	articleListJump(c, true, articleFacingText(c, articleDeletedText))
}

// ArticlesBulkDelete 批量删除文章（POST /admin/articles/bulk-delete，权限点 content:delete）。
//
// 逐条走**同一条单条删除路径**（h.contents.Delete）：某一条失败不中断整批 ——
// 批量操作因为一条失败就整批回滚时，用户会以为「一条都没删」然后反复重试。
// 结果按「已删除 N 篇 / 跳过 M 篇」渲染成提示页（有跳过即失败态，更显眼）。
func (h *articlePageHandle) ArticlesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）保持可见，但同样经白名单判定来源。
		articleListJump(c, false, articleFacingOrInternal(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.contents.Delete(c.Request.Context(), &contentdto.DeleteReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	// 有跳过 → 失败态（警告更显眼，运营下次会去看剩下那些）；全成功 → 成功态（1 秒后回列表）。
	articleListJump(c, skipped == 0, articleBulkDeleteResult(c, deleted, skipped))
}

// articleBulkText 批量结论文案的一条模板（i18n key + 中文原文）。
//
// 只有写侧用这一份：articleBulkDeleteResult 拿它 Sprintf 出整句，结论随提示页渲染
// （不再经 ?done=，读侧那套「数字归一后比对回显」已整批删除）。
type articleBulkText struct{ key, fallback string }

// articleBulkTextOf 取一条批量结论文案的当前语言文本。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（本文件的模板一律只允许 %s，
// 数字先经 strconv.Itoa）—— 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func articleBulkTextOf(c *gin.Context, t articleBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// articleBulkResultTemplates 批量删除的四个结论分支。
//
// 只在写侧使用：articleBulkDeleteResult 用它 Sprintf 出文案，结论随提示页渲染。
var articleBulkResultTemplates = []articleBulkText{
	{contentenums.BulkArticleNoneSelected, "没有选中任何文章，列表未改动。"},
	{contentenums.BulkArticleAllDeleted, "已删除 %s 篇文章。"},
	{contentenums.BulkArticleAllSkipped, "%s 篇文章都未能删除，列表未改动。"},
	{contentenums.BulkArticlePartial, "已删除 %s 篇，%s 篇未能删除（可能已被删除）。"},
}

// articleBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚，
// 不能只报「操作完成」（部分成功被静默成全部成功，用户不会再去看剩下那几篇）。
func articleBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return articleBulkTextOf(c, articleBulkResultTemplates[0])
	case skipped == 0:
		return fmt.Sprintf(articleBulkTextOf(c, articleBulkResultTemplates[1]), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(articleBulkTextOf(c, articleBulkResultTemplates[2]), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(articleBulkTextOf(c, articleBulkResultTemplates[3]), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

// ArticleScorePanel 渲染 SEO 评分侧栏片段（POST /admin/articles/score）。
//
// 走 HTMX 局部刷新而不是整页重绘：编辑者刚改完标题/关键词就按「重新评分」，
// 表单不重画，光标与未保存的正文都不会丢。无 JS 时退化为「保存后刷新整页看分数」——
// 编辑页的初始评分就是服务端算好后直接渲染进去的。
//
// 复用页面设置面板那份片段（fragments/seo_score）：两边吃的是同一个 scoreView，
// 各写一份模板只会让「哪些检查项算未达标」的呈现方式慢慢分叉。
func (h *articlePageHandle) ArticleScorePanel(c *gin.Context) {
	data := articleFormOf(c).data()
	// t 是片段模板的取词函数：seo_score 片段不经 shell.Prepare，缺 t 时 Jet 把取词调用
	// 求值成空串（不报错、不 500），整片提示会变成空白。
	c.HTML(http.StatusOK, "fragments/seo_score",
		gin.H{"Score": articleScoreViewOf(data, strings.TrimSpace(c.PostForm("url")), requestScoreLang(c), shell.TranslateFor(c)),
			"t": shell.TranslateFor(c)})
}

// ArticleSeoDrawer SEO 评测抽屉片段（GET /admin/articles/seo/drawer?id=xxx）。
//
// 评测面板从编辑页右栏移到了抽屉：右栏该留给「正文 + 预览」（写的时候一直要看的东西），
// 评测是按需看的 —— 写完一段才想看分。常驻卡在不看的时刻只是噪声，还占掉 1/3 栏宽。
//
// 取数：库里**已保存**的文章（不是表单当前值 —— 抽屉打开那一刻，页面上未保存的改动
// 不在服务端）。要看改动后的分数用抽屉里的「重新评分」：它 hx-include="#article-form"
// 带上当前表单值，与改造前那条路径逐字一致（改造只搬了容器位置）。
//
// 失败一律给状态码、不拼半截片段：drawer.js 对非 200 与对「缺 data-drawer-fragment /
// 缺列的片段」是同一种处置（都是「加载失败，请重试」），拼半截只是多花一次渲染。
func (h *articlePageHandle) ArticleSeoDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := strings.TrimSpace(c.Query("id"))
	if id == "" || h == nil || h.contents == nil {
		c.Status(http.StatusNotFound)
		return
	}
	var item *contentdto.ContentResp
	got, err := h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
	if err != nil {
		logger.Scene("content").With("article_id", id).Error(err, "读取文章失败（SEO 评测抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	item = got
	if item == nil || item.ID != id {
		c.Status(http.StatusNotFound)
		return
	}
	data := item.Data
	c.HTML(http.StatusOK, "admin/content/article_seo_drawer.html", shell.Prepare(c, gin.H{
		"Score": articleScoreViewOf(data, articlePreviewURL(articleSlugOf(item)), requestScoreLang(c), shell.TranslateFor(c)),
		"IsNew": false,
	}))
}

// ArticlePreviewFrame 文章详情页的真实预览帧（GET /admin/articles/preview-frame?id=xxx）。
//
// 与「实时回显」的分工（rich-editor/live-preview.js 仍服务新建页）：那个把编辑器内容拼进 iframe，
// 改一下立刻看到，但只有白底正文排版 —— 看不到详情页模板的任何东西。
// 这个用**详情页模板**渲染（presentation.PreviewInstance，不写库、不激活），看到的就是发布后的形态，
// 代价是只反映**已保存**的数据：改完正文先保存，再刷新这里。
//
// 为什么 iframe 直接 src 而不是前端取 HTML 塞 srcdoc：详情页是完整文档（自带样式与脚本），
// 塞进 srcdoc 会与外层后台页面同源、脚本可能互扰；指一个页面组路由（Session 鉴权，不需 CSRF）
// 既最省事也最隔离。
func (h *articlePageHandle) ArticlePreviewFrame(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	tr := shell.TranslateFor(c)
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		articlePreviewFrameNotice(c, tr("admin.article.preview.needSave", "保存这篇文章后，这里会显示它的详情页真实形态。"))
		return
	}
	if h == nil || h.instances == nil {
		articlePreviewFrameNotice(c, tr("admin.article.preview.unavailable", "预览不可用：发布能力未装配。"))
		return
	}
	res, err := h.instances.PreviewInstance(c.Request.Context(), &presentationdto.PreviewInstanceReq{
		EntityType: articleEntityType, EntityID: id,
	})
	if err != nil {
		// 原文只进日志（后台页面不得直出内部错误）；页面上给一句能行动的话。
		logger.Scene("content").With("article_id", id).Error(err, "文章详情页预览渲染失败")
		articlePreviewFrameNotice(c, tr("admin.article.preview.failed", "预览渲染失败（多半是这部文章还没有可用的详情页模板）。"))
		return
	}
	if res == nil || strings.TrimSpace(res.HTML) == "" {
		articlePreviewFrameNotice(c, tr("admin.article.preview.empty", "还没有可渲染的详情页：先给 article 类型建一套内容模板。"))
		return
	}
	// 直出的是**构建器渲染出来的详情页 HTML**（预览产物），不是错误信息。
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(res.HTML))
}

// articlePreviewFrameNotice 预览不可用时的替代页（iframe 里的一句人话）。
//
// 不用 5xx：iframe 对 5xx 显示的是浏览器自带错误页，读的人只会以为「后台坏了」——
// 而这里大多数情况是「还没保存」或「还没建模板」，都是正常状态。
func articlePreviewFrameNotice(c *gin.Context, text string) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(
		`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>`+
			`body{margin:0;padding:24px;font:14px/1.7 -apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;`+
			`color:#57606a;background:#fff;}p{margin:0;}</style></head><body><p>`+html.EscapeString(text)+`</p></body></html>`))
}

// —— 表单与视图 ——

// 导入页面的固定取值。
//
// kind 用 home + none 目标：page / article / product 这些 kind 要求绑定内容目标
// （page 模块 validateKind 的规则），而导入是「把正文复制成一页普通内容」，没有内容实体可绑。
// 页面管理页的新建页面用的是同一组取值，导入出来的页面与它完全同类。
const (
	articleImportKind       = "home"
	articleImportTargetType = "none"
	// 默认页面路径前缀：/article-<slug>。给一个能直接用的默认值，作者可改。
	articleImportPathPrefix = "/article-"
)

// 导入相关文案（登记在白名单里 —— 它们会作为提示页正文渲染）。
//
// 常量值是 **i18n key**，中文兜底在 articleImportFacingMessages —— 两者成对，
// 取词统一走 articleImportTextOf。
const (
	articleImportEmptyBodyText = "admin.article.import.err.emptyBody"
	articleImportNoProjectText = "admin.article.import.err.noProject"
	articleImportNoPathText    = "admin.article.import.err.noPath"
	articleImportNoPageText    = "admin.article.import.err.noPageCapability"
	articleImportDepsText      = "admin.article.import.err.depsMissing"
)

// articleImportTextOf 本页文案的当前语言文本（key + 白名单里的中文兜底）。
func articleImportTextOf(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, articleImportFacingMessages[key])
}

// articleImportText 组件类型 / 降级动作的词条：key + 中文兜底。
type articleImportText struct{ Key, Fallback string }

// articleImportPreviewView 预览视图（模板只做分支渲染，不做统计与判断 ——
// 统计口径只在这里定义一次，真实渲染测试喂同一份数据走同一条组装路径）。
type articleImportPreviewView struct {
	OK        bool
	NodeCount int
	// CountText 「转换结果：N 个组件」的整句（Go 侧 FillTranslate 生成）。
	// 为什么不拆成前后缀给模板拼：中英语序不同（「3 个组件」/「3 components」），
	// 英文那半的前缀是空串，而空串在取值链里等同「缺失」，会静默回落到中文。
	CountText string
	Types     []articleImportTypeView
	Warnings  []articleImportWarningView
	Lossless  bool
}

// articleImportTypeView 一种组件的数量。
type articleImportTypeView struct {
	Type  string
	Label string
	Count int
}

// articleImportWarningView 一条降级记录的中文表述。
type articleImportWarningView struct {
	Tag    string
	Action string
	Detail string
}

// articleComponentLabels 组件类型 → 中文名（预览里给人看的，不是给机器判的）。
//
// 只列会出现的那几个：导入方向只产出可逆子集 + 占位，不会出现几十种组件。
var articleComponentLabels = map[string]articleImportText{
	"core.heading":   {"admin.article.import.node.heading", "标题"},
	"core.text":      {"admin.article.import.node.text", "正文"},
	"core.list":      {"admin.article.import.node.list", "列表"},
	"core.quote":     {"admin.article.import.node.quote", "引用"},
	"core.image":     {"admin.article.import.node.image", "图片"},
	"core.divider":   {"admin.article.import.node.divider", "分隔线"},
	"core.table":     {"admin.article.import.node.table", "表格"},
	"core.container": {"admin.article.import.node.container", "容器"},
}

// articleImportActionLabels 降级动作 → 文案（与 richdoc.WarningAction 一一对应）。
var articleImportActionLabels = map[richdoc.WarningAction]articleImportText{
	richdoc.ActionUnwrap:      {"admin.article.import.action.unwrap", "去壳保留内容"},
	richdoc.ActionDrop:        {"admin.article.import.action.drop", "已丢弃"},
	richdoc.ActionTrim:        {"admin.article.import.action.trim", "已裁剪"},
	richdoc.ActionPlaceholder: {"admin.article.import.action.placeholder", "占位（不可还原）"},
}

// ArticleImportPreview 预览转换结果（POST /admin/articles/import-preview）。
//
// 纯计算：不写库、不建页面、不改任何东西。读的是**已保存**的正文 ——
// 预览与创建必须基于同一份内容，否则"预览没问题的"和"创建出来的"会不一样。
func (h *articlePageHandle) ArticleImportPreview(c *gin.Context) {
	item, err := h.articleByID(c)
	if err != nil {
		c.HTML(http.StatusOK, "fragments/article_import",
			gin.H{"Preview": articleImportPreviewView{}, "Err": articleFacingError(c, err),
				"t": shell.TranslateFor(c)})
		return
	}
	res, err := richdoc.HTMLToNodes(articleStr(item.Data, "body"))
	if err != nil {
		c.HTML(http.StatusOK, "fragments/article_import",
			gin.H{"Preview": articleImportPreviewView{}, "Err": articleFacingError(c, err),
				"t": shell.TranslateFor(c)})
		return
	}
	// t 是片段模板的取词函数：片段不经 shell.Prepare，缺 t 时 Jet 把 tr(...) 求值成空串
	//（不报错、不 500、不记日志），整块提示会变成空白。
	c.HTML(http.StatusOK, "fragments/article_import",
		gin.H{"Preview": articleImportPreviewViewOf(res, shell.TranslateFor(c)), "Err": "",
			"t": shell.TranslateFor(c)})
}

// ArticleImportCreate 新建页面草稿并打开画布（POST /admin/articles/import-page）。
//
// 成功后直接跳工作台：使用者点这个按钮的意图就是"我要去画布里改它"，
// 中间再插一个"创建成功"的页面只是多一次点击。
func (h *articlePageHandle) ArticleImportCreate(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	draftPath := strings.TrimSpace(c.PostForm("draftPath"))

	if id == "" {
		articleListJump(c, false, articleFacingText(c, articleMissingIDText))
		return
	}
	if h.pages == nil {
		articleEditJump(c, false, id, articleImportTextOf(c, articleImportDepsText))
		return
	}
	if projectID == "" {
		articleEditJump(c, false, id, articleImportTextOf(c, articleImportNoProjectText))
		return
	}
	if draftPath == "" {
		articleEditJump(c, false, id, articleImportTextOf(c, articleImportNoPathText))
		return
	}
	item, err := h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
	if err != nil {
		articleEditJump(c, false, id, articleFacingError(c, err))
		return
	}
	res, err := richdoc.HTMLToNodes(articleStr(item.Data, "body"))
	if err != nil {
		articleEditJump(c, false, id, articleFacingError(c, err))
		return
	}
	if len(res.Nodes) == 0 {
		articleEditJump(c, false, id, articleImportTextOf(c, articleImportEmptyBodyText))
		return
	}
	doc, err := articleImportDocument(item.Data, res.Nodes)
	if err != nil {
		articleEditJump(c, false, id, articleFacingError(c, err))
		return
	}
	page, err := h.pages.Create(c.Request.Context(), &pagecontract.CreateReq{
		ProjectID:         projectID,
		Kind:              articleImportKind,
		ContentTargetType: articleImportTargetType,
		DraftPath:         draftPath,
		DraftDocument:     doc,
	})
	if err != nil {
		articleEditJump(c, false, id, articleImportFacingError(c, err))
		return
	}
	// 成功直接进工作台（不经提示页）：点这个按钮的意图就是「我要去画布里改它」，
	// 中间再插一个提示页只是多一次点击 —— 与 block 的新建成功不同，这里没有
	// 「必须让运营看清的一句话」要停留。失败分支仍走提示页（回编辑页，见上）。
	c.Redirect(http.StatusFound, "/workbench?id="+page.ID)
}

// articleByID 读一篇文章（缺少 id 时给参数错误）。
func (h *articlePageHandle) articleByID(c *gin.Context) (*contentdto.ContentResp, error) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		id = strings.TrimSpace(c.Query("id"))
	}
	if id == "" {
		return nil, errArticleIDMissing
	}
	return h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
}

// errArticleIDMissing 本页自造的参数错误（进白名单，见 articleFacingMessages 的补充）。
var errArticleIDMissing = &articleImportError{text: articleMissingIDText}

// articleImportError 本页自造的简单错误（只为让 articleFacingError 认得它）。
type articleImportError struct{ text string }

func (e *articleImportError) Error() string { return e.text }

// articleImportDocument 组装 Page 草稿文档。
//
// 文章的 SEO 字段带进页面设置：页面设置面板、构建期 meta 与编辑期评分器读的都是那里，
// 带过去之后"导入"就不会把已经写好的标题/描述/主关键词丢在文章里。
// 标题与描述取 title / excerpt（2026-09-30 字段合并：文章的 SEO 标题与描述就是它们）。
func articleImportDocument(data map[string]any, nodes []*core.Node) (json.RawMessage, error) {
	seo := map[string]any{"schemaType": "article"}
	if v := articleStr(data, "title"); v != "" {
		seo["title"] = v
	}
	if v := articleStr(data, "excerpt"); v != "" {
		seo["description"] = v
	}
	if v := articleStr(data, "focusKeyword"); v != "" {
		seo["focusKeyword"] = v
	}
	doc := map[string]any{
		"settings": map[string]any{
			// 版心模式是编译端必填项（空值会被设置校验拒掉）。
			"layout": map[string]any{"mode": "full"},
			"seo":    seo,
		},
		"root": nodes,
	}
	return json.Marshal(doc)
}

// articleImportPreviewViewOf 转换结果 → 预览视图（纯函数）。
func articleImportPreviewViewOf(res *richdoc.Result, trs ...func(key, fallback string) string) articleImportPreviewView {
	tr := articlePublishTr(trs)
	if res == nil {
		return articleImportPreviewView{}
	}
	counts := map[string]int{}
	order := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		if n == nil {
			continue
		}
		if _, seen := counts[n.Type]; !seen {
			order = append(order, n.Type)
		}
		counts[n.Type]++
	}
	view := articleImportPreviewView{
		OK:        true,
		NodeCount: len(res.Nodes),
		CountText: i18n.FillTranslate(tr, "admin.article.import.resultCount",
			"转换结果：{count} 个组件", map[string]string{"count": strconv.Itoa(len(res.Nodes))}),
		Lossless: len(res.Warnings) == 0,
		Types:    make([]articleImportTypeView, 0, len(order)),
		Warnings: make([]articleImportWarningView, 0, len(res.Warnings)),
	}
	for _, t := range order {
		// 认不出来的组件显示原始类型名：比显示"未知"更有排查价值。
		label := t
		if item, ok := articleComponentLabels[t]; ok {
			label = tr(item.Key, item.Fallback)
		}
		view.Types = append(view.Types, articleImportTypeView{Type: t, Label: label, Count: counts[t]})
	}
	for _, w := range res.Warnings {
		action := string(w.Action)
		if item, ok := articleImportActionLabels[w.Action]; ok {
			action = tr(item.Key, item.Fallback)
		}
		view.Warnings = append(view.Warnings, articleImportWarningView{Tag: w.Tag, Action: action, Detail: w.Detail})
	}
	return view
}

// articleImportFacingError page 模块错误 → 可展示文案（先查导入白名单，再落统一内部错误）。
func articleImportFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	if msg, ok := articleImportFacingMessages[raw]; ok {
		return msg
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if msg, ok := articleImportFacingMessages[strings.TrimSpace(raw[:idx])]; ok {
			return msg
		}
	}
	if msg := articleFacingText(c, raw); msg != "" {
		return msg
	}
	// 未命中：原文只进日志（带 user_id），对外给归口文案。
	return articleInternalText(c, err)
}

// articleImportFacingMessages 导入流程可展示的文案白名单（page 模块错误常量的值）。
var articleImportFacingMessages = map[string]string{
	pageenums.ErrInvalidParam:    "提交的信息不完整，请检查工程与页面路径后重试。",
	pageenums.ErrProjectNotFound: "选择的站点工程不存在，请刷新页面后重试。",
	pageenums.ErrInvalidKind:     "页面类型与内容目标不匹配（导入用的是普通页面类型），这是程序错误，请联系管理员。",
	pageenums.ErrInvalidDocument: "生成的页面草稿不合法，请联系管理员（这是导入器的缺陷）。",
	pageenums.ErrInvalidPath:     "页面路径不合法：只能包含字母、数字、连字符与斜杠。",
	pageenums.ErrPathOccupied:    "这个页面路径已经被占用了，换一个（例如在末尾加 -2）。",
	articleImportEmptyBodyText:   articleImportEmptyBodyText,
	articleImportNoProjectText:   articleImportNoProjectText,
	articleImportNoPathText:      articleImportNoPathText,
	articleImportNoPageText:      articleImportNoPageText,
	articleImportDepsText:        articleImportDepsText,
	articleMissingIDText:         articleMissingIDText,
}

// ArticleNewPage GET /admin/articles/new：文章新建整页。
//
// 新建页不取数（标题 / 路径全空、正文空），只渲染表单；预览由前端 live-preview.js
// 从 Trix 编辑器同步，服务端零往返。Form 键集与编辑页 articleEditPageData 的 form
// 对齐（模板点号取值，缺键会让 Jet 报错并截断整页）。
func (h *articlePageHandle) ArticleNewPage(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/content/article_new.html", shell.Prepare(c, gin.H{
		"title": articleNewTitle,
		"menu":  "articles",
		"Form": gin.H{
			"ID": "", "Slug": "", "Title": "", "Body": "",
			"Revision": int64(0), "UpdatedAt": "",
		},
	}))
}

// article_page_data.go - 文章管理页的数据装配（列表/编辑页数据、表单绑定与导入块视图）。

// articleForm 编辑表单的字段集合（与 content 字段白名单一一对应）。
//
// 不含 seoTitle / seoDescription：两者已与 title / excerpt 合并（2026-09-30），
// 编辑页不再有这两个输入框，落库时由 data() 直接取标题与摘要。
type articleForm struct {
	ID            string
	Slug          string
	Title         string
	Body          string
	Excerpt       string
	FeaturedImage string
	FocusKeyword  string
}

// articleFormOf 从 POST 表单读字段。
//
// body 过富文本白名单（唯一来源 core.SanitizeRichHTML）：Trix 提交的是 HTML，
// 直接落库等于把「谁能写 script」这个问题交给前端 —— 清洗必须在写入口做。
func articleFormOf(c *gin.Context) articleForm {
	return articleForm{
		ID:            strings.TrimSpace(c.PostForm("id")),
		Slug:          strings.TrimSpace(c.PostForm("slug")),
		Title:         strings.TrimSpace(c.PostForm("title")),
		Body:          core.SanitizeRichHTML(c.PostForm("body")),
		Excerpt:       strings.TrimSpace(c.PostForm("excerpt")),
		FeaturedImage: strings.TrimSpace(c.PostForm("featuredImage")),
		FocusKeyword:  strings.TrimSpace(c.PostForm("focusKeyword")),
	}
}

// data 转成内容实体字段表。
//
// 空字符串**照写不落**（与「清空这个字段」是同一个意思）：字段白名单里的字段
// 全部提交，用户删掉的内容才会真的被删掉；写成「空值跳过」的话，编辑者永远删不掉
// 一个已填的摘要 —— 那是比多写几行空字符串严重得多的 bug。
//
// seoTitle / seoDescription 与 title / excerpt 合并（2026-09-30）：这里直接把标题与
// 摘要写进那两个键，历史数据里遗留的旧 SEO 值会在下一次保存时被覆盖 ——
// 与读侧口径（service/content_resolver.go 的归一）一致。
func (f articleForm) data() map[string]any {
	return map[string]any{
		"title":          f.Title,
		"body":           f.Body,
		"excerpt":        f.Excerpt,
		"featuredImage":  f.FeaturedImage,
		"seoTitle":       f.Title,
		"seoDescription": f.Excerpt,
		"focusKeyword":   f.FocusKeyword,
	}
}

// articleListPageData 列表页渲染数据（纯函数：不取数、不依赖 gin.Context）。
//
// 只保留 Err 一处提示来源（列表取数失败）：写动作的结论走提示页，不再经
// ?ok= / ?done= 回带（见 content_err.go）。
func articleListPageData(list []*contentdto.ContentResp, published map[string]string, pageErr string,
	trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	rows := make([]gin.H, 0, len(list))
	for _, it := range list {
		rows = append(rows, articleListRow(tr, it, published[it.ID]))
	}
	return gin.H{
		"title":          articlePageTitle,
		"menu":           "articles",
		"Rows":           rows,
		"Total":          len(rows),
		"Empty":          len(rows) == 0,
		"Err":            pageErr,
		"BlogBase":       articleBlogPathPrefix,
		"Keyword":        "",
		"ClearFilterURL": "/admin/articles",
		"Limit":          articleListPageSize,
		// FilterQuery 写动作表单 action 的筛选 query（ArticlesPage 里覆盖成预编码串）。
		// 给零值是为了模板渲染路径变窄时也不缺键（纯函数测试直接渲染模板时不带它）。
		"FilterQuery": "",
	}
}

// articleListRow 一篇文章 → 表格行。
//
// 发布状态只有两种取值来源：查到了线上路径（已发布）或没查到（未发布 / 未建实例）。
// 不区分「未发布」与「查询失败」—— 列表页不是排查页，编辑页会给出完整状态。
func articleListRow(tr func(key, fallback string) string, it *contentdto.ContentResp, urlPath string) gin.H {
	published := strings.TrimSpace(urlPath) != ""
	return gin.H{
		"ID":         it.ID,
		"Title":      articleTextOrEmpty(articleStr(it.Data, "title")),
		"Slug":       articleTextOrEmpty(it.Slug),
		"Excerpt":    articleTextOrEmpty(articleStr(it.Data, "excerpt")),
		"Revision":   it.Revision,
		"UpdatedAt":  it.UpdatedAt,
		"Published":  published,
		"URLPath":    urlPath,
		"PublicURL":  articlePublicURL(urlPath),
		"EditURL":    articleEditURL(it.ID),
		"StateLabel": articleStateLabel(tr, published),
	}
}

// articleStateLabel 列表页的发布状态文案（key + 中文兜底，取词在调用点）。
func articleStateLabel(tr func(key, fallback string) string, published bool) string {
	if published {
		return tr(contentenums.StatePublished, "已发布")
	}
	return tr(contentenums.StateUnpublished, "未发布")
}

// articleEditPageData 编辑页渲染数据。
//
// item 为 nil 表示新建（表单全空）；id 非空但 item 为 nil 表示读取失败
// （pageErr 已带上原因），此时仍渲染空表单让编辑者能重新保存。
//
// Err 的写侧只剩一处：articleUpdateFailure 原地重渲时带回的保存错误。成功回执
// 不再进模板（走提示页，见 content_err.go），所以没有 Ok 键。
func articleEditPageData(ctx context.Context, h *articlePageHandle, item *contentdto.ContentResp,
	id string, pageErr, lang string, trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	data := gin.H{}
	if item != nil {
		data = item.Data
	}
	form := gin.H{
		"ID":            id,
		"Slug":          articleSlugOf(item),
		"Title":         articleStr(data, "title"),
		"Body":          articleStr(data, "body"),
		"Excerpt":       articleStr(data, "excerpt"),
		"FeaturedImage": articleStr(data, "featuredImage"),
		"FocusKeyword":  articleStr(data, "focusKeyword"),
		"Revision":      articleRevisionOf(item),
		"UpdatedAt":     articleUpdatedAtOf(item),
	}
	out := gin.H{
		"title":           articleEditTitle,
		"menu":            "articles",
		"IsNew":           item == nil && id == "",
		"Form":            form,
		"Err":             pageErr,
		"InvalidField":    "",
		"EditUnavailable": false,
		"ListURL":         "/admin/articles",
		// TemplateEditURL 必须在**任何装配状态下**都存在：模板里是 {{if .TemplateEditURL}}，
		// 而 Jet 对缺失的键报错并截断整页（下面是装配成功时才会覆盖它）。
		"TemplateEditURL": "",
		// 初始评分：已保存的正文直接算一遍，编辑者打开页面就能看到当前水平
		// （改动后按「重新评分」走 HTMX 片段，见 ArticleScorePanel）。
		"Score": articleScoreViewOf(data, articlePreviewURL(articleSlugOf(item)), lang, tr),
	}
	// 工程列表查一次、两个区块共用（发布区块与导入区块都要它）。
	projectOptions := articleProjectOptions(ctx, h)
	for k, v := range articlePublishView(ctx, h, id, articleSlugOf(item), projectOptions, tr) {
		out[k] = v
	}
	for k, v := range articleImportBlockView(h, item, id, projectOptions, tr) {
		out[k] = v
	}
	if h != nil && h.templates != nil && id != "" {
		if resolved, err := h.templates.ResolveTemplate(ctx, articleEntityType); err == nil {
			projectID := ""
			if len(projectOptions) > 0 {
				if pid, ok := projectOptions[0]["ID"].(string); ok {
					projectID = pid
				}
			}
			out["TemplateEditURL"] = articleTemplateEditURL(resolved.TemplateID, articleEntityType, id, projectID)
		}
	}
	return out
}

// articleTemplateEditURL 文章详情模板的可视化编辑入口（/workbench?template=…）。
//
// 与 contenttemplate 页面的 workbenchTemplateURL 是同一个工作台入口的两种调用方
// （那边从模板列表进、这边从文章的编辑页进），各自持一份构造：
// 两个模块分属不同的 inbound 包，共用一个 URL 构造只会让其中一个模块反向依赖另一个的 HTTP 层。
func articleTemplateEditURL(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	q.Set("entityType", entityType)
	q.Set("entityId", entityID)
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return "/workbench?" + q.Encode()
}

// articleImportBlockView 「导入到画布」区块的渲染数据（纯组装，不取数）。
//
// 三个按钮的可用性条件必须在这里判清楚：一个点了会 500 的按钮比不给按钮更糟。
// 新建中（还没有文章 id）、没有工程、页面能力未装配 —— 三种情况各给各的说法。
func articleImportBlockView(h *articlePageHandle, item *contentdto.ContentResp, id string,
	projectOptions []gin.H, trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	if id == "" {
		return articleImportUnavailable(tr(contentenums.ImportHintSaveFirst, "先保存这篇文章，再回来把它导入画布。"))
	}
	if h == nil || h.pages == nil {
		return articleImportUnavailable(articleImportDepsText)
	}
	if len(projectOptions) == 0 {
		return articleImportUnavailable(tr(contentenums.ImportHintNoProject, "还没有站点工程：先在「页面」里建一个工程，导入需要知道页面挂到哪个站。"))
	}
	// 默认路径 /article-<slug>：这里**刻意不走**站点 URL 规则（siteurl）——
	// 导入生成的是一个**手工页面**，页面路径本身就是它的身份（没有 slug 可依），
	// 走文章详情页的模式反而会得到一个"看起来像文章详情页"的页面路径。
	// slug 为空时给一个能直接改的占位，不留空表单。
	slug := articleSlugOf(item)
	defaultPath := articleImportPathPrefix + "new"
	if slug != "" {
		defaultPath = articleImportPathPrefix + slug
	}
	out := articleImportUnavailable("")
	out["ImportAvailable"] = true
	out["ImportProjects"] = projectOptions
	out["ImportDefaultPath"] = defaultPath
	out["ImportHasPublished"] = articleStr(itemData(item), "body") != ""
	out["ImportPreviewTarget"] = "#article-import-result"
	return out
}

// articleImportUnavailable 导入区块的不可用形态：**键集与可用形态完全一致**。
//
// 理由同 articlePublishUnavailable：模板用点号取值，缺键会让 Jet 报错并截断整页。
func articleImportUnavailable(hint string) gin.H {
	return gin.H{
		"ImportAvailable":     false,
		"ImportHint":          hint,
		"ImportProjects":      []gin.H{},
		"ImportDefaultPath":   "",
		"ImportHasPublished":  false,
		"ImportPreviewTarget": "",
	}
}

// itemData 取实体字段（item 为空时给空表，避免调用方到处判空）。
func itemData(item *contentdto.ContentResp) map[string]any {
	if item == nil || item.Data == nil {
		return map[string]any{}
	}
	return item.Data
}

// firstNonEmpty 取第一个非空字符串（返回值不做 Trim，与 dashboard 版一致 ——
// 调用方拿到的就是原值，判定用的是 Trim 后的结果）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyString 取第一个非空值（返回 Trim 后的结果）。
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// contentText 取内容实体数据里的一个字符串字段（非字符串按空处理）。
func contentText(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// hasMarkup 是否含 HTML 标签（与构建期 core.HasRichMarkup 同一判据的轻量版）。
func hasMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

// translationLangOption 工作台语言下拉项。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// scoreView SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type scoreView struct {
	OK       bool
	Total    int
	Grade    string
	Sections []scoreSectionView
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重（页面草稿与文章）。
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

// 评分颜色 → 等级文案的映射**只有一份**，在 seoscore.ScoreGradeText（internal/seo/score_grade.go）。
// 此前本文件与 project/inbound/http/settings_panel.go 各持一份逐字相同的副本，
// 且两处注释都写着“正确的归宿是 seoscore 包” —— 已按此收编，取词改调该出口。

// staleImpactPageLimit 影响面清单一次列出的页面数（与 /admin/pages 的 staleOverviewLimit 同值）。
//
// 这是**消费者口径**，所以定义在调用方：ListStalePages 的 limit 由调用方给，
// 不填时落到 model 的 50 条默认 —— 一份 50 行的清单即使折叠着也会让人觉得「影响面很大」。
// 只读区块的作用是让人**看见**影响面，不是给出完整清单；被截断的条数由 Total 给出并在页面上说明。
const staleImpactPageLimit = 8

// staleImpactView 组装「待重建页面影响面」的渲染数据（纯函数，取数在 articleStaleImpact）。
//
// 统一返回 gin.H 而不是结构体：模板里是 map 取值链（.StaleImpact.Pages），
// 嵌套 map 在 Jet 上的求值路径最短、也最容易用 isset 判存在。
func staleImpactView(available bool, pages []gin.H, total int, truncated bool, hint string) gin.H {
	if pages == nil {
		pages = []gin.H{}
	}
	return gin.H{
		"Available": available,
		"Pages":     pages,
		"Total":     total,
		"Truncated": truncated,
		"Limit":     staleImpactPageLimit,
		"Hint":      hint,
	}
}

// articleStaleImpact 取「全站待重建」影响面的数据（只读观测）。
//
// 取数走 page 契约的 ListStalePages：它与 /admin/pages 的「全站待重建」区块是**同一个查询**，
// 三处（pages / blocks / articles）因此显示同一份数与同一份清单。此前 content 与 block 各自
// 逐工程 List 再自行截断 —— 同一个「待重建」概念有三份实现，三处的数并不保证相同。
// 这份契约就是 block 侧那份注释里说的「要么放进 page 模块」的落点。
//
// 降级语义与 page 侧一致：失败 → Available=false（显示「读不到」），**绝不**渲染成
// 「0 个待重建」（那会把一次读取失败伪装成一切正常）；契约返回 (nil, nil) 是异常形态，
// 同样按读不到处理。
func articleStaleImpact(ctx context.Context, h *articlePageHandle, trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	if h == nil || h.pages == nil {
		// 与 block 侧的待重建影响面文案**共用同一批 key**（两个页面说的是同一件事，
		// 各写一份的下场是同一个现象在两页上有两种说法 —— 见本文件顶部注释）。
		return staleImpactView(false, nil, 0, false,
			tr(blockenums.ImpactUnavailableNoPageCapability, "页面能力未装配（装配层未把 page 契约传给块管理页），本次无法统计待重建影响面。"))
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		Limit:      staleImpactPageLimit,
		Descending: true,
	})
	if err != nil {
		// 工程表为空时契约返回 ErrProjectRequired（没有可作用域的工程）：这里与真正的读取失败
		// 合并显示为「读不到」。page 侧能把它单独当作空态，是因为它同模块可直接引用该哨兵；
		// 跨模块 import page/service 是禁止的（AGENTS.md 模块边界），所以这里不做区分。
		logger.Scene("content").Error(err, "读取全站待重建清单失败，待重建影响面本次不可用")
		return staleImpactView(false, nil, 0, false,
			tr(blockenums.ImpactUnavailableProjectReadFailed, "读取站点工程失败，本次无法统计待重建影响面。"))
	}
	if res == nil {
		logger.Scene("content").Warn("全站待重建清单返回空结果（契约实现异常）")
		return staleImpactView(false, nil, 0, false, "")
	}
	pages := make([]gin.H, 0, len(res.Pages))
	for i := range res.Pages {
		// 转成 gin.H 而不是把契约 DTO 直接交给模板：模板的取值链是 .StaleImpact.Pages[i].Path，
		// map 缺键能被 isset 兜住，而契约 DTO 将来改名/加字段会直接打断整页渲染。
		pages = append(pages, gin.H{
			"ID":          res.Pages[i].ID,
			"Path":        res.Pages[i].Path,
			"ProjectID":   res.Pages[i].ProjectID,
			"ProjectName": res.Pages[i].ProjectName,
		})
	}
	return staleImpactView(true, pages, res.Total, res.Truncated, "")
}

// ArticleStaleDrawer 待重建页面清单的**只读抽屉**片段（GET /admin/articles/stale/drawer）。
//
// 与页头徽章同源（同一个 ListStalePages）：徽章给「有几个」，抽屉给「是哪几个」。
// 清单收进抽屉之后，文章列表页的首屏只留一行可点的徽章。
//
// 取不到数据一律只给状态码，不拼半截片段：drawer.js 对非 200 显示「加载失败，请重试」，
// 而缺 data-drawer-fragment 或缺列的片段会被它的 fragmentRoot 校验判非法 ——
// 两者在用户眼里是同一个失败界面，但后者还多花一次渲染。
func (h *articlePageHandle) ArticleStaleDrawer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.pages == nil {
		c.Status(http.StatusNotFound)
		return
	}
	tr := shell.TranslateFor(c)
	res, err := h.pages.ListStalePages(c.Request.Context(), &pagecontract.StalePageListReq{
		Limit:      staleImpactPageLimit,
		Descending: true,
	})
	if err != nil {
		logger.Scene("content").Error(err, "读取全站待重建清单失败（抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	if res == nil {
		logger.Scene("content").Warn("全站待重建清单返回空结果（契约实现异常，抽屉片段）")
		c.Status(http.StatusNotFound)
		return
	}
	rows := make([]gin.H, 0, len(res.Pages))
	// 工程数决定「所属工程」列显不显示：单工程时那一列整列都是同一个名字，
	// 是纯噪声；多工程时缺了它又分不清两个工程同名的 /about。
	projects := make(map[string]struct{}, 2)
	for i := range res.Pages {
		projects[res.Pages[i].ProjectID] = struct{}{}
		rows = append(rows, gin.H{
			"ID":          res.Pages[i].ID,
			"Path":        res.Pages[i].Path,
			"ProjectName": res.Pages[i].ProjectName,
			// Published 区分「已发布但有更新」与「从未上线」：后者的下一步不是重建而是发布。
			"Published": res.Pages[i].Published,
			// 失败痕迹（迁移 474）：非空说明这页**不是还没轮到，而是重建失败过**。
			"FailedNote": rebuildFailureNote(tr, res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}
	c.HTML(http.StatusOK, "admin/partials/stale_pages_drawer.html", shell.Prepare(c, gin.H{
		"Rows": rows, "Total": res.Total, "Truncated": res.Truncated,
		"Limit": staleImpactPageLimit, "MultiProject": len(projects) > 1,
	}))
}

// rebuildFailureNote 组装「最近一次自动重建失败」的一句文案；没有失败痕迹时返回空串。
//
// 阶段与时刻来自迁移 474 落在 pages 上的两列。**不含错误原文** —— 原文可能带 SQL / 路径 /
// 内部标识，后台页面不得直出内部错误（AGENTS.md 红线），它只进结构化日志（带 page_id 可定位）；
// 这里给的是「失败在哪一步、什么时候」，让人知道下一步去哪查。
//
// 与 /admin/blocks、/admin/pages 上同名函数是三份：它们各自在自己的模块包内（跨模块共用
// 要走契约，而这是纯展示装配）。三处文案与阶段取值必须一致，改一处请同步另两处。
func rebuildFailureNote(tr func(key, fallback string) string, stage string, at *utils.JSONTime) string {
	if strings.TrimSpace(stage) == "" && at == nil {
		return ""
	}
	label := stage
	switch stage {
	case "plan":
		label = tr("admin.pages.impact.stage_plan", "计划阶段（站点语言清单或旧发布范围读不到）")
	case "build":
		label = tr("admin.pages.impact.stage_build", "构建 / 发布阶段")
	}
	prefix := tr("admin.pages.impact.rebuild_failed", "最近一次自动重建失败：")
	if at == nil {
		return prefix + label
	}
	return prefix + label + "（" + time.Time(*at).Local().Format("2006-01-02 15:04") + "）"
}

// articleTranslationRow 工作台一行（一个可翻译取值）。
type articleTranslationRow struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	// Rich 富文本字段（body）：译文需与原文同形。
	Rich bool
}

// articleTranslationGroup 按文章分组的行集合。
type articleTranslationGroup struct {
	Key        string
	EntityType string
	EntityID   string
	Title      string
	Subtitle   string
	Rows       []articleTranslationRow
}

// articleTranslationHandle 文章翻译工作台。
type articleTranslationHandle struct {
	contents contentcontract.ContentService
	writer   *i18n.ContentWriter
}

// NewArticleTranslationHandle 构造。
func NewArticleTranslationHandle(contents contentcontract.ContentService) *articleTranslationHandle {
	return &articleTranslationHandle{contents: contents}
}

// SetContentWriter 注入译文写入端口（装配期）。
func (h *articleTranslationHandle) SetContentWriter(w *i18n.ContentWriter) { h.writer = w }

// articleFieldLabel 字段的展示名（工作台用）：字段名 → {i18n key, 中文兜底}。
var articleFieldLabel = map[string]articleTranslationText{
	"title":   {"admin.article.translations.field.title", "标题"},
	"body":    {"admin.article.translations.field.body", "正文"},
	"excerpt": {"admin.article.translations.field.excerpt", "摘要"},
	// seoTitle / seoDescription 的登记已删（2026-09-30 字段合并）：两个字段的值
	// 分别是 title / excerpt 的别名，已从可翻译字段清单里去掉（contentcontract），
	// 这里留着标签表只会让「有个能翻的 SEO 标题」看起来还存在。
}

// articleTranslationText 一条待取词文案（key + 中文兜底）。
type articleTranslationText struct{ Key, Fallback string }

// articleTranslationTextField 字段展示名 → 当前语言文案（未登记字段原样回显字段名，便于排查）。
func articleTranslationTextField(tr func(key, fallback string) string, field string) string {
	if item, ok := articleFieldLabel[field]; ok {
		return tr(item.Key, item.Fallback)
	}
	return field
}

// articleTranslationTextOf 本页文案的当前语言文本（key 直接给，兜底在下方 map 里）。
func articleTranslationTextOf(c *gin.Context, key, fallback string) string {
	return shell.TranslateFor(c)(key, fallback)
}

// ArticleTranslations GET /admin/articles/translations。
func (h *articleTranslationHandle) ArticleTranslations(c *gin.Context) {
	data := h.build(c, strings.TrimSpace(c.Query("lang")), strings.TrimSpace(c.Query("project")), strings.TrimSpace(c.Query("keyword")))
	c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
}

// SaveArticleTranslations POST /admin/articles/translations/save（整表提交）。
func (h *articleTranslationHandle) SaveArticleTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	lang := strings.TrimSpace(c.PostForm("lang"))
	projectID := strings.TrimSpace(c.PostForm("project"))
	keyword := strings.TrimSpace(c.PostForm("keyword"))
	page, _ := strconv.Atoi(c.PostForm("page"))
	data := h.build(c, lang, projectID, keyword)
	// 保存校验必须使用提交时的同一页，否则第二页的行会被误判为原文已变化。
	if page != data.Page || c.PostForm("limit") != strconv.Itoa(data.Limit) {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.pageChanged", "列表页码已变化，请刷新后重试")}
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.rowCountMismatch", "提交的行数不一致，请刷新后重试")}
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if h.writer == nil {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.storageUnavailable", "译文存储不可用")}
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	keys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.err.sourceChanged", "{context}：原文已变化，请刷新后重试",
				map[string]string{"context": contextName}))
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue // 空输入 = 本行不写入（不动库中已有译文）
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if verr := validateArticleTarget(contextName, source, target); verr != "" {
			// verr 是**词条 key**：直接拼到页面上会显示裸 key，必须取词（兜底在下方那句中文里）。
			rowErrors = append(rowErrors, i18n.FillTranslate(shell.TranslateFor(c), verr, articleTranslationFallbacks[verr],
				map[string]string{"context": contextName}))
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.Source, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		keys = append(keys, key)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		articleTranslationDone(c, projectID, lang, keyword, data.Page, data.Limit, 0)
		return
	}

	// 第二步：只写真正变化的行（重复保存零写入、不推进 revision）。
	hashesAll := make([]string, 0, len(items))
	for _, it := range items {
		hashesAll = append(hashesAll, it.SourceHash)
	}
	before, berr := h.writer.LoadDetails(ctx, lang, hashesAll)
	if berr != nil {
		logger.Scene("content").With("lang", lang).Error(berr, "读取现有文章译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changed := make([]string, 0, len(items))
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
		changed = append(changed, keys[i])
	}
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("content").
				With("lang", lang).
				With("user_id", shell.CurrentUserID(c)).
				Error(uerr, "写入文章译文失败")
			// 这一处已记过带 lang 的日志：文案出口用不记日志的版本，避免同一错误记两条。
			data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.err.saveFailed", "保存失败：{detail}",
				map[string]string{"detail": articleFacingOrInternal(c, uerr)})}
			c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
			return
		}
	}
	articleTranslationDone(c, projectID, lang, keyword, data.Page, data.Limit, written)
}

// articleTranslationDone 译文保存的结论：提示页，回跳只带筛选。
func articleTranslationDone(c *gin.Context, projectID, lang, keyword string, page, limit, written int) {
	var msg string
	if written > 0 {
		msg = i18n.FillTranslate(shell.TranslateFor(c),
			"admin.article.translations.saved", "已保存 {count} 条译文（下次构建生效）。",
			map[string]string{"count": strconv.Itoa(written)})
	} else {
		msg = articleTranslationTextOf(c, "admin.article.translations.savedNone", "没有需要写入的变化。")
	}
	shell.RenderJump(c, shell.Jump{
		OK:       true,
		Msg:      msg,
		Back:     articleTranslationLocation(projectID, lang, keyword, page, limit),
		BackText: shell.TranslateFor(c)("admin.article.translations.heading", "文章翻译"),
		Seconds:  1,
	})
}

// articleTranslationsData 页面数据。
type articleTranslationsData struct {
	Title      string
	Menu       string
	Lang       string
	Keyword    string
	Page       int
	Limit      int
	ProjectID  string
	HasData    bool
	Pagination gin.H
	Langs      []translationLangOption
	Groups     []articleTranslationGroup
	RowCount   int
	Done       int
	Total      int
	Saved      bool
	SavedNote  string
	Errors     []string
}

// templateMap 转小写键 map（layout 以 {{.title}} / {{.menu}} 取值）。
func (d *articleTranslationsData) templateMap() gin.H {
	out := gin.H{
		"title": d.Title, "menu": d.Menu,
		"Lang": d.Lang, "Langs": d.Langs, "Groups": d.Groups,
		"Keyword": d.Keyword, "Page": d.Page, "Limit": d.Limit, "ProjectID": d.ProjectID, "HasData": d.HasData,
		"RowCount": d.RowCount, "Done": d.Done, "Total": d.Total,
		"Saved": d.Saved, "SavedNote": d.SavedNote, "Errors": d.Errors,
	}
	for k, v := range d.Pagination {
		out[k] = v
	}
	return out
}

// sourceOf 按语境 + hash 反查本次提交对应的原文（防表单被裁剪 / 篡改）。
func (d *articleTranslationsData) sourceOf(contextName, hash string) (articleTranslationRow, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.Context == contextName && r.SourceHash == hash {
				return r, true
			}
		}
	}
	return articleTranslationRow{}, false
}

// articleTranslationFallbacks 校验类文案 key → 中文兜底（写成一张表而不是散在 return 里：
// 取词点要拿同一个兜底，两处各写一份就会「词条改了、兜底没改」）。
var articleTranslationFallbacks = map[string]string{
	"admin.article.translations.err.contextInvalid":  "语境非法：不是文章的可翻译字段",
	"admin.article.translations.err.notTranslatable": "原文不参与翻译（空串、纯数字或纯符号）",
	"admin.article.translations.err.richMismatch":    "正文译文的形态与原文不一致：原文含 HTML 标签时译文也必须含标签",
}

// validateArticleTarget 校验一条文章译文的可写性（与构建期同源）。
func validateArticleTarget(contextName string, source articleTranslationRow, target string) string {
	entityType, field, ok := i18n.ParseContentContext(contextName)
	if !ok || !contentcontract.IsTranslatableField(entityType, field) {
		return "admin.article.translations.err.contextInvalid"
	}
	if !i18n.ShouldTranslateContent(source.Source) {
		return "admin.article.translations.err.notTranslatable"
	}
	if source.Rich && hasMarkup(source.Source) != hasMarkup(target) {
		return "admin.article.translations.err.richMismatch"
	}
	return ""
}

// articleTranslationLocation 译文工作台的回跳地址：只带筛选，不带结论文案。
func articleTranslationLocation(projectID, lang, keyword string, page, limit int) string {
	q := url.Values{"lang": {lang}}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit != 20 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return "/admin/articles/translations?" + q.Encode()
}

// build 组装工作台数据：文章列表 → 可翻译字段 → 现有译文。
//
// 译文**批量预载**（LoadTargets 一次查询）再回填：按行逐条查会随文章数放大成 N 次 SQL，
// 而工作台一屏就要列出 50 篇的全部可翻译字段。
func (h *articleTranslationHandle) build(c *gin.Context, lang, projectID, keyword string) *articleTranslationsData {
	ctx := c.Request.Context()
	page, limit := shell.PageParams(c)
	if c.Request.Method == http.MethodPost {
		page, _ = strconv.Atoi(c.PostForm("page"))
		limit, _ = strconv.Atoi(c.PostForm("limit"))
		if limit < 1 || limit > 100 {
			limit = 20
		}
	}
	if page < 1 {
		page = 1
	}
	data := &articleTranslationsData{
		Title: "admin.article.translations.heading", Menu: "article-translations", Lang: lang,
		Keyword: keyword, Page: page, Limit: limit, ProjectID: projectID,
		Groups: []articleTranslationGroup{},
	}
	for _, code := range i18n.AvailableLangs() {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}
	if lang == "" {
		return data
	}
	if h.contents == nil {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.depsMissing", "内容模块未装配")}
		return data
	}
	filter := &contentdto.ListReq{EntityType: "article", Keyword: keyword}
	total, err := h.contents.Count(ctx, filter)
	if err != nil {
		data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
			"admin.article.translations.err.countFailed", "读取文章总数失败：{detail}",
			map[string]string{"detail": articleFacingError(c, err)})}
		return data
	}
	data.HasData = total > 0
	if keyword != "" && total == 0 {
		// 空匹配与真空数据不同：额外计数只在筛选零结果时执行。
		unfiltered, countErr := h.contents.Count(ctx, &contentdto.ListReq{EntityType: "article"})
		if countErr != nil {
			data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.err.countFailed", "读取文章总数失败：{detail}",
				map[string]string{"detail": articleFacingError(c, countErr)})}
			return data
		}
		data.HasData = unfiltered > 0
	}
	page = articleClampPage(page, limit, total)
	data.Page = page
	filter.Limit, filter.Offset = limit, (page-1)*limit
	list, err := h.contents.List(ctx, filter)
	if err != nil {
		// 形态③（模板数据 Errors）：err.Error() 直接拼进来会把 PG 原文摆到页面上。
		data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
			"admin.article.translations.err.listFailed", "读取文章列表失败：{detail}",
			map[string]string{"detail": articleFacingError(c, err)})}
		return data
	}
	data.Pagination = shell.BuildPagination(total, page, limit,
		shell.FilterBaseURL("/admin/articles/translations", map[string]string{
			"lang": lang, "project": projectID, "keyword": keyword, "limit": strconv.Itoa(limit),
		}), shell.TranslateFor(c)).TemplateKeys()
	fields := contentcontract.TranslatableFields("article")
	// 记录每一行在工作台里的位置：回填译文时要按 (组下标, 行下标) 写回。
	type rowRef struct {
		groupIdx int
		rowIdx   int
		hash     string
		context  string
	}
	var refs []rowRef
	groups := make([]articleTranslationGroup, 0, len(list))
	for _, item := range list {
		group := articleTranslationGroup{
			Key: item.ID, EntityType: "article", EntityID: item.ID,
			Subtitle: item.Slug, Rows: []articleTranslationRow{},
		}
		if t, ok := item.Data["title"].(string); ok {
			group.Title = t
		}
		for _, f := range fields {
			src, ok := item.Data[f].(string)
			if !ok || !i18n.ShouldTranslateContent(src) {
				continue
			}
			contextName := i18n.ContentContext("article", f)
			h := i18n.ContentHash(src)
			refs = append(refs, rowRef{
				groupIdx: len(groups), rowIdx: len(group.Rows), hash: h, context: contextName,
			})
			group.Rows = append(group.Rows, articleTranslationRow{
				Context: contextName, Field: f, FieldLabel: articleTranslationTextField(shell.TranslateFor(c), f),
				Source: src, SourceHash: h, Rich: f == "body",
			})
		}
		data.RowCount += len(group.Rows)
		data.Total++
		groups = append(groups, group)
	}
	data.Groups = groups
	// 预载现有译文并回填（writer 未注入时跳过：工作台仍可看原文，只是显示不出已有译文）。
	if h.writer != nil && len(refs) > 0 {
		hashes := make([]string, 0, len(refs))
		for _, r := range refs {
			hashes = append(hashes, r.hash)
		}
		targets, lerr := h.writer.LoadTargets(ctx, lang, hashes)
		if lerr != nil {
			logger.Scene("content").With("lang", lang).Error(lerr, "读取现有文章译文失败")
		} else {
			for _, r := range refs {
				if t, ok := targets[i18n.ContentIndexKey(r.hash, r.context)]; ok && t != "" {
					data.Groups[r.groupIdx].Rows[r.rowIdx].Target = t
					data.Groups[r.groupIdx].Rows[r.rowIdx].Translated = true
					data.Done++
				}
			}
		}
	}
	return data
}

// articleErrScene 日志场景名（与 content 模块其它 logger.Scene("content") 一致）。
const articleErrScene = "content"

// —— 数据取值 ——

// articleStr 取内容实体字段里的字符串值（非字符串按空处理）。
func articleStr(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// articleTextOrEmpty 空值统一显示成占位符（表格空白单元格读不出「没有值」）。
func articleTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return articleEmptyField
	}
	return value
}

// articleSlugOf / articleRevisionOf / articleUpdatedAtOf 实体 → 表单字段。
//
// item 为 nil（新建，或读取失败）时给零值：表单仍可渲染与提交。
func articleSlugOf(item *contentdto.ContentResp) string {
	if item == nil {
		return ""
	}
	return item.Slug
}

func articleRevisionOf(item *contentdto.ContentResp) int64 {
	if item == nil {
		return 0
	}
	return item.Revision
}

func articleUpdatedAtOf(item *contentdto.ContentResp) string {
	if item == nil {
		return ""
	}
	return item.UpdatedAt
}

// —— 路径 ——

// articleEditURL 文章编辑页地址。
func articleEditURL(id string) string {
	return "/admin/articles/edit?" + url.Values{"id": {id}}.Encode()
}

// articlePreviewURL 文章的线上路径（评分器的 URL 检查用）。
//
// 未发布时用默认博客前缀拼一个「将来会是什么样」的地址：URL 相关检查看的是
// 路径形态（层级、长度、是否含参数），不是它现在能否打开。
func articlePreviewURL(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return articleBlogPathPrefix + slug
}

// articleDefaultURLPath 发布区块的路径默认值（可改）。
func articleDefaultURLPath(slug string) string {
	return articlePreviewURL(slug)
}

// articlePublicURL 站点内逻辑路径 → 浏览器可打开的访问面地址。
//
// 与系统页面槽位页同一口径：active_path 恒带前导 "/"，这里仍做一次归一 ——
// 拼出 "/siteblog/x" 这种地址的错法是静默的（链接能渲染、点了才 404）。
func articlePublicURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return "/site/" + strings.TrimPrefix(p, "/")
}

// —— 发布状态 ——

// articlesPublished 批量取「文章 id → 线上路径」，只含真正已发布的。
//
// 逐条查而不是一次查全表：presentation 契约按实体查询（GetByEntity）是既有口径，
// 为列表页新增一个「按类型列实例」的批量入口会把只读端口撑宽。
// 列表已限 50 条，最多 50 次主键查询，换掉一次契约扩张是划算的。
//
// 查询失败按「未发布」处理：列表页不是排查页，不让一条查询失败把整页打成错误页。
func articlesPublished(ctx context.Context, port articlePublishPort, list []*contentdto.ContentResp) map[string]string {
	out := map[string]string{}
	if port == nil {
		return out
	}
	for _, it := range list {
		res, err := port.GetByEntity(ctx, &presentationdto.GetByEntityReq{
			EntityType: articleEntityType, EntityID: it.ID,
		})
		if err != nil || res == nil {
			continue
		}
		if strings.TrimSpace(res.URLPath) != "" {
			out[it.ID] = res.URLPath
		}
	}
	return out
}

// —— 评分视图 ——

// scoreViewFromResult 评分结果 → 视图（不含 SERP 预览：那部分由调用方按自己的
// 标题/描述来源填）。页面设置面板与文章编辑页共用这一份转换 —— 两处各写一遍的话，
// 「哪些检查项算未达标」这种判断会分叉。
func scoreViewFromResult(res *scoring.Result, trs ...func(key, fallback string) string) scoreView {
	tr := articlePublishTr(trs)
	if res == nil {
		return scoreView{}
	}
	sv := scoreView{OK: true, Total: res.Total, Grade: res.Grade}
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
				Text: i18n.FillTranslate(tr, projectenums.SEOScoreIssueFormat, "{label}：{actual}（基准 {benchmark}）→ {hint}",
					map[string]string{"label": ck.Label, "actual": ck.Actual, "benchmark": ck.Benchmark, "hint": ck.Hint}),
				Target: ck.Target,
			})
		}
		sv.Sections = append(sv.Sections, item)
	}
	return sv
}

// articleScoreViewOf 文章字段 → 评分视图（编辑页初始渲染与评分片段共用）。
func articleScoreViewOf(data map[string]any, articleURL, lang string,
	trs ...func(key, fallback string) string) scoreView {
	tr := articlePublishTr(trs)
	sv := scoreViewFromResult(seoscore.ScoreArticle(data, articleURL, lang), tr)
	if !sv.OK {
		return sv
	}
	// SERP 预览取的就是标题与摘要（2026-09-30 字段合并）：seoTitle / seoDescription
	// 已与它们合并，这里不再做「SEO 字段优先」的二段取值。
	sv.SerpTitle = articleStr(data, "title")
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(contentenums.ScoreSerpTitleEmpty, "（未填写文章标题）")
	}
	sv.SerpDesc = articleStr(data, "excerpt")
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(contentenums.ScoreSerpDescEmpty, "（未填写摘要 / SEO 描述）")
	}
	sv.SerpURL = articleURL
	if sv.SerpURL == "" {
		sv.SerpURL = articleBlogPathPrefix + "example"
	}
	return sv
}

// —— 文案归口 ——
//
// 写动作的回跳与提示页出口在 content_err.go（articlePageJump / articleListJump /
// articleEditJump）；本段只保留错误 → 可展示文案的归口漏斗。
// 原先的 articleRedirectList / articleRedirectEdit / articleRedirect（302 + ?err= / ?ok=）
// 已整批删除：结论改由提示页在响应体里渲染。

// articleInternalText 未命中任何白名单时的统一出口（错误文案三件套的第三件）：
// 原文只进日志（场景 + user_id + 原始错误），对外给归口文案。
//
// 页面上出现 "pq: duplicate key value violates unique constraint" 或
// `relation "contents" does not exist` 既看不懂，也把库表结构泄了出去 ——
// 写动作的结论现在由提示页在响应体里渲染（content_err.go），模板数据 Errors 与
// 错误原文一样不是可信边界。
func articleInternalText(c *gin.Context, err error) string {
	if err != nil {
		logger.Scene(articleErrScene).
			With("user_id", shell.CurrentUserID(c)).
			Error(err, "content 后台页操作失败（非业务错误，只对外给归口文案）")
	}
	return shell.PageInternalText(c)
}

// articleFacingOrInternal 不记日志的文案出口：调用点已经记过一条更具体的日志
// （带 lang）时用它，免得同一个错误在日志里出现两遍。
func articleFacingOrInternal(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := articleFacingText(c, err.Error()); msg != "" {
		return msg
	}
	return shell.PageInternalText(c)
}

// articleFacingError 把 content 契约的错误转成可展示文案（未命中 → 日志 + 归口文案）。
func articleFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := articleFacingText(c, err.Error()); msg != "" {
		return msg
	}
	return articleInternalText(c, err)
}

// articleErrControlledPrefixes 受控提示的前缀白名单（**按当前语言生成**）。
//
// 它们不是 enums key（因此进不了 articleFacingMessages），但整句都由本仓库自己拼出：
// 不含表名 / SQLSTATE / 路径，且带着运营照着做的数字。目前只有一条 ——
// shell.BulkIDs 的上限拒绝（internal/shell/bulk.go 的 BulkIDsFacingText）。
//
// 前缀必须跟着语言算：shell 那条提示是**按请求语言**取词渲染的，写死中文前缀会让
// 英文后台下的这条受控提示被判成未命中 → 回落归口文案（用户看不到「分批做」这句可行动的话）。
// 取词用**同一个 key 与同一个兜底模板**（shell.MsgBulkIDsTooMany），
// 与 shell 的写侧同源，不另抄一份措辞。
//
// 按**前缀**判而不是按来源直接透出：上游将来改成上抛别的错误时前缀不再命中，
// 会自动退回归口文案 / 回显 fallback，不会把不认识的原文顺出去。
func articleErrControlledPrefixes(c *gin.Context) []string {
	tpl := shell.TranslateFor(c)(shell.MsgBulkIDsTooMany, "一次最多操作 %s 项")
	return []string{fmt.Sprintf(strings.ReplaceAll(tpl, "%s", "%d"), shell.MaxBulkIDs)}
}

// articleControlledText 受控提示 → 原样透出（保留可行动信息）；未命中返回空串。
func articleControlledText(c *gin.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, prefix := range articleErrControlledPrefixes(c) {
		if strings.HasPrefix(raw, prefix) {
			return raw
		}
	}
	return ""
}

// articleFacingText 白名单校验：命中返回可展示文案，未命中返回空串。
//
// content 契约有两类错误串：
//   - 裸常量（"ErrSlugTaken"）；
//   - 「常量: 明细」（"ErrInvalidField: "body""，validateData 拼的）。
//
// 第二类必须按前缀命中，只做精确匹配的话它们会全部落到统一内部错误 ——
// 运营看到「系统内部错误」而实际问题只是提交了一个不支持的字段。
func articleFacingText(c *gin.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 受控提示（shell.BulkIDs 的上限拒绝）放行：不是 enums key，但整句由本仓库拼出。
	// 批量删除的失败出口（ArticlesBulkDelete → articleListJump）靠这一份判据 ——
	// 少了它，「一次最多操作 10 项」会被自己的白名单吞掉（用户看不到任何提示）。
	if msg := articleControlledText(c, raw); msg != "" {
		return msg
	}
	tr := shell.TranslateFor(c)
	if fallback, ok := articleFacingMessages[raw]; ok {
		return tr(raw, fallback)
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if key := strings.TrimSpace(raw[:idx]); key != "" {
			if fallback, ok := articleFacingMessages[key]; ok {
				return tr(key, fallback)
			}
		}
	}
	return ""
}

// 发布区块的提示文案（同样登记在白名单里，因为它们会作为提示页正文渲染）。
// 本页文案的常量值是 **i18n key**，中文兜底在同文件的 articlePublishFacingMessages ——
// 两者成对，取词统一走 articlePublishTextOf / articlePublishHintText。
const (
	articlePublishedText      = "admin.article.publish.ok.published"
	articleRebuiltText        = "admin.article.publish.ok.rebuilt"
	articleURLUpdatedText     = "admin.article.publish.ok.urlUpdated"
	articleNoProjectText      = "admin.article.publish.err.noProject"
	articleNoURLPathText      = "admin.article.publish.err.noURLPath"
	articleNoTemplatePickText = "admin.article.publish.err.noTemplatePick"
	// articleNoTemplateHint 没有 article 模板时的出口说明（不含任何按钮）。
	articleNoTemplateHint = "admin.article.publish.hint.noTemplate"
	// articlePublishUnavailableText 发布能力未装配时的出口文案。
	articlePublishUnavailableText = "admin.article.publish.err.unavailable"
	// articlePublishDepsMissingText 发布能力未装配（装配缺陷）的就地说明。
	articlePublishDepsMissingText = "admin.article.publish.err.depsMissing"
	// articleNewPathRequiredText 改 URL 时没填新路径。
	articleNewPathRequiredText = "admin.article.publish.err.newPathRequired"
	// articleSaveFirstHint 还没有实体时的发布区块提示。
	articleSaveFirstHint = "admin.article.publish.hint.saveFirst"
)

// articlePublishTextOf 本页文案的当前语言文本（key + 白名单里的中文兜底）。
//
// 只留这一个取词出口：各写一份中文的下场是「词条改了、兜底没改」，
// 页面上时而译文时而旧中文，而两处都不报错。
func articlePublishTextOf(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, articlePublishFacingMessages[key])
}

// articlePublishHintText 发布区块提示的取词（可选变参 tr：区块装配在无 gin.Context 的路径上）。
func articlePublishHintText(tr func(key, fallback string) string, key string) string {
	return tr(key, articlePublishFacingMessages[key])
}

// articlePublishTr 取词函数的可选变参：不传时原样返回兜底文案。
func articlePublishTr(trs []func(key, fallback string) string) func(key, fallback string) string {
	if len(trs) > 0 && trs[0] != nil {
		return trs[0]
	}
	return func(_, fallback string) string { return fallback }
}

// articlePublishFacingMessages 发布流程可展示的文案白名单。
//
// 与文章页那份分开：presentation 的 ErrNotFound 指「实例不存在」，content 的同名常量
// 指「文章不存在」—— 同一个 key 在两处含义不同，合并成一张表必然吃掉一边。
var articlePublishFacingMessages = map[string]string{
	presentationenums.ErrInvalidParam:         "发布参数不完整，请检查工程、路径与模板。",
	presentationenums.ErrNotFound:             "这篇文章还没有详情页实例，先发布一次。",
	presentationenums.ErrNoTemplate:           "该类型没有可用的内容模板，先建一套文章详情模板。",
	presentationenums.ErrEntityMissing:        "文章不存在，可能已被删除。",
	presentationenums.ErrBuildFailed:          "构建失败，请检查模板与文章正文后重试。",
	presentationenums.ErrProjectRequired:      "站点里有多个工程，请显式选择这篇所属的工程。",
	presentationenums.ErrProjectNotFound:      "选择的站点工程不存在，请刷新后重试。",
	presentationenums.ErrRegistryMissing:      "实体类型注册表未装配（装配缺陷），请联系管理员。",
	presentationenums.ErrTemplateTypeMismatch: "这套模板不是文章类型的，换一套再试。",
	presentationenums.ErrInvalidPath:          "访问路径不合法：必须以 / 开头，且不含空格、引号与 .. 路径段。",
	presentationenums.ErrSamePath:             "新路径与当前路径相同，没有需要修改的地方。",
	presentationenums.ErrPathOccupied:         "这个路径已被其他页面或详情页占用，换一个再试。",
	articlePublishedText:                      "已发布。文章详情页已上线，访问面立即可见。",
	articleRebuiltText:                        "已重新发布。原路径的产物已更新。",
	articleURLUpdatedText:                     "已改 URL。新路径已上线，旧路径按你的选择处理（301 跳转或直接失效）。",
	articleNoProjectText:                      "请先选择这篇文章属于哪个站点工程。",
	articleNoURLPathText:                      "请填写文章详情页的访问路径。",
	articleNoTemplatePickText:                 "请选择一套文章详情模板。",
	articleNoTemplateHint:                     "这个站还没有「文章详情模板」（entityType=article 的内容模板），所以现在没有东西可以渲染这篇文章。模板的建立与内容编辑链路目前没有后台入口，需要先建一套 article 类型的内容模板，再回来发布。",
	articlePublishUnavailableText:             "发布能力未装配，请联系管理员。",
	articlePublishDepsMissingText:             "发布能力未装配（装配缺陷），本页只显示文章内容。",
	articleNewPathRequiredText:                "请填写新的访问路径。",
	articleSaveFirstHint:                      "先保存这篇文章，再回来看发布状态。",
}

// ArticlePublish 首次发布文章详情页（POST /admin/articles/publish）。
func (h *articlePageHandle) ArticlePublish(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	urlPath := strings.TrimSpace(c.PostForm("urlPath"))
	templateID := strings.TrimSpace(c.PostForm("templateId"))

	if h.instances == nil {
		articleEditJump(c, false, id, articlePublishTextOf(c, articlePublishUnavailableText))
		return
	}
	if projectID == "" {
		articleEditJump(c, false, id, articlePublishTextOf(c, articleNoProjectText))
		return
	}
	if urlPath == "" {
		articleEditJump(c, false, id, articlePublishTextOf(c, articleNoURLPathText))
		return
	}
	if !strings.HasPrefix(urlPath, "/") {
		// 路径必须从根开始：CreateInstance 直接把它写进 URL 占用表，
		// 一个不带前导斜杠的路径会占用一个永远打不开的名额。
		urlPath = "/" + urlPath
	}
	if _, err := h.instances.CreateInstance(c.Request.Context(), &presentationdto.CreateInstanceReq{
		EntityType: articleEntityType, EntityID: id,
		URLPath: urlPath, ProjectID: projectID, TemplateID: templateID,
	}); err != nil {
		articleEditJump(c, false, id, articlePublishFacingError(c, err))
		return
	}
	articleEditJump(c, true, id, articlePublishTextOf(c, articlePublishedText))
}

// ArticleRebuild 重新发布（POST /admin/articles/rebuild）。
//
// 沿用实例当前绑定的模板重建；文章内容本身改了以后由依赖扇出自动标记待重建，
// 这里的按钮是「我想立刻看到结果」的手动出口。
func (h *articlePageHandle) ArticleRebuild(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if h.instances == nil {
		articleEditJump(c, false, id, articlePublishTextOf(c, articlePublishUnavailableText))
		return
	}
	if _, err := h.instances.Rebuild(c.Request.Context(), &presentationdto.RebuildReq{EntityID: id}); err != nil {
		articleEditJump(c, false, id, articlePublishFacingError(c, err))
		return
	}
	articleEditJump(c, true, id, articlePublishTextOf(c, articleRebuiltText))
}

// ArticleUpdateURL 修改已发布文章详情页的线上路径（POST /admin/articles/url）。
//
// 正文、SEO 字段、模板绑定一律不动 —— 改 URL 只重建产物并把旧链接按策略处置
// （勾选 = 301，不勾 = 直接失效）。这是「路径是站点事实、不是内容的一部分」
// 在后台的出口：改标题不会动 URL，改 URL 也不需要重新编辑文章。
func (h *articlePageHandle) ArticleUpdateURL(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if h.instances == nil {
		articleEditJump(c, false, id, articlePublishTextOf(c, articlePublishUnavailableText))
		return
	}
	newPath := strings.TrimSpace(c.PostForm("newPath"))
	if newPath == "" {
		articleEditJump(c, false, id, articlePublishTextOf(c, articleNewPathRequiredText))
		return
	}
	if _, err := h.instances.UpdateURL(c.Request.Context(), &presentationdto.UpdateURLReq{
		EntityType:   articleEntityType,
		EntityID:     id,
		NewPath:      newPath,
		WithRedirect: c.PostForm("withRedirect") != "",
	}); err != nil {
		articleEditJump(c, false, id, articlePublishFacingError(c, err))
		return
	}
	articleEditJump(c, true, id, articlePublishTextOf(c, articleURLUpdatedText))
}

// articlePublishView 组装发布区块渲染数据（纯函数，取数在 articlePublishViewData）。
//
// id 为空（新建中）时不查任何东西：还没有实体，发布无从谈起。
func articlePublishView(ctx context.Context, h *articlePageHandle, id, slug string, projectOptions []gin.H,
	trs ...func(key, fallback string) string) gin.H {
	tr := articlePublishTr(trs)
	if strings.TrimSpace(id) == "" {
		return articlePublishUnavailable(articlePublishHintText(tr, articleSaveFirstHint))
	}
	if h.instances == nil {
		return articlePublishUnavailable(articlePublishHintText(tr, articlePublishDepsMissingText))
	}

	out := articlePublishUnavailable("")
	out["PublishConfigured"] = true
	inst, err := h.instances.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: articleEntityType, EntityID: id,
	})
	if err == nil && inst != nil && strings.TrimSpace(inst.URLPath) != "" {
		out["Published"] = true
		out["URLPath"] = inst.URLPath
		out["PublicURL"] = articlePublicURL(inst.URLPath)
		out["Stale"] = inst.Stale
		out["Status"] = inst.Status
		return out
	}
	out["Published"] = false
	// 默认路径按站点 URL 规则派生（取表单里第一个工程；用户可改工程、也可直接改路径）。
	// 派生只是预填 —— 派生不出来时回落到一个不会撞车的占位，让用户自己写。
	out["DefaultURLPath"] = articleDetailDefaultPath(ctx, h, projectOptions, slug, id)
	out["Projects"] = projectOptions
	templates := articleTemplateOptions(ctx, h)
	out["Templates"] = templates
	out["HasTemplates"] = len(templates) > 0
	out["NoTemplateHint"] = articlePublishHintText(tr, articleNoTemplateHint)
	return out
}

// articlePublishUnavailable 发布区块的不可用形态：**键集与可用形态完全一致**。
//
// 模板对这些可选区块用点号取值（{{.URLPath}} / {{.PublicURL}} / {{.HasTemplates}}），
// 而 Jet 遇到缺失的键不是渲染成空，而是报错并**截断整页输出** —— 编辑页会只剩
// 上半截、状态码仍是 200，看起来像「样式坏了」，极难联想到是少了一个键。
// 因此键齐全由数据侧保证，模板不再为「键可能不存在」写分支。
func articlePublishUnavailable(hint string) gin.H {
	return gin.H{
		"PublishConfigured": false,
		"PublishHint":       hint,
		"Published":         false,
		"URLPath":           "",
		"PublicURL":         "",
		"Stale":             false,
		"Status":            "",
		"DefaultURLPath":    "",
		"Projects":          []gin.H{},
		"Templates":         []gin.H{},
		"HasTemplates":      false,
		"NoTemplateHint":    "",
	}
}

// articleDetailDefaultPath 文章详情页的默认发布路径：按 URL 规则派生，派生不出来时兜底。
//
// 兜底用 /article-<slug>（而不是 /blog/<slug>）：规则派生不出来说明"这个站没给文章配模式"，
// 此时猜一个常见前缀反而可能撞上真实存在的列表页（博客列表常占 /blog）——
// 撞车的表现是发布被拒，而用户根本不知道是"默认值"的错。
func articleDetailDefaultPath(ctx context.Context, h *articlePageHandle,
	projectOptions []gin.H, slug, id string) string {
	if len(projectOptions) > 0 {
		if pid, ok := projectOptions[0]["ID"].(string); ok && pid != "" {
			if p := shell.SiteDetailPath(ctx, h.projects, pid, siteurl.KindArticle, slug, id); p != "" {
				return p
			}
		}
	}
	if slug == "" {
		return articleImportPathPrefix + "new"
	}
	return articleImportPathPrefix + slug
}

// articleProjectOptions 工程下拉（发布时必须落到一个工程：实例表的 project_id 非空）。
func articleProjectOptions(ctx context.Context, h *articlePageHandle) []gin.H {
	out := []gin.H{}
	if h.projects == nil {
		return out
	}
	list, err := h.projects.List(ctx)
	if err != nil {
		return out
	}
	for _, p := range list {
		out = append(out, gin.H{"ID": p.ID, "Name": p.Name})
	}
	return out
}

// articleTemplateOptions 文章类型的内容模板下拉（空表 = 这个站还没有文章详情模板）。
func articleTemplateOptions(ctx context.Context, h *articlePageHandle) []gin.H {
	out := []gin.H{}
	if h.templates == nil {
		return out
	}
	list, err := h.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: articleEntityType})
	if err != nil {
		return out
	}
	for _, t := range list {
		out = append(out, gin.H{"ID": t.ID, "Name": t.Name, "Version": t.DraftVersion})
	}
	return out
}

// articlePublishFacingError 发布错误 → 可展示文案（先查发布白名单，再查文章白名单）。
func articlePublishFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	if msg, ok := articlePublishFacingMessages[raw]; ok {
		return msg
	}
	if idx := strings.IndexByte(raw, ':'); idx > 0 {
		if msg, ok := articlePublishFacingMessages[strings.TrimSpace(raw[:idx])]; ok {
			return msg
		}
	}
	if msg := articleFacingText(c, raw); msg != "" {
		return msg
	}
	// 未命中：原文只进日志（带 user_id），对外给归口文案。
	return articleInternalText(c, err)
}

// articlePublishURL 发布 / 重建表单的动作地址（当前页路径，集中一处便于改名）。
const (
	articlePublishPath  = "/admin/articles/publish"
	articleRebuildPath  = "/admin/articles/rebuild"
	articleListPath     = "/admin/articles"
	articleEditPagePath = "/admin/articles/edit"
)
