package service

// 这一组是「HTTP 之外的那一半」：读页面 / 主题 / 块 / 模板、把契约响应压成画布要的轻量形状、
// 拼预览查询串、算静态资源版本、算画布标题。它们原来散在 inbound/http 的三个文件里，
// 由 handler 直接编排（router.go 的 pageOf、workbench_handle.go 的 blockSummaries 等）。
// 搬到这里的判据是：**换一个入口（CLI / 后台任务 / 将来的画布 v2）也要用同一份口径** ——
// 尤其是 pageOf 的「先问归属再取详情」，它是一条越权防护，不该有两份实现。
//
// 本文件不认识 gin：ctx 由调用方传，取词由调用方给（Translate）。

// 背景：workbench.js 的 renderTree 用 100+ 行 DOM 代码递归建树并给每个节点绑 6 类事件。
// 本文件把「树 HTML」搬到服务端产出，客户端只保留一次事件委托
//（选中/拖拽/右键/重命名/caret 折叠），DOM 由服务端给出。
//
// 端点：POST /workbench/outline，参数 document（草稿 JSON）+ selectedId + filter。

// 原实现在 inbound/http 的 workbench_err.go 里：判据是**文档事实**与**模块哨兵 key**，
// 与 HTTP 无关 —— 只有「写什么状态码、取什么文案」是 HTTP 关切，那部分留在 handler。
//
// 三条口径各自独立，不要互相借判据：
//   - 编译失败（页面 / 块 / 结构模板）→ 文档事实（有没有字段绑定、有没有过校验）；
//   - 模板预览失败（有样例实体）→ 模块哨兵字符串；
//   - 实例覆盖文档保存失败 → 渲染链路的错误包装。

// 合并自原 inbound/http 的 inspector_handle.go / inspector_field_handle.go /
// 这些文件里与 HTTP 无关的部分（分组、字段构造、下拉取数、重复项骨架）整体下沉，
// handler 只留「解析请求 → 调本方法 → 渲染片段」。
//
// 取词：面板 HTML 由 Go 拼串产出，模板只做插槽，所以分组标题、字段标签、占位符、
// 按钮提示都必须在这里取词 —— 全部经 tr（Translate）。

// 面板 HTML 在这里生成，不再由浏览器拼 DOM：结构只有一处定义。客户端只做事件委托
// （改条目名、上移、下移、删除、添加）；需要读写文档 AST 的那一半（对齐语义 ——
// 增删条目时同步画布上对应的面板节点、分配节点 ID）仍留在客户端，服务端拿不到那份状态。
// 见 workbench/methods/controls/repeater.js 的 bindRepeaterPanel。
//
// 数据契约（客户端靠这些属性工作，改名要同步改绑定函数）：
//
//	data-wb-rep         根元素，值为组件类型
//	data-wb-rep-field   主字段键
//	data-wb-rep-input   主字段输入框，值为条目序号
//	data-wb-rep-extra   额外布尔字段键
//	data-wb-rep-index   条目序号
//	data-wb-rep-op      add / remove / move（move 另带 data-wb-rep-to）
//
// 与客户端渲染版的差别只在「结构由谁生成」：类名、按钮文案、提示语逐字一致，
// 否则同一次改动的产物与老路径会长得不一样，回归时看不出是哪种来源。
//
// **适用范围（别硬套）**：这里只覆盖「数组 ↔ 子节点一一对应」的对齐型面板 ——
// 折叠项数 = 面板数、页签数 = 面板数，增删条目必须同步增删画布节点（对齐语义在
// palette.js 的 alignMutation）。另两类面板不是这套模式：
//
//	普通数组（faq / social）  条目与子节点无关，增删只改 props；faq 的每行还挂一个
//	                        富文本答案字段（客户端增强控件），服务端只能给占位。
//	嵌套数组（nav）          条目带子项递归、两个字段、目标切换，行结构不是一维的。
//
// 支持范围由组件的 AlignedRepeaterProvider 声明，注册时核对真实 Props 类型；
// Go 面板与 generated-contracts.js 共用声明，不在工作台维护组件配置表。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/block/dto"
	"go_wp/internal/module/contenttemplate/dto"
	"go_wp/internal/module/contenttemplate/enums"
	"go_wp/internal/module/navigation/dto"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/workbench/enums"
)

// PageByID 按 id 读取页面（画布 / 预览共用同一取数口径）。
//
// Detail 把 projectID 当**必填的越权防护 scope**（少它只会得到「参数缺失」，
// 看起来像「页面不存在」）。画布路由手上只有 pageId，所以先用只读的
// ProjectOfPage 问「这个页面属于谁」，再按 scope 取详情。
func (s *Service) PageByID(ctx context.Context, pageID string) (*pagecontract.PageResp, error) {
	if s == nil || s.pages == nil {
		return nil, errors.New("页面服务未装配")
	}
	projectID, err := s.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return s.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

// ThemeIDOf 页面挂接的主题 ID（未挂接返回空串）。
func ThemeIDOf(page *pagecontract.PageResp) string {
	if page.ThemeID == "" {
		return ""
	}
	return page.ThemeID
}

// ThemeSettingsOf 页面挂接主题的 settings（colors/fontFamily 等），未挂接或查询失败返回 nil。
func (s *Service) ThemeSettingsOf(ctx context.Context, page *pagecontract.PageResp) json.RawMessage {
	if page.ThemeID == "" {
		return nil
	}
	theme, err := s.projects.GetTheme(ctx, page.ThemeID)
	if err != nil || theme == nil {
		return nil
	}
	return theme.Settings
}

// BlockSummaries 工程块列表的轻量投影（id/name/kind/category/reuseMode，不含文档大字段）。
// category 供 workbench 全局块按分类分组；reuseMode 供「引用/复制」双动作分流（docs/02-D §5.3）。
//
// 返回 []map[string]any 而不是 gin.H：这是契约无关的形状投影，service 不认识 gin。
// 值进 gin.H 后再 json.Marshal，字节与原实现一致。
func (s *Service) BlockSummaries(ctx context.Context, projectID string) []map[string]any {
	blocks, err := s.blocks.List(ctx, &blockcontract.ListReq{ProjectID: projectID})
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, map[string]any{"id": b.ID, "name": b.Name, "kind": b.Kind, "category": b.Category, "reuseMode": b.ReuseMode})
	}
	return out
}

// TemplateByID 按模板 id 取预览目标：优先用画布自己带过来的工程作用域
// （content_templates 带 FORCE 策略，作用域缺省时只能靠「工程唯一」解析）。
//
// 调用方负责先确认模板端口已装配（原 handler 的 previewTemplateTarget 就做这件事）。
func (s *Service) TemplateByID(ctx context.Context, templateID, projectID string) (*contenttemplatedto.TemplateResp, error) {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return s.contentTemplates.GetScoped(ctx, pid, templateID)
	}
	return s.contentTemplates.Get(ctx, &contenttemplatedto.GetReq{ID: templateID})
}

