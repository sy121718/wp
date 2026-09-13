package dashboardhttp

// article_handle.go — 后台文章管理页（INF-1）与编辑期 SEO 评分侧栏（SEO-10）。
//
// 为什么要有这一页：CMS 内容实体（contents 表，迁移 080 起只保留 article 一种类型）
// 此前只有 JSON API，没有后台界面 —— 发一篇文章得手搓 API 调用，于是内容模板、
// 文章集合（content:article）、文章详情模板这些**已经落地**的能力没有内容可渲染，
// 访问面的博客只能靠造数据。本文件补齐入口：列表 / 新建 / 编辑 / 删除 / 发布，
// 以及 SEO-10 要求的正文侧栏评测（密度 / 长度 / 可读性 / 内链）。
//
// 五条与 content 模块的约定：
//
//  1. 跨模块只依赖 contentcontract 与不可变 contentdto，不 import content 的
//     model / service。写操作直接调契约（本地调用），不绕回自己的 HTTP API。
//
//  2. **正文按富文本处理**：落库前过 core.SanitizeRichHTML（白名单的唯一来源），
//     与 workbench 的 ct:"richtext" 控件同一份清洗 —— 后台页不是绕过白名单的后门。
//
//  3. **权限点复用既有 content:* 系列**（迁移 033），本次不新增权限点、不新增迁移：
//     运营看到的是「文章」，服务端判定的是「内容」，两者是同一批权限点。
//
//  4. **写入即触发依赖扇出**（PIPE-3 / INF-2）：content 契约在 Create/Update/Delete
//     之后自己推导依赖源键并交给扇出端口，引用这篇文章的页面与已发布的详情实例会被
//     标记待重建。本页不需要（也不应该）自己做这件事 —— 绕开契约直接写库才会丢这个行为。
//
//  5. **评分不阻塞保存**：评分只是提示，没有「分数不够就不许存」的门槛。
//     草稿先存下来是常态，编辑者要能反复改。

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

const (
	// articleEntityType contents 表当前唯一合法类型（迁移 080 的 CHECK 约束）。
	articleEntityType = "article"
	// articleListLimit 列表一次取多少条。
	//
	// 后台文章列表不分页：列表页还要逐条查发布状态，条数直接决定查询次数；
	// 文章量级远小于商品，50 条足够覆盖日常，超过这个量级再谈分页。
	articleListLimit = 50
	// 页面标题（字面量走 withI18n 的 fallback 链路：t(标题, 标题) 回落原文，
	// 与系统页面槽位页、商品详情模板页同口径；不新增 dashboard enums 集合）。
	articlePageTitle = "文章"
	articleEditTitle = "编辑文章"
	articleNewTitle  = "新建文章"
	// articleEmptyField 空字段展示占位（表格空白单元格读不出「没有值」）。
	articleEmptyField = "—"
	// articleBlogPathPrefix 文章详情页的默认线上路径前缀（可在发布区块里改）。
	articleBlogPathPrefix = "/blog/"
)

// 成功回执（进 ?ok=，因此同样登记在白名单里）。
const (
	articleCreatedText = "文章已创建。写完正文后记得保存。"
	articleSavedText   = "已保存。引用这篇文章的页面与已发布的详情页已标记待重建。"
	articleDeletedText = "文章已删除。"
)

// articleFacingMessages 本页可以原样展示给运营的文案（白名单）。
//
// 键有两类：
//   - content 模块的错误常量值（常量值就是常量名本身，见 contentenums）；
//   - 本页自造的成功 / 参数级文案，值等于自身（它们会进 ?ok= / ?err=）。
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
	articleCreatedText:           articleCreatedText,
	articleSavedText:             articleSavedText,
	articleDeletedText:           articleDeletedText,
	articleMissingIDText:         "缺少文章标识，请回到列表页重试。",
	articleMissingTitleText:      "请先填写文章标题。",
	articleMissingSlugText:       "请先填写文章路径（slug）。",
}

// 参数级错误（本页自造；同样登记白名单 —— 自造文案不登记会在回显时被自己吞掉）。
const (
	articleMissingIDText    = "缺少文章标识，请回到列表页重试。"
	articleMissingTitleText = "请先填写文章标题。"
	articleMissingSlugText  = "请先填写文章路径（slug）。"
)

// articlePublishPort 发布区块所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本页真正用到的三个方法：读绑定、首次发布、重建。不直接依赖
// presentationcontract 全量契约（它还带着删除、切换模板等本页不碰的写能力）。
type articlePublishPort interface {
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
}

// articlePageHandle 文章管理页处理器。
type articlePageHandle struct {
	contents  contentcontract.ContentService
	projects  projectcontract.ProjectService
	templates contenttemplatecontract.ContentTemplateService
	// instances 发布能力（装配期注入；为空时发布区块整体不渲染）。
	instances articlePublishPort
}

