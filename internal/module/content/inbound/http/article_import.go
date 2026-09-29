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
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/richdoc"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/web/shell"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/i18n"
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