// PluginAssembly 启用插件装配素材（无插件模块契约或无启用插件时返回 nil）。
//
// 与原实现的差别：**去掉了单请求内缓存**（原 handler 把结果挂在 gin.Context 的
// "pluginAssembly" 键上）。全仓只有画布入口一处调用，一次请求最多取一次，
// 缓存从来没有命中过；而它把「一次取数」的语义藏进了 HTTP 上下文里。
// 真正需要复用的调用方自己存返回值即可。
func (s *Service) PluginAssembly(ctx context.Context) *plugincontract.Assembly {
	if s.plugins == nil {
		return nil
	}
	asm, err := s.plugins.EnabledAssembly(ctx)
	if err != nil {
		return nil
	}
	if asm != nil && (len(asm.PluginFS) > 0 || len(asm.Specs) > 0) {
		return asm
	}
	return nil
}

// TemplatePreviewQuery 模板画布 iframe 与「新标签预览」共用的查询串。
//
// entityId 为空（结构模板的无实体模式）时不带该参数：空串参数与服务端「缺参数」在
// 日志与排查里长得一样，少一个无意义的空参数省一次误判。
func TemplatePreviewQuery(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	q.Set("entityType", entityType)
	if entityID != "" {
		q.Set("entityId", entityID)
	}
	q.Set("editor", "1")
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return q.Encode()
}

// BlockByID 按 id 取全局块详情（块画布与块预览共用同一取数口径）。
//
// 与原 handler 内联调用等价：Detail 的越权防护 scope 由块的 ProjectID 承担，
// 这里没有额外的 `pageOf` 式两跳（block 契约的 Detail 只收 id）。
func (s *Service) BlockByID(ctx context.Context, blockID string) (*blockdto.BlockResp, error) {
	return s.blocks.Detail(ctx, &blockdto.DetailReq{ID: blockID})
}

// StaticJSVersion 工作台脚本缓存版本：取拆分后模块目录（static/js/workbench/**）
// 下所有 .js 的最新 mtime。任一模块改动都会让入口 URL 的 ?v= 变化，配合
// StaticCacheMiddleware 的协商缓存，浏览器不会再执行旧模块。
//
// 读的是工作目录下的相对路径 —— 版本值只影响浏览器缓存键，与运行环境无关。
func StaticJSVersion() string {
	root := filepath.Join("internal", "templates", "static", "js", "workbench")
	var latest int64
	err := filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // 目录缺失/权限问题不阻断渲染，版本退化为 0
		}
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if err != nil || latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}

// WorkbenchTitle 画布标题：作者自己的 SEO 标题优先，否则「前缀 + 草稿路径」。
//
// 取词在 Go 侧完成（tr 参与签名）：这个值会作为 data.title 交给 shell.Prepare，
// 而 injectI18n 对 title 的处理是 `t(title, title)` —— 拼接过的句子不是 key，
// 只会原样返回，所以前缀必须先在这里翻译好（词条 workbench.title.*）。
func WorkbenchTitle(page *pagecontract.PageResp, tr Translate) string {
	if page == nil || strings.TrimSpace(page.ID) == "" {
		return tr(workbenchenums.TitleEditor)
	}
	var doc struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(page.DraftDocument, &doc)
	if doc.Settings.SEO.Title != "" {
		return doc.Settings.SEO.Title
	}
	return tr(workbenchenums.TitleEditorPrefix) + page.DraftPath
}

// OutlineNode 结构树节点（文档 JSON 的子集）。
type OutlineNode struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Name     string        `json:"name"`
	Hidden   bool          `json:"hidden"`
	Locked   bool          `json:"locked"`
	Children []OutlineNode `json:"children"`
}

// RenderOutlineHTML 递归渲染节点树为 HTML（树结构简单，用拼串而非模板递归）。
//
// tr 参与签名只为一件事：按钮提示与徽标文案要按请求语言取词。这些句子进的是
// HTML 属性与文本节点，不经过 Jet 取词层 —— 见 workbenchenums 里 workbench.outline.*
// 那批 key 的说明。
func RenderOutlineHTML(nodes []OutlineNode, selectedID, filter string, tr Translate) string {
	var sb strings.Builder
	writeOutlineNodes(tr, &sb, nodes, selectedID, filter)
	return sb.String()
}

// writeOutlineNodes 深度优先输出 <ul><li><div class="wb-node">…</div><ul>…</ul></li>…</ul>。
func writeOutlineNodes(tr Translate, sb *strings.Builder, nodes []OutlineNode, selectedID, filter string) {
	sb.WriteString("<ul>")
	for i := range nodes {
		n := &nodes[i]
		if filter != "" && !outlineSubtreeHit(n, filter) {
			continue
		}
		label := outlineLabel(n)
		cls := "wb-node"
		if n.ID == selectedID {
			cls += " is-selected"
		}
		sb.WriteString("<li>")
		// role=treeitem + tabindex=0：键盘可达（焦点环样式见 workbench-a11y.css，
		// 方向键/Enter 行为由客户端 bindTreeHtmx 的事件委托实现）。
		sb.WriteString(`<div class="` + cls + `" role="treeitem" tabindex="0" data-id="` + html.EscapeString(n.ID) +
			`" data-type="` + html.EscapeString(n.Type) + `" draggable="true">`)
		caret := ""
		if len(n.Children) > 0 {
			caret = "▾"
		}
		sb.WriteString(`<button class="wb-caret" title="` +
			html.EscapeString(tr(workbenchenums.OutlineToggle)) + `">` + caret + `</button>`)
		// data-named 标记用户是否自定义了名称：未命名时客户端用组件中文名覆盖显示。
		sb.WriteString(`<span class="wb-node-name" data-named="` + boolFlag(n.Name != "") + `">` +
			html.EscapeString(label) + `</span>`)
		if n.Hidden {
			sb.WriteString(`<span class="wb-node-flag" title="` +
				html.EscapeString(tr(workbenchenums.OutlineHiddenHint)) + `">` +
				html.EscapeString(tr(workbenchenums.OutlineHiddenBadge)) + `</span>`)
		}
		if n.Locked {
			sb.WriteString(`<span class="wb-node-flag" title="` +
				html.EscapeString(tr(workbenchenums.OutlineLockedHint)) + `">` +
				html.EscapeString(tr(workbenchenums.OutlineLockedBadge)) + `</span>`)
		}
		sb.WriteString(`<span class="wb-node-actions">`)
		for _, op := range []struct{ text, titleKey, op string }{
			{"↑", workbenchenums.OutlineOpUp, "up"}, {"↓", workbenchenums.OutlineOpDown, "down"},
			{"⧉", workbenchenums.OutlineOpDup, "dup"}, {"✕", workbenchenums.OutlineOpDel, "del"},
		} {
			sb.WriteString(`<button type="button" class="wb-node-action" data-wb-op="` + op.op +
				`" title="` + html.EscapeString(tr(op.titleKey)) + `">` + op.text + `</button>`)
		}
		sb.WriteString(`</span></div>`)
		if len(n.Children) > 0 {
			writeOutlineNodes(tr, sb, n.Children, selectedID, filter)
		}
		sb.WriteString("</li>")
	}
	sb.WriteString("</ul>")
}

