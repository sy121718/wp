package contenthttp

// article_import.go — 把文章正文导入画布（06-B 决策 5 的第一个真实用途）。
//
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

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/richdoc"
	contentdto "go_wp/internal/module/content/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
)

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

// 导入相关文案（登记在白名单里 —— 它们会进 ?err=）。
const (
	articleImportEmptyBodyText = "这篇文章的正文是空的，没有可导入的内容。先写点东西再导入。"
	articleImportNoProjectText = "请先选择目标站点工程。"
	articleImportNoPathText    = "请填写新页面的访问路径。"
	articleImportNoPageText    = "导入需要页面能力（未装配），请联系管理员。"
	articleImportDepsText      = "页面能力未装配（装配缺陷），本页只显示文章内容。"
)

// articleImportPreviewView 预览视图（模板只做分支渲染，不做统计与判断 ——
// 统计口径只在这里定义一次，真实渲染测试喂同一份数据走同一条组装路径）。
type articleImportPreviewView struct {
	OK        bool
	NodeCount int
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
var articleComponentLabels = map[string]string{
	"core.heading":   "标题",
	"core.text":      "正文",
	"core.list":      "列表",
	"core.quote":     "引用",
	"core.image":     "图片",
	"core.divider":   "分隔线",
	"core.table":     "表格",
	"core.container": "容器",
}

// articleImportActionLabels 降级动作 → 中文（与 richdoc.WarningAction 一一对应）。
var articleImportActionLabels = map[richdoc.WarningAction]string{
	richdoc.ActionUnwrap:      "去壳保留内容",
	richdoc.ActionDrop:        "已丢弃",
	richdoc.ActionTrim:        "已裁剪",
	richdoc.ActionPlaceholder: "占位（不可还原）",
}

// ArticleImportPreview 预览转换结果（POST /admin/articles/import-preview）。
//
// 纯计算：不写库、不建页面、不改任何东西。读的是**已保存**的正文 ——
// 预览与创建必须基于同一份内容，否则"预览没问题的"和"创建出来的"会不一样。
func (h *articlePageHandle) ArticleImportPreview(c *gin.Context) {
	item, err := h.articleByID(c)
	if err != nil {
		c.HTML(http.StatusOK, "fragments/article_import",
			gin.H{"Preview": articleImportPreviewView{}, "Err": articleFacingError(c, err)})
		return
	}
	res, err := richdoc.HTMLToNodes(articleStr(item.Data, "body"))
	if err != nil {
		c.HTML(http.StatusOK, "fragments/article_import",
			gin.H{"Preview": articleImportPreviewView{}, "Err": articleFacingError(c, err)})
		return
	}
	c.HTML(http.StatusOK, "fragments/article_import",
		gin.H{"Preview": articleImportPreviewViewOf(res), "Err": ""})
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
		articleRedirectList(c, articleMissingIDText, "")
		return
	}
	if h.pages == nil {
		articleRedirectEdit(c, id, "", articleImportDepsText)
		return
	}
	if projectID == "" {
		articleRedirectEdit(c, id, "", articleImportNoProjectText)
		return
	}
	if draftPath == "" {
		articleRedirectEdit(c, id, "", articleImportNoPathText)
		return
	}
	item, err := h.contents.Get(c.Request.Context(), &contentdto.GetReq{ID: id})
	if err != nil {
		articleRedirectEdit(c, id, "", articleFacingError(c, err))
		return
	}
	res, err := richdoc.HTMLToNodes(articleStr(item.Data, "body"))
	if err != nil {
		articleRedirectEdit(c, id, "", articleFacingError(c, err))
		return
	}
	if len(res.Nodes) == 0 {
		articleRedirectEdit(c, id, "", articleImportEmptyBodyText)
		return
	}
	doc, err := articleImportDocument(item.Data, res.Nodes)
	if err != nil {
		articleRedirectEdit(c, id, "", articleFacingError(c, err))
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
		articleRedirectEdit(c, id, "", articleImportFacingError(c, err))
		return
	}
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
func articleImportDocument(data map[string]any, nodes []*core.Node) (json.RawMessage, error) {
	seo := map[string]any{"schemaType": "article"}
	if v := firstNonEmpty(articleStr(data, "seoTitle"), articleStr(data, "title")); v != "" {
		seo["title"] = v
	}
	if v := firstNonEmpty(articleStr(data, "seoDescription"), articleStr(data, "excerpt")); v != "" {
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
func articleImportPreviewViewOf(res *richdoc.Result) articleImportPreviewView {
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
		Lossless:  len(res.Warnings) == 0,
		Types:     make([]articleImportTypeView, 0, len(order)),
		Warnings:  make([]articleImportWarningView, 0, len(res.Warnings)),
	}
	for _, t := range order {
		label, ok := articleComponentLabels[t]
		if !ok {
			label = t // 认不出来的组件显示原始类型名：比显示"未知"更有排查价值
		}
		view.Types = append(view.Types, articleImportTypeView{Type: t, Label: label, Count: counts[t]})
	}
	for _, w := range res.Warnings {
		action, ok := articleImportActionLabels[w.Action]
		if !ok {
			action = string(w.Action)
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
	if msg := articleFacingText(raw); msg != "" {
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