// NewArticlePageHandle 构造。
func NewArticlePageHandle(contents contentcontract.ContentService, projects projectcontract.ProjectService,
	templates contenttemplatecontract.ContentTemplateService, instances articlePublishPort) *articlePageHandle {
	return &articlePageHandle{contents: contents, projects: projects, templates: templates, instances: instances}
}

// ArticlesPage 文章列表（GET /admin/articles）。
func (h *articlePageHandle) ArticlesPage(c *gin.Context) {
	ctx := c.Request.Context()
	pageErr := articleQueryText(c, c.Query("err"), articleInternalText(c))
	pageOk := articleQueryText(c, c.Query("ok"), "")

	list, err := h.contents.List(ctx, &contentdto.ListReq{EntityType: articleEntityType, Limit: articleListLimit})
	if err != nil {
		pageErr = firstNonEmpty(pageErr, articleFacingError(c, err))
	}
	c.HTML(http.StatusOK, "admin/articles.html",
		withCSRF(c, articleListPageData(list, articlesPublished(ctx, h.instances, list), pageErr, pageOk)))
}

// ArticleEditPage 文章编辑页（GET /admin/articles/edit?id=）；不带 id 即新建。
func (h *articlePageHandle) ArticleEditPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.Query("id"))
	pageErr := articleQueryText(c, c.Query("err"), articleInternalText(c))
	pageOk := articleQueryText(c, c.Query("ok"), "")

	var item *contentdto.ContentResp
	if id != "" {
		got, err := h.contents.Get(ctx, &contentdto.GetReq{ID: id})
		if err != nil {
			pageErr = firstNonEmpty(pageErr, articleFacingError(c, err))
		} else {
			item = got
		}
	}
	c.HTML(http.StatusOK, "admin/article_edit.html",
		withCSRF(c, articleEditPageData(ctx, h, item, id, pageErr, pageOk)))
}

// ArticleCreate 新建文章（POST /admin/articles/create，权限点 content:create）。
func (h *articlePageHandle) ArticleCreate(c *gin.Context) {
	form := articleFormOf(c)
	if form.Title == "" {
		articleRedirectList(c, articleMissingTitleText, "")
		return
	}
	if form.Slug == "" {
		articleRedirectList(c, articleMissingSlugText, "")
		return
	}
	res, err := h.contents.Create(c.Request.Context(), &contentdto.CreateReq{
		EntityType: articleEntityType, Slug: form.Slug, Data: form.data(),
	})
	if err != nil {
		articleRedirectList(c, articleFacingError(c, err), "")
		return
	}
	// 新建后直接进编辑页：列表页的表单只有标题与路径，正文要在这里写。
	articleRedirectEdit(c, res.ID, articleCreatedText, "")
}

// ArticleUpdate 保存文章（POST /admin/articles/update，权限点 content:update）。
func (h *articlePageHandle) ArticleUpdate(c *gin.Context) {
	form := articleFormOf(c)
	if form.ID == "" {
		articleRedirectList(c, articleMissingIDText, "")
		return
	}
	if form.Title == "" {
		articleRedirectEdit(c, form.ID, "", articleMissingTitleText)
		return
	}
	if _, err := h.contents.Update(c.Request.Context(), &contentdto.UpdateReq{ID: form.ID, Data: form.data()}); err != nil {
		articleRedirectEdit(c, form.ID, "", articleFacingError(c, err))
		return
	}
	articleRedirectEdit(c, form.ID, articleSavedText, "")
}

// ArticleDelete 删除文章（POST /admin/articles/delete，权限点 content:delete）。
func (h *articlePageHandle) ArticleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		articleRedirectList(c, articleMissingIDText, "")
		return
	}
	if err := h.contents.Delete(c.Request.Context(), &contentdto.DeleteReq{ID: id}); err != nil {
		articleRedirectList(c, articleFacingError(c, err), "")
		return
	}
	articleRedirectList(c, "", articleDeletedText)
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
	c.HTML(http.StatusOK, "fragments/seo_score",
		gin.H{"Score": articleScoreViewOf(data, strings.TrimSpace(c.PostForm("url")))})
}

// —— 表单与视图 ——

// articleForm 编辑表单的字段集合（与 content 字段白名单一一对应）。
type articleForm struct {
	ID             string
	Slug           string
	Title          string
	Body           string
	Excerpt        string
	FeaturedImage  string
	SEOTitle       string
	SEODescription string
	FocusKeyword   string
}