// outlineLabel 节点显示名：用户命名 > 组件类型（去 core. 前缀，客户端会换成中文）> 节点 ID。
func outlineLabel(n *OutlineNode) string {
	if strings.TrimSpace(n.Name) != "" {
		return n.Name
	}
	if t := strings.TrimPrefix(n.Type, "core."); t != "" {
		return t
	}
	return n.ID
}

// outlineSubtreeHit 节点自身或任一后代命中过滤词。
func outlineSubtreeHit(n *OutlineNode, filter string) bool {
	if strings.Contains(strings.ToLower(outlineLabel(n)), filter) {
		return true
	}
	for i := range n.Children {
		if outlineSubtreeHit(&n.Children[i], filter) {
			return true
		}
	}
	return false
}

// boolFlag 布尔转 "1"/""（模板/属性用）。
func boolFlag(v bool) string {
	if v {
		return "1"
	}
	return ""
}

// PreviewDocKind 预览文档的来源（决定可归因文案的口径）。
type PreviewDocKind int

const (
	// PreviewDocPage 手工页面草稿（page 编译管线）。
	PreviewDocPage PreviewDocKind = iota
	// PreviewDocBlock 全局块草稿（同上）。
	PreviewDocBlock
	// PreviewDocStructureTemplate 结构模板（页眉 / 页脚）的无样例实体模式。
	PreviewDocStructureTemplate
)

// PreviewDocKindName 日志用名称（结构化日志里区分三种画布）。
func PreviewDocKindName(kind PreviewDocKind) string {
	switch kind {
	case PreviewDocBlock:
		return "block"
	case PreviewDocStructureTemplate:
		return "structure_template"
	default:
		return "page"
	}
}

// PreviewOutcome 预览编译的出口分级（调用方据此选状态码与文案，本包不认识 HTTP 码）。
type PreviewOutcome int

const (
	// PreviewOK 编译成功，返回的 HTML 可用。
	PreviewOK PreviewOutcome = iota
	// PreviewInvalidDocument 文档非法（调用方对作者给 400）。
	PreviewInvalidDocument
	// PreviewCompileRejected 编译失败（422；文案见 ClassifyPreviewRejection）。
	PreviewCompileRejected
	// PreviewInternal 装配 / 模板加载等内部失败（500；原文只进日志）。
	PreviewInternal
)

// RenderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
//
// 编译复用 page 模块 CompilePreview（与正式构建同源装配管线，docs/06 §10）：
// 全局块引用展开、插件组件集注入、主题/集合解析均与构建一致，画布所见即产物。
// editorBridge（画布联动 JS）为调用方的后处理拼接，与编译无关，仅预览启用。
// projectID 为文档所属站点工程（页面/块的记录字段），驱动导航等站点级资源解析；
// currentPath 为页面访问路径（导航当前项高亮，块预览传空）。
//
// 不在这里做 nil 保护：与原 handler 内联实现一致（未装配 pages 时由装配侧保证，
// 该路径在页面 / 块 / 结构模板三个入口处都先被挡住）。
func (s *Service) RenderPreview(ctx context.Context, document json.RawMessage, projectID, currentPath, lang string, withEditorBridge bool) ([]byte, PreviewOutcome, error) {
	html, err := s.pages.CompilePreview(ctx, document, projectID, currentPath, lang, withEditorBridge)
	if err != nil {
		switch {
		case errors.Is(err, pagecontract.ErrPreviewInvalidDocument):
			return nil, PreviewInvalidDocument, err
		case errors.Is(err, pagecontract.ErrPreviewCompileFailed):
			return nil, PreviewCompileRejected, err
		default:
			return nil, PreviewInternal, err
		}
	}
	return html, PreviewOK, nil
}

// ClassifyPreviewRejection 预览编译失败的分级判据（纯函数，不取词、不写响应）。
//
//	problem 非空 → ① 组件校验原文（调用方拼「归口文案：原文」透出给作者）；
//	key 非空     → ② 可归因文案的 key（调用方取词；取不到就落 ③ 归口）；
//	两者皆空     → ③ 归口文案。
//
// 分级顺序固定：① errors.As 命中 *pagecontract.PreviewProblem —— 组件校验问题
// （「手风琴至少需要一个折叠项」这类），带原文透出：这是工作台画布的核心价值，
// 作者据此直接在画布上修；校验在编译里最先发生，作者先修它再看别的。
// ② 文档事实能说清的（字段绑定 / 文档没过校验）—— 走 workbench.err.* 可行动文案；
// ③ 其余（装配缺失、组件模板加载失败）—— 归口文案，**原文只进结构化日志**。
func ClassifyPreviewRejection(document json.RawMessage, kind PreviewDocKind, err error) (problem, key string) {
	// ① 作者可操作的组件校验提示（产生处带类型标记，不靠嗅探文本）。
	var pp *pagecontract.PreviewProblem
	if errors.As(err, &pp) {
		if msg := strings.TrimSpace(pp.Msg); msg != "" {
			return msg, ""
		}
	}
	// ② 文档事实能说清原因的。
	if k, ok := PreviewCompileFacingKey(document, kind); ok {
		return "", k
	}
	return "", ""
}

// PreviewCompileFacingKey 预览编译失败的分类（判据是**文档事实**，不嗅探错误字符串）。
//
// 为什么能这么判：这条管线（page.CompilePreview → compileDocument）**从不注入内容解析器**
// —— 页面 / 全局块 / 结构模板都不是内容实体实例，所以「文档里声明了字段绑定」与
// 「编译会失败」是同一件事，且失败原因可以精确归因。
//
//  1. 有字段绑定       → 结构模板 / 页面块两条不同处置的文案；
//  2. 文档没过容错校验 → 组件配置不完整（作者在检查器里就能补齐）；
//  3. 其余             → 归不了因（装配缺失 / 组件模板加载失败等内部问题），调用方给归口文案。
//
// 与 ①（PreviewProblem 类型标记）的分工：校验类问题在产生处就被标记，走 ① 带**原文**透出；
// 这里的第 2 条是**同判据的兜底** —— 标记要靠 page/service 的失败路径补上，而这个 422 出口
// 是通用的（将来别的编译入口也走它），兜底保证「标记缺失时仍给一句可行动文案」，
// 而不是退回泛化句。
//
// 只在编译**已经失败**之后调用：校验通过而编译失败的文档不会被误判成「配置问题」。
func PreviewCompileFacingKey(document json.RawMessage, kind PreviewDocKind) (string, bool) {
	page, err := builder.ParsePage(document)
	if err != nil || page == nil {
		return "", false
	}
	if DocumentHasFieldBinding(page) {
		if kind == PreviewDocStructureTemplate {
			return workbenchenums.ErrStructureTemplateFieldBinding, true
		}
		return workbenchenums.ErrPreviewFieldBindingUnsupported, true
	}
	if _, verr := builder.ValidatePageTolerant(page); verr != nil {
		return workbenchenums.ErrPreviewDocumentInvalid, true
	}
	return "", false
}

