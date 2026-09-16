package dashboardhttp

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	contentdto "go_wp/internal/module/content/dto"
)

// article_page_data.go - 文章管理页的数据装配（列表/编辑页数据、表单绑定与导入块视图）。

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
	id string, pageErr, pageOk, lang string) gin.H {
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
		// TemplateEditURL 必须在**任何装配状态下**都存在：模板里是 {{if .TemplateEditURL}}，
		// 而 Jet 对缺失的键报错并截断整页（下面是装配成功时才会覆盖它）。
		"TemplateEditURL": "",
		// 初始评分：已保存的正文直接算一遍，编辑者打开页面就能看到当前水平
		// （改动后按「重新评分」走 HTMX 片段，见 ArticleScorePanel）。
		"Score": articleScoreViewOf(data, articlePreviewURL(articleSlugOf(item)), lang),
	}
	// 工程列表查一次、两个区块共用（发布区块与导入区块都要它）。
	projectOptions := articleProjectOptions(ctx, h)
	for k, v := range articlePublishView(ctx, h, id, articleSlugOf(item), projectOptions) {
		out[k] = v
	}
	for k, v := range articleImportBlockView(h, item, id, projectOptions) {
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
			out["TemplateEditURL"] = workbenchTemplateURL(resolved.TemplateID, articleEntityType, id, projectID)
		}
	}
	return out
}

// articleImportBlockView 「导入到画布」区块的渲染数据（纯组装，不取数）。
//
// 三个按钮的可用性条件必须在这里判清楚：一个点了会 500 的按钮比不给按钮更糟。
// 新建中（还没有文章 id）、没有工程、页面能力未装配 —— 三种情况各给各的说法。
func articleImportBlockView(h *articlePageHandle, item *contentdto.ContentResp, id string,
	projectOptions []gin.H) gin.H {
	if id == "" {
		return articleImportUnavailable("先保存这篇文章，再回来把它导入画布。")
	}
	if h == nil || h.pages == nil {
		return articleImportUnavailable(articleImportDepsText)
	}
	if len(projectOptions) == 0 {
		return articleImportUnavailable("还没有站点工程：先在「页面」里建一个工程，导入需要知道页面挂到哪个站。")
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
