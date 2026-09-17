package contenthttp

// article_handle.go — 文章管理页处理器（原 dashboard/inbound/http/article_handle.go，INF-1）。
//
// 搬迁只做三件事：包名换成 contenthttp；壳层调用换成 shell.*；
// 页面标题常量由 dashboard enums 换成同值的 i18n key 字面量（词条表键不变）。
// 取数逻辑、权限点、模板名一律未动。

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
)

const (
	// articleEntityType contents 表当前唯一合法类型（迁移 080 的 CHECK 约束）。
	articleEntityType = "article"
	// articleListLimit 列表一次取多少条。
	//
	// 后台文章列表不分页：列表页还要逐条查发布状态，条数直接决定查询次数；
	// 文章量级远小于商品，50 条足够覆盖日常，超过这个量级再谈分页。
	articleListLimit = 50
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
// 只列本页真正用到的方法：读绑定、首次发布、重建、改 URL。不直接依赖
// presentationcontract 全量契约（它还带着删除、切换模板等本页不碰的写能力）。
type articlePublishPort interface {
	GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
	// UpdateURL 改 URL（发布后换路径）：新路径激活 + 旧路径 301 / 取消激活。
	UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error)
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
func (h *articlePageHandle) ArticlesPage(c *gin.Context) {
	ctx := c.Request.Context()
	pageErr := articleQueryText(c, c.Query("err"), shell.PageInternalText(c))
	pageOk := articleQueryText(c, c.Query("ok"), "")

	list, err := h.contents.List(ctx, &contentdto.ListReq{EntityType: articleEntityType, Limit: articleListLimit})
	if err != nil {
		pageErr = firstNonEmpty(pageErr, articleFacingError(c, err))
	}
	c.HTML(http.StatusOK, "admin/articles.html",
		shell.Prepare(c, articleListPageData(list, articlesPublished(ctx, h.instances, list), pageErr, pageOk)))
}

// ArticleEditPage 文章编辑页（GET /admin/articles/edit?id=）；不带 id 即新建。
func (h *articlePageHandle) ArticleEditPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.TrimSpace(c.Query("id"))
	pageErr := articleQueryText(c, c.Query("err"), shell.PageInternalText(c))
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
		shell.Prepare(c, articleEditPageData(ctx, h, item, id, pageErr, pageOk, requestScoreLang(c))))
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
		gin.H{"Score": articleScoreViewOf(data, strings.TrimSpace(c.PostForm("url")), requestScoreLang(c))})
}

// —— 表单与视图 ——