// DocumentHasFieldBinding 文档里是否声明了字段绑定。
//
// 两条来源都要查，漏一条就漏一类组件 —— 而漏掉的那一类正是最容易出现在结构模板里的：
//
//  1. builder.CollectFieldRefs —— 只认实现了 core.FieldBindingProvider 的组件
//     （product / productcard / productlist / productselector，绑定藏在各自的槽位结构里）；
//  2. 通用扫描 binding.field —— heading / text / image / gallery / button 的绑定是**普通
//     props 字段**（形状统一：{"binding":{"field":...}}），不在 (1) 的收集范围里。
//
// 判据是「props 里 binding.field 非空」这一**文档事实**，不是错误字符串。
func DocumentHasFieldBinding(page *builder.Page) bool {
	if page == nil {
		return false
	}
	if refs, err := builder.CollectFieldRefs(page); err == nil && len(refs) > 0 {
		return true
	}
	for _, n := range page.Root {
		if nodeHasBindingField(n) {
			return true
		}
	}
	return false
}

// nodeHasBindingField 递归查找 binding.field 非空的节点。
//
// props 解不出该形状只说明「这个组件没有这种绑定」，不构成判定失败（各组件 props 差异很大，
// 用最小的探针结构而不是逐组件反序列化）。
func nodeHasBindingField(n *core.Node) bool {
	if n == nil {
		return false
	}
	if len(n.Props) > 0 {
		var probe struct {
			Binding *struct {
				Field string `json:"field"`
			} `json:"binding"`
		}
		if err := json.Unmarshal(n.Props, &probe); err == nil &&
			probe.Binding != nil && strings.TrimSpace(probe.Binding.Field) != "" {
			return true
		}
	}
	for _, c := range n.Children {
		if nodeHasBindingField(c) {
			return true
		}
	}
	return false
}

// TemplatePreviewFacingKey 模板预览（有样例实体）失败的分类。
//
// 判据是**模块哨兵 key**：presentation / contenttemplate 的哨兵是字符串常量，
// errors.New(<常量>) 在各调用点新建，errors.Is 拿不到同一个值 —— 与 workbench_instance.go
// 判 ErrDetachConfirmRequired 同一口径。未命中 = 归不了因（ErrBuildFailed 下的编译细节、
// ErrRegistryMissing 这类装配缺陷），调用方给归口文案。
func TemplatePreviewFacingKey(raw string) (string, bool) {
	switch {
	case strings.Contains(raw, contenttemplateenums.ErrFieldBindingInvalid):
		return workbenchenums.ErrTemplateFieldBindingInvalid, true
	case strings.Contains(raw, presentationenums.ErrTemplateTypeMismatch):
		return workbenchenums.ErrTemplateEntityTypeMismatch, true
	case strings.Contains(raw, presentationenums.ErrProjectRequired),
		strings.Contains(raw, presentationenums.ErrProjectNotFound):
		return workbenchenums.ErrTemplateProjectScope, true
	}
	return "", false
}

// InstanceSaveFacingKey 实例覆盖文档保存失败的分类（workbench/instance/save 的 422）。
//
// 与模板预览同为「渲染链路失败」，但入口不同：这里的编译失败一律被
// presentation 包成 ErrBuildFailed，字段绑定越界只会以它内部的文案出现 ——
// 不再细分，统一给一条「没写入 + 怎么修」的文案，避免把内部细节当分类依据。
func InstanceSaveFacingKey(raw string) (string, bool) {
	switch {
	case strings.Contains(raw, presentationenums.ErrBuildFailed):
		return workbenchenums.ErrInstanceSaveRejected, true
	case strings.Contains(raw, presentationenums.ErrProjectRequired),
		strings.Contains(raw, presentationenums.ErrProjectNotFound):
		return workbenchenums.ErrTemplateProjectScope, true
	}
	return "", false
}

