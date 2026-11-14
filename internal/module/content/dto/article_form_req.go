package contentdto

import (
	"strings"

	"go_wp/pkg/richtext"
)

// ArticleFormReq 是文章新建 / 更新的表单（标题、正文、摘要与 SEO 字段）。
//
// 字段名与模板控件逐字一致；正文在绑定后过富文本白名单，其余字段去首尾空白。
// 放在 DTO 里而不是控制器里：同一条表单会被新建、更新与 SEO 评分三个端点复用，
// 清洗与取值的口径只能有一份。
type ArticleFormReq struct {
	ID            string `form:"id"`
	Slug          string `form:"slug"`
	Title         string `form:"title"`
	Body          string `form:"body"`
	Excerpt       string `form:"excerpt"`
	FeaturedImage string `form:"featuredImage"`
	FocusKeyword  string `form:"focusKeyword"`

	// URL 只被 SEO 评分端点读取（评分看的是「文章挂在哪个路径」）。
	URL string `form:"url"`
}

// Normalize 统一清洗正文并去掉其余字段的首尾空白。
//
// 显式方法而不是 pkg/binding 的 FormBinder 钩子：普通表单走 Gin 原生 FormPost 绑定，
// 清洗由控制器在绑定成功后显式调用，绑定层不再承担第二次解析。
func (r *ArticleFormReq) Normalize() {
	r.ID = strings.TrimSpace(r.ID)
	r.Slug = strings.TrimSpace(r.Slug)
	r.Title = strings.TrimSpace(r.Title)
	r.Body = richtext.SanitizeRichHTML(r.Body)
	r.Excerpt = strings.TrimSpace(r.Excerpt)
	r.FeaturedImage = strings.TrimSpace(r.FeaturedImage)
	r.FocusKeyword = strings.TrimSpace(r.FocusKeyword)
	r.URL = strings.TrimSpace(r.URL)
}

// Data 把表单投影成内容实体的数据字段。
func (r *ArticleFormReq) Data() map[string]any {
	return map[string]any{
		"title":          r.Title,
		"body":           r.Body,
		"excerpt":        r.Excerpt,
		"featuredImage":  r.FeaturedImage,
		"seoTitle":       r.Title,
		"seoDescription": r.Excerpt,
		"focusKeyword":   r.FocusKeyword,
	}
}

// ArticleDeleteReq 是单条删除 / 重新发布表单：只带一个 id。
type ArticleDeleteReq struct {
	ID string `form:"id"`
}

// ArticleBulkDeleteReq 是文章批量删除表单：ids 按真实字段重复提交。
type ArticleBulkDeleteReq struct {
	IDs []string `form:"ids"`
}

// ArticleImportReq 是「正文导入画布」的表单：预览取 id，创建取工程与新页面路径。
type ArticleImportReq struct {
	ID        string `form:"id"`
	ProjectID string `form:"projectId"`
	DraftPath string `form:"draftPath"`
}

// ArticleLinkReq 是内链建议的表单：当前文章、工程、主关键词与正文已用链接（每行一条）。
type ArticleLinkReq struct {
	ID           string `form:"id"`
	ProjectID    string `form:"projectId"`
	FocusKeyword string `form:"focusKeyword"`
	Linked       string `form:"linked"`
}

// Normalize 去掉短字段的首尾空白；linked 是整段文本，只在这里统一裁剪。
func (r *ArticleLinkReq) Normalize() {
	r.ID = strings.TrimSpace(r.ID)
	r.ProjectID = strings.TrimSpace(r.ProjectID)
	r.FocusKeyword = strings.TrimSpace(r.FocusKeyword)
	r.Linked = strings.TrimSpace(r.Linked)
}

// ArticlePublishReq 是发布文章详情页的表单。
type ArticlePublishReq struct {
	ID         string `form:"id"`
	ProjectID  string `form:"projectId"`
	URLPath    string `form:"urlPath"`
	TemplateID string `form:"templateId"`
}

// ArticleURLReq 是修改文章访问路径的表单。
type ArticleURLReq struct {
	ID           string `form:"id"`
	NewPath      string `form:"newPath"`
	WithRedirect bool   `form:"withRedirect"`
}

// ArticleTranslationsSaveReq 是文章译文保存表单：上下文 / 原文哈希 / 译文三列按行重复提交。
//
// Page / Limit 保持字符串：回跳页码是「和当前页比对」的预置值，
// 非数字输入按原逻辑落到「页码已变化」的受控提示，而不是绑定失败。
type ArticleTranslationsSaveReq struct {
	Lang    string `form:"lang"`
	Project string `form:"project"`
	Keyword string `form:"keyword"`
	Page    string `form:"page"`
	Limit   string `form:"limit"`

	RowContext []string `form:"rowContext"`
	RowHash    []string `form:"rowHash"`
	RowTarget  []string `form:"rowTarget"`
}

// Normalize 只去掉列表预置项的首尾空白；语言与工程标识由调用方进一步校验。
func (r *ArticleTranslationsSaveReq) Normalize() {
	r.Lang = strings.TrimSpace(r.Lang)
	r.Project = strings.TrimSpace(r.Project)
	r.Keyword = strings.TrimSpace(r.Keyword)
	for i := range r.RowContext {
		r.RowContext[i] = strings.TrimSpace(r.RowContext[i])
	}
}
