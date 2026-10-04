package service

// workbench_preview.go — 预览编译的失败分类与出口分级。
//
// 原实现在 inbound/http 的 workbench_err.go 里：判据是**文档事实**与**模块哨兵 key**，
// 与 HTTP 无关 —— 只有「写什么状态码、取什么文案」是 HTTP 关切，那部分留在 handler。
//
// 三条口径各自独立，不要互相借判据：
//   - 编译失败（页面 / 块 / 结构模板）→ 文档事实（有没有字段绑定、有没有过校验）；
//   - 模板预览失败（有样例实体）→ 模块哨兵字符串；
//   - 实例覆盖文档保存失败 → 渲染链路的错误包装。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	pagecontract "go_wp/internal/module/page/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

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