// InspectorSchemaItem 与 core.SchemaJSON 的输出对齐（后端单源，前端不再各自解析）。
type InspectorSchemaItem struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Section string `json:"section"`
	Default string `json:"default"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Step    int    `json:"step"`
	MaxLen  int    `json:"maxLen"`
	Unit    string `json:"unit"`
	Options []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
	Hidden bool `json:"hidden"`
}

// InspectorOption 下拉/分段选项（模板渲染用）。
type InspectorOption struct {
	Value    string
	Label    string
	Selected bool
}

// InspectorSubInput 多输入控件（spacing/corners/rtext）的单个子输入。
type InspectorSubInput struct {
	// Path 相对 props 的完整路径（如 advanced.margin.desktop.top）。
	Path        string
	Label       string
	Value       string
	Placeholder string
}

// InspectorRangeRow 区间列表控件的一行：上限为空表示「以上」（799+）。
type InspectorRangeRow struct {
	Min string
	Max string
}

// InspectorField 单个字段（模板渲染用）。
type InspectorField struct {
	Key   string
	Label string
	// UI 渲染形态：text/textarea/number/bool/select/color/spacing/corners/rtext/classes/cssdecls/media/mediaList/dimension
	UI          string
	Value       string
	Bool        bool
	Options     []InspectorOption
	Min         int
	Max         int
	Step        int
	Placeholder string
	Inputs      []InspectorSubInput
	// Rows 区间列表控件的行（rangelist）：把 `0-199,799+` 这类值拆成可编辑的行。
	Rows []InspectorRangeRow
	// Slot 非空表示该字段由客户端增强控件渲染（取色器/联动锁/媒体选择等）：
	// 服务端只输出定位占位 div，客户端用既有控件函数填充（避免两套控件实现）。
	Slot string
	// HTML 非空表示该字段的整块结构已由服务端生成（重复项面板等）：模板原样输出，
	// 客户端只绑行为 —— 结构只有一处定义（见 workbench_repeater.go）。
	HTML string
	// NavNewRef 非空表示该 entityref 字段支持「就地新建」（当前只有 navigation）：
	// 模板据此渲染一个折叠的新建表单，客户端提交后把新项写回本字段。
	// 端口未注入 / 无工程上下文时不置位 —— 入口整体不渲染，不留一个点了没反应的表单。
	NavNewRef string
	// KindOptions 新建菜单项时的位置选项（只随 NavNewRef 一起用）。
	KindOptions []InspectorOption
}

// InspectorSection 面板分组（WP 式折叠分组）。
// Key 为分组标识（content/style/layout/…），客户端据此把增强面板插入对应折叠组。
type InspectorSection struct {
	Key    string
	Title  string
	Open   bool
	Used   int
	Fields []InspectorField
}

// DocNode 页面文档节点（仅面板定位所需字段）。
type DocNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []DocNode       `json:"children"`
}

// 分组顺序与中文标题 key（与前端旧面板保持一致）。标题按请求语言取词：
// 拼进面板 HTML 的是译文，key 与中文兜底登记在 workbenchenums（workbench.inspector.section.*）。
var inspectorSectionOrder = []struct{ Key, TitleKey string }{
	{"content", workbenchenums.InspectorSectionContent},
	{"style", workbenchenums.InspectorSectionStyle},
	{"layout", workbenchenums.InspectorSectionLayout},
	{"background", workbenchenums.InspectorSectionBackground},
	{"border", workbenchenums.InspectorSectionBorder},
	{"transform", workbenchenums.InspectorSectionTransform},
	{"motion", workbenchenums.InspectorSectionMotion},
	{"hover", workbenchenums.InspectorSectionHover},
	{"responsive", workbenchenums.InspectorSectionResponsive},
	{"advanced", workbenchenums.InspectorSectionAdvanced},
}

// InspectorSections 把节点文档装配成模板可渲染的分组数据。
//
// 装配链：组件 schema（构建内核，按节点类型取）→ 单字段构造 → 分组桶 →
// 重复项面板骨架。任一步取数失败都返回 error，由调用方决定出口（HTTP 侧是 500）。
func (s *Service) InspectorSections(ctx context.Context, node *DocNode, tab, projectID string, tr Translate) ([]InspectorSection, error) {
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		return nil, err
	}
	var items []InspectorSchemaItem
	if node != nil {
		if raw, ok := schemas[node.Type]; ok {
			if err = json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
		}
	}
	var props map[string]any
	if node != nil && len(node.Props) > 0 {
		_ = json.Unmarshal(node.Props, &props)
	}
	sections := s.buildInspectorSections(ctx, items, props, tab, projectID, tr)
	// 重复项面板（折叠项 / 页签）：结构由服务端生成，客户端只绑行为。
	return AppendRepeaterPanel(sections, node, props, tab, tr), nil
}

// FindDocNode 在页面文档里按 ID 查找节点（深度优先）。
func FindDocNode(doc json.RawMessage, nodeID string) (*DocNode, error) {
	if len(doc) == 0 || nodeID == "" {
		return nil, nil
	}
	var page struct {
		Root []DocNode `json:"root"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		return nil, err
	}
	var walk func(nodes []DocNode) *DocNode
	walk = func(nodes []DocNode) *DocNode {
		for i := range nodes {
			if nodes[i].ID == nodeID {
				return &nodes[i]
			}
			if found := walk(nodes[i].Children); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(page.Root), nil
}

// buildInspectorSections 把 schema 控件按分组转成模板数据（跳过 hidden 与不满足条件的字段）。
//
// tr 是「key → 当前语言文案」的取词函数：分组标题与各字段的 Label / Placeholder
// 都在本函数的下游产出，模板只负责把它们铺出来。
func (s *Service) buildInspectorSections(ctx context.Context, items []InspectorSchemaItem, props map[string]any, tab, projectID string, tr Translate) []InspectorSection {
	buckets := map[string][]InspectorField{}
	used := map[string]int{}
	// corners 合并：radiusTL/TR/BR/BL 与 advanced.radius.topLeft/… 各只渲染一次。
	cornersDone := false
	for _, ctl := range items {
		if ctl.Hidden || !inspectorFieldVisible(ctl.Key, props) {
			continue
		}
		sec := ctl.Section
		if sec == "" {
			sec = "content"
		}
		if isCornerKey(ctl.Key) {
			if cornersDone {
				continue
			}
			cornersDone = true
			buckets[sec] = append(buckets[sec], cornersField(ctl, props, tr))
			continue
		}
		if isCornerTailKey(ctl.Key) {
			continue
		}
		f := s.inspectorFieldOf(ctx, ctl, props, projectID, tr)
		if f.Key == "" {
			continue
		}
		if f.Value != "" || f.Bool || hasSubValue(f.Inputs) {
			used[sec]++
		}
		buckets[sec] = append(buckets[sec], f)
	}
	out := make([]InspectorSection, 0, len(inspectorSectionOrder))
	seen := map[string]bool{}
	for _, sec := range inspectorSectionOrder {
		if !sectionInTab(sec.Key, tab) {
			continue
		}
		fields := buckets[sec.Key]
		if len(fields) == 0 {
			continue
		}
		seen[sec.Key] = true
		out = append(out, InspectorSection{
			Key: sec.Key, Title: tr(sec.TitleKey), Fields: fields, Used: used[sec.Key],
			// 展开规则：有值的分组展开、内容分组默认展开、首个分组兜底展开
			// （空面板全收起时用户看不到任何控件，必须至少露一组）。
			Open: used[sec.Key] > 0 || sec.Key == "content" || len(out) == 0,
		})
	}
	// 未登记分组兜底（字典序，保证确定性）。
	rest := make([]string, 0, 4)
	for k := range buckets {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		// 兜底分组同样受页签过滤约束（否则被过滤掉的已登记分组会被当成「未登记」加回来）。
		if !sectionInTab(k, tab) {
			continue
		}
		out = append(out, InspectorSection{Key: k, Title: k, Fields: buckets[k], Used: used[k], Open: used[k] > 0 || len(out) == 0})
	}
	return out
}

// sectionInTab 分组归属页签：content = 内容页签；其余（基础/布局/背景/边框/变换/动效/
// 响应式/高级）= 样式页签。tab 为空表示不过滤（渲染全部）。
func sectionInTab(section, tab string) bool {
	switch tab {
	case "content":
		return section == "content"
	case "style":
		return section != "content"
	}
	return true
}

// inspectorFieldOf 单个 schema 控件 → 模板字段。
//
// tr 是「key → 当前语言文案」的取词函数：本函数与它的下游（圆角 / 间距 / 响应式控件、
// 导航下拉）产出的 Label 与 Placeholder 会直接进面板 HTML，模板层不参与这些句子，
// 所以取词必须在这里完成。
func (s *Service) inspectorFieldOf(ctx context.Context, ctl InspectorSchemaItem, props map[string]any, projectID string, tr Translate) InspectorField {
	f := InspectorField{Key: ctl.Key, Label: ctl.Label, Min: ctl.Min, Max: ctl.Max, Step: ctl.Step}
	if f.Label == "" {
		f.Label = ctl.Key
	}
	value := propString(props, ctl.Key)
	switch ctl.Kind {
	case "entityref":
		f.UI = "select"
		f.Value = value
		refKind := ""
		if len(ctl.Options) > 0 {
			refKind = ctl.Options[0].Value
		}
		f.Options = s.entityRefInspectorOptions(ctx, projectID, refKind, value, tr)
		// 导航菜单项：检查器里可以就地新建（写回走 navigation 契约的 Create）。
		// 三个条件缺一不可 —— refKind 是 navigation、端口已注入、有工程上下文；
		// 少任何一个都会渲染出一个「提交必然失败」的入口，比不显示更糟。
		if refKind == "navigation" && s.navigations != nil && projectID != "" {
			f.NavNewRef = refKind
			f.KindOptions = navigationKindOptions(tr)
		}
	case "multientityref":
		// 多选实体（标签 id 列表等，审计 EDT-007）：值仍是逗号分隔串（读写兼容），
		// 但勾选状态由当前值直接渲染 —— 打开面板就知道已经选了哪几个，
		// 不用去数一串 id。选项按 ct tag 声明的实体类型逐个取。
		f.UI = "multientityref"
		f.Value = value
		selected := map[string]bool{}
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				selected[id] = true
			}
		}
		for _, kindOpt := range ctl.Options {
			for _, o := range s.entityRefInspectorOptions(ctx, projectID, kindOpt.Value, "", tr) {
				if o.Value == "" {
					continue // 「（不限）」在单选的语义里有用，在多选里是噪声
				}
				f.Options = append(f.Options, InspectorOption{
					Value: o.Value, Label: o.Label, Selected: selected[o.Value],
				})
			}
		}
	case "rangelist":
		// 区间列表（预设价格档位等，审计 EDT-007）：既有格式 `0-199,799+` 保持不变，
		// 只是把「手写整串」换成逐行编辑。
		f.UI = "rangelist"
		f.Value = value
		f.Rows = parseRangeRows(value)
	case "bool":
		f.UI = "bool"
		f.Bool = value == "true"
	case "select":
		f.UI = "select"
		f.Value = value
		for _, o := range ctl.Options {
			f.Options = append(f.Options, InspectorOption{Value: o.Value, Label: o.Label, Selected: o.Value == value})
		}
	case "text", "textarea":
		// text / textarea：多行纯文本输入。
		f.UI = "textarea"
		f.Value = value
	case "richtext":
		// 富文本内容字段（core.text 正文 / card 正文 / quote 引用 / infobox 描述 / faq 答案）：
		// 编辑器为 Trix，服务端只输出 slot 占位，客户端 fillInspectorSlots 用 richTextField 填充
		// （core.text 的 mode=plaintext 时前端回退多行输入）。
		f.UI = "richtext"
		f.Slot = "richtext"
		f.Value = value
	case "int", "slider", "number":
		f.UI = "number"
		f.Value = value
		if f.Step == 0 {
			f.Step = 1
		}
	case "color":
		f.UI = "color"
		f.Slot = "color"
		f.Value = value
	case "spacing", "margin":
		f.UI = "spacing"
		f.Slot = "spacing"
		f.Inputs = spacingInputs(props, ctl.Key, tr)
	case "boxspacing":
		// container 的 box.padding/margin：三端 CSS 简写，客户端按「一行四向 + 联动」编辑。
		f.UI = "boxspacing"
		f.Slot = "boxspacing"
	case "rtext":
		f.UI = "rtext"
		f.Slot = "rtext"
		f.Inputs = responsiveTextInputs(props, ctl.Key, tr)
	case "classes":
		f.UI = "classes"
		f.Value = value
		f.Placeholder = tr(workbenchenums.InspectorPhClasses)
	case "cssdecls":
		f.UI = "cssdecls"
		f.Value = value
		f.Placeholder = tr(workbenchenums.InspectorPhCSSDecls)
	case "media":
		f.UI = "media"
		f.Slot = "media"
		f.Value = value
	case "mediaList":
		f.UI = "mediaList"
		f.Slot = "mediaList"
		f.Value = propListString(props, ctl.Key)
	case "dimension":
		f.UI = "dimension"
		f.Slot = "dimension"
		f.Value = value
		f.Placeholder = tr(workbenchenums.InspectorPhDimension)
	case "collectionfield", "bindingfield":
		// 集合字段映射（core.cardstack 的 5 个字段）与内容字段绑定（item.<字段>）：
		// 选项来自后端字段白名单（按当前节点的「内容集合」过滤），手填字段名会绕过白名单，
		// 所以服务端只输出 slot，客户端用 collectionFieldControl / bindingFieldControl
		// 渲染成下拉 —— 不走 default 的文本框（退化成手填等于把白名单丢了）。
		f.UI = ctl.Kind
		f.Slot = ctl.Kind
		f.Value = value
	default:
		f.UI = "text"
		f.Value = value
	}
	return f
}