// articleFormOf 从 POST 表单读字段。
//
// body 过富文本白名单（唯一来源 core.SanitizeRichHTML）：Trix 提交的是 HTML，
// 直接落库等于把「谁能写 script」这个问题交给前端 —— 清洗必须在写入口做。
func articleFormOf(c *gin.Context) articleForm {
	return articleForm{
		ID:             strings.TrimSpace(c.PostForm("id")),
		Slug:           strings.TrimSpace(c.PostForm("slug")),
		Title:          strings.TrimSpace(c.PostForm("title")),
		Body:           core.SanitizeRichHTML(c.PostForm("body")),
		Excerpt:        strings.TrimSpace(c.PostForm("excerpt")),
		FeaturedImage:  strings.TrimSpace(c.PostForm("featuredImage")),
		SEOTitle:       strings.TrimSpace(c.PostForm("seoTitle")),
		SEODescription: strings.TrimSpace(c.PostForm("seoDescription")),
		FocusKeyword:   strings.TrimSpace(c.PostForm("focusKeyword")),
	}
}

// data 转成内容实体字段表。
//
// 空字符串**照写不落**（与「清空这个字段」是同一个意思）：字段白名单里的字段
// 全部提交，用户删掉的内容才会真的被删掉；写成「空值跳过」的话，编辑者永远删不掉
// 一个已填的 SEO 标题 —— 那是比多写几行空字符串严重得多的 bug。
func (f articleForm) data() map[string]any {
	return map[string]any{
		"title":          f.Title,
		"body":           f.Body,
		"excerpt":        f.Excerpt,
		"featuredImage":  f.FeaturedImage,
		"seoTitle":       f.SEOTitle,
		"seoDescription": f.SEODescription,
		"focusKeyword":   f.FocusKeyword,
	}
}

// articleListPageData 列表页渲染数据（纯函数：不取数、不依赖 gin.Context）。
func articleListPageData(list []*contentdto.ContentResp, published map[string]string, pageErr, pageOk string) gin.H {
	rows := make([]gin.H, 0, len(list))
	for _, it := range list {
		rows = append(rows, articleListRow(it, published[it.ID]))
	}
	return gin.H{
		"title":    articlePageTitle,
		"menu":     "articles",
		"Rows":     rows,
		"Total":    len(rows),
		"Empty":    len(rows) == 0,
		"Err":      pageErr,
		"Ok":       pageOk,
		"BlogBase": articleBlogPathPrefix,
	}
}

// articleListRow 一篇文章 → 表格行。
//
// 发布状态只有两种取值来源：查到了线上路径（已发布）或没查到（未发布 / 未建实例）。
// 不区分「未发布」与「查询失败」—— 列表页不是排查页，编辑页会给出完整状态。
func articleListRow(it *contentdto.ContentResp, urlPath string) gin.H {
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
		"StateLabel": articleStateLabel(published),
	}
}

// articleStateLabel 列表页的发布状态文案。
func articleStateLabel(published bool) string {
	if published {
		return "已发布"
	}
	return "未发布"
}

// articleEditPageData 编辑页渲染数据。
//
// item 为 nil 表示新建（表单全空）；id 非空但 item 为 nil 表示读取失败
// （pageErr 已带上原因），此时仍渲染空表单让编辑者能重新保存。
func articleEditPageData(ctx context.Context, h *articlePageHandle, item *contentdto.ContentResp,
	id string, pageErr, pageOk string) gin.H {
	data := gin.H{}
	if item != nil {
		data = item.Data
	}
	form := gin.H{
		"ID":             id,
		"Slug":           articleSlugOf(item),
		"Title":          articleStr(data, "title"),
		"Body":           articleStr(data, "body"),
		"Excerpt":        articleStr(data, "excerpt"),
		"FeaturedImage":  articleStr(data, "featuredImage"),
		"SEOTitle":       articleStr(data, "seoTitle"),
		"SEODescription": articleStr(data, "seoDescription"),
		"FocusKeyword":   articleStr(data, "focusKeyword"),
		"Revision":       articleRevisionOf(item),
		"UpdatedAt":      articleUpdatedAtOf(item),
	}
	out := gin.H{
		"title":   articleEditTitle,
		"menu":    "articles",
		"IsNew":   item == nil && id == "",
		"Form":    form,
		"Err":     pageErr,
		"Ok":      pageOk,
		"ListURL": "/admin/articles",
		// 初始评分：已保存的正文直接算一遍，编辑者打开页面就能看到当前水平
		// （改动后按「重新评分」走 HTMX 片段，见 ArticleScorePanel）。
		"Score": articleScoreViewOf(data, articlePreviewURL(articleSlugOf(item))),
	}
	for k, v := range articlePublishView(ctx, h, id, articleSlugOf(item)) {
		out[k] = v
	}
	return out
}