// cornersField 把四角圆角（组件级 radiusTL 或通用层 radius.topLeft）合并为一个字段。
func cornersField(ctl InspectorSchemaItem, props map[string]any, tr Translate) InspectorField {
	f := InspectorField{Key: ctl.Key, Label: ctl.Label, UI: "corners", Slot: "corners"}
	if f.Label == "" {
		f.Label = tr(workbenchenums.InspectorCorners)
	}
	if strings.HasSuffix(ctl.Key, "radiusTL") {
		base := strings.TrimSuffix(ctl.Key, "TL")
		for _, pair := range []struct{ suffix, labelKey string }{
			{"TL", workbenchenums.InspectorCornerTopLeft}, {"TR", workbenchenums.InspectorCornerTopRight},
			{"BR", workbenchenums.InspectorCornerBottomRight}, {"BL", workbenchenums.InspectorCornerBottomLeft},
		} {
			f.Inputs = append(f.Inputs, InspectorSubInput{
				Path: base + pair.suffix, Label: tr(pair.labelKey), Value: propString(props, base+pair.suffix),
			})
		}
		return f
	}
	base := strings.TrimSuffix(ctl.Key, "topLeft")
	for _, pair := range []struct{ suffix, labelKey string }{
		{"topLeft", workbenchenums.InspectorCornerTopLeft}, {"topRight", workbenchenums.InspectorCornerTopRight},
		{"bottomRight", workbenchenums.InspectorCornerBottomRight}, {"bottomLeft", workbenchenums.InspectorCornerBottomLeft},
	} {
		f.Inputs = append(f.Inputs, InspectorSubInput{
			Path: base + pair.suffix, Label: tr(pair.labelKey), Value: propString(props, base+pair.suffix),
		})
	}
	return f
}

// spacingInputs 三端 × 四向边距子输入。
func spacingInputs(props map[string]any, key string, tr Translate) []InspectorSubInput {
	out := make([]InspectorSubInput, 0, 12)
	for _, bp := range []struct{ key, labelKey string }{
		{"desktop", workbenchenums.InspectorBpDesktop}, {"tablet", workbenchenums.InspectorBpTablet},
		{"mobile", workbenchenums.InspectorBpMobile},
	} {
		for _, dir := range []struct{ key, labelKey string }{
			{"top", workbenchenums.InspectorDirTop}, {"right", workbenchenums.InspectorDirRight},
			{"bottom", workbenchenums.InspectorDirBottom}, {"left", workbenchenums.InspectorDirLeft},
		} {
			path := fmt.Sprintf("%s.%s.%s", key, bp.key, dir.key)
			out = append(out, InspectorSubInput{
				Path: path, Label: tr(bp.labelKey) + tr(dir.labelKey), Value: propString(props, path), Placeholder: "0px",
			})
		}
	}
	return out
}

// responsiveTextInputs 三端文本子输入（字号/行高等）。
func responsiveTextInputs(props map[string]any, key string, tr Translate) []InspectorSubInput {
	out := make([]InspectorSubInput, 0, 3)
	for _, bp := range []struct{ key, labelKey string }{
		{"desktop", workbenchenums.InspectorBpDesktop}, {"tablet", workbenchenums.InspectorBpTablet},
		{"mobile", workbenchenums.InspectorBpMobile},
	} {
		path := key + "." + bp.key
		out = append(out, InspectorSubInput{Path: path, Label: tr(bp.labelKey), Value: propString(props, path)})
	}
	return out
}

// hasSubValue 多输入控件是否有已填值（分组「已用项数」统计）。
func hasSubValue(inputs []InspectorSubInput) bool {
	for _, in := range inputs {
		if in.Value != "" {
			return true
		}
	}
	return false
}

// isCornerKey 是否四角圆角的首个字段。
func isCornerKey(key string) bool {
	return strings.HasSuffix(key, "radiusTL") || strings.HasSuffix(key, "radius.topLeft")
}

// isCornerTailKey 四角圆角其余字段（已合并，跳过）。
func isCornerTailKey(key string) bool {
	return strings.HasSuffix(key, "radiusTR") || strings.HasSuffix(key, "radiusBR") || strings.HasSuffix(key, "radiusBL") ||
		strings.HasSuffix(key, "radius.topRight") || strings.HasSuffix(key, "radius.bottomRight") || strings.HasSuffix(key, "radius.bottomLeft")
}

// inspectorFieldVisible 条件字段显隐（与前端旧面板同一套规则）。
func inspectorFieldVisible(key string, props map[string]any) bool {
	switch {
	case key == "advanced.widthValue":
		return propString(props, "advanced.widthMode") == "fixed"
	case key == "visual.bgPositionXY":
		return propString(props, "visual.bgPosition") == "custom"
	case key == "visual.bgSizeValue":
		return propString(props, "visual.bgSize") == "custom"
	case strings.HasPrefix(key, "position.top"), strings.HasPrefix(key, "position.right"),
		strings.HasPrefix(key, "position.bottom"), strings.HasPrefix(key, "position.left"):
		pt := propString(props, "position.type")
		return pt != "" && pt != "static"
	case strings.HasPrefix(key, "position.drawer"):
		return propString(props, "position.type") == "drawer"
	}
	return true
}

// propString 按点路径取 props 值并转字符串（数字去尾零）。
func propString(props map[string]any, path string) string {
	var cur any = props
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[seg]
		if !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// propListString 取字符串数组值（媒体列表）并转为换行文本。
func propListString(props map[string]any, path string) string {
	var cur any = props
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[seg]
		if !ok {
			return ""
		}
	}
	list, ok := cur.([]any)
	if !ok {
		return ""
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		switch v := item.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			for _, key := range []string{"url", "src", "value"} {
				if s, ok := v[key].(string); ok && s != "" {
					out = append(out, s)
					break
				}
			}
		}
	}
	return strings.Join(out, "\n")
}

// parseRangeRows 把 `0-199,200-399,799+` 解析成行。
//
// 认不出的片段原样放进 Min 而不是丢弃：面板不是校验入口，把作者写的原文显示出来
// 让他自己改，比在这一层静默吞掉更好（构建期仍按既有规则拒绝并给出明确报错）。
func parseRangeRows(raw string) []InspectorRangeRow {
	var rows []InspectorRangeRow
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if min, ok := strings.CutSuffix(part, "+"); ok {
			rows = append(rows, InspectorRangeRow{Min: min})
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			rows = append(rows, InspectorRangeRow{Min: lo, Max: hi})
			continue
		}
		rows = append(rows, InspectorRangeRow{Min: part})
	}
	return rows
}

// entityRefInspectorOptions 把集合源可选筛选项转成检查器下拉（EDT-005）。
//
// tr 是「key → 当前语言文案」的取词函数：空选项与导航位置的标签都会直接进面板 HTML，
// 模板层不参与，所以取词在这里完成。
func (s *Service) entityRefInspectorOptions(ctx context.Context, projectID, refKind, selected string, tr Translate) []InspectorOption {
	out := []InspectorOption{{Value: "", Label: tr(workbenchenums.InspectorNavAny), Selected: selected == ""}}
	// 导航菜单项不是集合筛选项（不在商品数据源里），单独走导航模块的列表端口。
	if refKind == "navigation" {
		return s.navigationInspectorOptions(ctx, projectID, selected, tr)
	}
	if s == nil || s.products == nil || projectID == "" {
		return out
	}
	provider, ok := s.products.(core.CollectionFilterOptionsProvider)
	if !ok {
		return out
	}
	// 检查器持有的就是商品数据源：源标识用 contract 常量，不写第二份字面量。
	opts, err := provider.CollectionFilterOptions(ctx, productcontract.CollectionSourceProduct, projectID)
	if err != nil {
		return out
	}
	var choices []core.CollectionFilterChoice
	switch refKind {
	case "category":
		choices = opts.Categories
	case "brand":
		choices = opts.Brands
	case "tag":
		choices = opts.Tags
	default:
		return out
	}
	for _, ch := range choices {
		out = append(out, InspectorOption{
			Value: ch.ID, Label: ch.Name, Selected: ch.ID == selected,
		})
	}
	return out
}

// navigationInspectorOptions 列出本工程全部菜单项（按位置分组排序）。
//
// 标签带位置前缀：同一个工程里「产品」这类标题在页眉与移动端各有一条，
// 只显示标题会让检查器里出现两个一模一样的选项，选错就静默绑到另一端的菜单上。
func (s *Service) navigationInspectorOptions(ctx context.Context, projectID, selected string, tr Translate) []InspectorOption {
	out := []InspectorOption{{Value: "", Label: tr(workbenchenums.InspectorNavAny), Selected: selected == ""}}
	if s == nil || s.navigations == nil || projectID == "" {
		return out
	}
	rows, err := s.navigations.List(ctx, &navigationdto.ListReq{ProjectID: projectID})
	if err != nil {
		return out
	}
	for _, row := range rows {
		if row == nil || row.ID == "" {
			continue
		}
		// 只列根项：按项引用时渲染的是「该项及其子树」，挂到子项上也合法，
		// 但下拉里给全部项会让列表过长且层级难辨；子项可另用「按位置」模式取整棵树。
		if row.ParentID != nil && *row.ParentID != "" {
			continue
		}
		out = append(out, InspectorOption{
			Value:    row.ID,
			Label:    NavigationKindLabel(row.Kind, tr) + " · " + row.Title,
			Selected: row.ID == selected,
		})
	}
	return out
}

// navigationKindOptions 新建菜单项时的位置选项（与导航管理页的四个位置一致）。
func navigationKindOptions(tr Translate) []InspectorOption {
	return []InspectorOption{
		{Value: "header", Label: NavigationKindLabel("header", tr), Selected: true},
		{Value: "header_mobile", Label: NavigationKindLabel("header_mobile", tr)},
		{Value: "footer", Label: NavigationKindLabel("footer", tr)},
		{Value: "footer_mobile", Label: NavigationKindLabel("footer_mobile", tr)},
	}
}

// NavigationKindLabel 位置名（与 admin 导航页的选项文案同义，按请求语言取词）。
func NavigationKindLabel(kind string, tr Translate) string {
	switch kind {
	case "header":
		return tr(workbenchenums.InspectorNavKindHeader)
	case "header_mobile":
		return tr(workbenchenums.InspectorNavKindHeaderMobile)
	case "footer":
		return tr(workbenchenums.InspectorNavKindFooter)
	case "footer_mobile":
		return tr(workbenchenums.InspectorNavKindFooterMobile)
	}
	return kind
}

// repeaterRow 一条重复项的数据。
type repeaterRow struct {
	Value  string
	Extras map[string]bool
}

// repeaterRowsOf 从 props 里取出重复项数据（取不到就是空列表，与客户端同口径）。
func repeaterRowsOf(props map[string]any, spec *core.AlignedRepeaterSpec) []repeaterRow {
	raw, _ := props[spec.AlignKey].([]any)
	rows := make([]repeaterRow, 0, len(raw))
	for _, it := range raw {
		entry, _ := it.(map[string]any)
		row := repeaterRow{Extras: map[string]bool{}}
		if v, ok := entry[spec.Field].(string); ok {
			row.Value = v
		}
		for _, ex := range spec.Extra {
			if b, ok := entry[ex.Key].(bool); ok {
				row.Extras[ex.Key] = b
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// renderRepeaterHTML 生成重复项面板骨架。
//
// panelCount 是画布上对应的面板节点数：两者不一致时给红色提示（历史脏数据或直接在
// 画布上增删面板都可能造成不一致），让用户保存前就知道会被校验拦下。
//
// tr 是「key → 当前语言文案」的取词函数：本文件的 HTML 由 Go 拼串产出，
// 模板只做 `|unsafe` 插槽，所以按钮提示与数量说明必须在这里取词。
// spec.Noun（项名，如「折叠项」）来自组件声明（builder/core），不属于本模块的文案，
// 作为占位符 {noun} 填进已翻译的句子 —— 译文语序与中文不同也不会错位。
func renderRepeaterHTML(spec *core.AlignedRepeaterSpec, rows []repeaterRow, panelCount int, tr Translate) string {
	var b strings.Builder
	b.WriteString(`<div class="wb-repeater" data-wb-rep="` + html.EscapeString(spec.Type) +
		`" data-wb-rep-field="` + html.EscapeString(spec.Field) + `">`)
	for i, row := range rows {
		b.WriteString(`<div class="wb-repeater-row" data-wb-rep-index="` + fmt.Sprint(i) + `">`)
		b.WriteString(`<div class="wb-repeater-mid">`)
		placeholder := spec.Noun + fmt.Sprint(i+1) + " " + spec.Label
		b.WriteString(`<input type="text" data-wb-rep-input="` + fmt.Sprint(i) +
			`" value="` + html.EscapeString(row.Value) + `" placeholder="` + html.EscapeString(placeholder) + `">`)
		for _, ex := range spec.Extra {
			checked := ""
			if row.Extras[ex.Key] {
				checked = " checked"
			}
			b.WriteString(`<label class="wb-check-field"><input type="checkbox" data-wb-rep-extra="` +
				html.EscapeString(ex.Key) + `" data-wb-rep-index="` + fmt.Sprint(i) + `"` + checked + `> ` + html.EscapeString(ex.Label) + `</label>`)
		}
		b.WriteString(`</div><div class="wb-repeater-acts">`)
		if i > 0 {
			b.WriteString(repeaterButton("↑", tr(workbenchenums.InspectorRepeaterMoveUp), "move", i, "-1", "wb-btn wb-btn-sm wb-btn-ghost"))
		}
		if i < len(rows)-1 {
			b.WriteString(repeaterButton("↓", tr(workbenchenums.InspectorRepeaterMoveDown), "move", i, "1", "wb-btn wb-btn-sm wb-btn-ghost"))
		}
		b.WriteString(repeaterButton("✕",
			strings.ReplaceAll(tr(workbenchenums.InspectorRepeaterRemove), "{noun}", spec.Noun),
			"remove", i, "", "wb-icon-btn"))
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`<button type="button" class="wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add" data-wb-rep-op="add">` +
		html.EscapeString(spec.AddText) + `</button>`)
	b.WriteString(`<p class="wb-empty"`)
	if len(rows) != panelCount {
		b.WriteString(` style="color: var(--c-danger, #d93425)"`)
	}
	b.WriteString(`>`)
	if len(rows) == panelCount {
		b.WriteString(html.EscapeString(fillRepeaterText(tr(workbenchenums.InspectorRepeaterMatched), map[string]string{
			"{noun}": spec.Noun, "{count}": fmt.Sprint(len(rows)),
		})))
	} else {
		b.WriteString(html.EscapeString(fillRepeaterText(tr(workbenchenums.InspectorRepeaterMismatch), map[string]string{
			"{noun}": spec.Noun, "{rows}": fmt.Sprint(len(rows)), "{panels}": fmt.Sprint(panelCount),
		})))
	}
	b.WriteString(`</p></div>`)
	return b.String()
}

// fillRepeaterText 用占位符值填充已翻译的句子（占位符约定同 sys_i18n 的 {name}）。
//
// 键之间互不为子串，所以 map 的遍历顺序不影响结果；用它而不是 fmt.Sprintf 是为了让
// 译文重新排序占位符时不必改 Go 代码。
func fillRepeaterText(text string, values map[string]string) string {
	for k, v := range values {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}

// repeaterButton 行内操作按钮（to 为相对位移，仅 move 用）。
func repeaterButton(text, title, op string, index int, to, cls string) string {
	s := `<button type="button" class="` + cls + `" data-wb-rep-op="` + op +
		`" data-wb-rep-index="` + fmt.Sprint(index) + `"`
	if to != "" {
		s += ` data-wb-rep-to="` + to + `"`
	}
	return s + ` title="` + html.EscapeString(title) + `">` + html.EscapeString(text) + `</button>`
}

// AppendRepeaterPanel 把重复项面板的服务端骨架挂进「内容」分组末尾。
//
// 只出结构 —— 行为留在客户端（repeater.js 的 bindRepeaterPanel）。
func AppendRepeaterPanel(sections []InspectorSection, node *DocNode, props map[string]any, tab string, tr Translate) []InspectorSection {
	if tab == "style" || tab == "motion" {
		return sections
	}
	spec := core.AlignedRepeaterFor(node.Type)
	if spec == nil {
		return sections
	}
	field := InspectorField{
		Key:  "__repeater",
		HTML: renderRepeaterHTML(spec, repeaterRowsOf(props, spec), len(node.Children), tr),
	}
	for i := range sections {
		if sections[i].Key == "content" {
			sections[i].Fields = append(sections[i].Fields, field)
			return sections
		}
	}
	// 组件没有内容分组时补一个：tabs 的字段全在样式里（竖向 / 对齐 / 配色），
	// 但「页签列表」本身是内容 —— 挂在样式分组里位置不对。
	// content 是分组顺序表 inspectorSectionOrder 的第一项，前置插入即可。
	return append([]InspectorSection{{Key: "content", Title: tr(workbenchenums.InspectorSectionContent), Open: true, Fields: []InspectorField{field}}}, sections...)
}
