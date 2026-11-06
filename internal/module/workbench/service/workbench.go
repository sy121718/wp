package service

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/url"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	navigationdto "go_wp/internal/module/navigation/dto"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	productcontract "go_wp/internal/module/product/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/builder/editor"
)

func (s *Service) PageByID(ctx context.Context, pageID string) (*pagecontract.PageResp, error) {
	if s == nil || s.pages == nil {
		// 装配期自检：正常启动路径到不了这里（routers 装配保证 pages 非 nil）。
		// service 不造界面词条 —— 这里给的是诊断事实，不是给用户看的句子；
		// handler 目前把它当「页面取不到」处理，状态码语义待收口（见台账）。
		return nil, errors.New("workbench: page service not assembled")
	}
	projectID, err := s.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return s.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

func ThemeIDOf(page *pagecontract.PageResp) string {
	if page.ThemeID == "" {
		return ""
	}
	return page.ThemeID
}

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

func (s *Service) BlockSummaries(ctx context.Context, projectID string) []map[string]any {
	blocks, err := s.blocks.List(ctx, &blockcontract.ListReq{ProjectID: projectID})
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, map[string]any{"id": b.ID, "projectId": b.ProjectID, "name": b.Name, "kind": b.Kind, "category": b.Category, "reuseMode": b.ReuseMode, "presetKey": b.PresetKey, "presetVersion": b.PresetVersion})
	}
	return out
}

func (s *Service) TemplateByID(ctx context.Context, templateID, projectID string) (*contenttemplatedto.TemplateResp, error) {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return s.contentTemplates.GetScoped(ctx, pid, templateID)
	}
	return s.contentTemplates.Get(ctx, &contenttemplatedto.GetReq{ID: templateID})
}

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

func (s *Service) BlockByID(ctx context.Context, blockID string) (*blockdto.BlockResp, error) {
	return s.blocks.Detail(ctx, &blockdto.DetailReq{ID: blockID})
}

// StaticJSVersion 返回最终复合资产（公共 UI + 编辑器工具 + 应用宿主适配）的内容哈希，
// 由 templates 读取实际 embed 复合 FS，不写死私有磁盘目录。
func StaticJSVersion() string {
	return templates.StaticJSVersion()
}

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

type OutlineNode = editor.OutlineNode

// RenderOutlineHTML 渲染大纲树；文案由调用方按请求语言提供。
func RenderOutlineHTML(nodes []OutlineNode, selectedID, filter string, tr Translate) string {
	return editor.RenderOutlineHTML(nodes, selectedID, filter, outlineLabels(tr))
}

func outlineLabels(tr Translate) editor.OutlineLabels {
	return editor.OutlineLabels{
		Toggle: tr(workbenchenums.OutlineToggle), HiddenHint: tr(workbenchenums.OutlineHiddenHint),
		HiddenBadge: tr(workbenchenums.OutlineHiddenBadge), LockedHint: tr(workbenchenums.OutlineLockedHint),
		LockedBadge: tr(workbenchenums.OutlineLockedBadge), OpUp: tr(workbenchenums.OutlineOpUp),
		OpDown: tr(workbenchenums.OutlineOpDown), OpDup: tr(workbenchenums.OutlineOpDup),
		OpDel: tr(workbenchenums.OutlineOpDel),
	}
}

type PreviewDocKind int

const (
	PreviewDocPage PreviewDocKind = iota

	PreviewDocBlock

	PreviewDocStructureTemplate
)

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

type PreviewOutcome int

const (
	PreviewOK PreviewOutcome = iota

	PreviewInvalidDocument

	PreviewCompileRejected

	PreviewInternal
)

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

func ClassifyPreviewRejection(document json.RawMessage, kind PreviewDocKind, err error) (problem, key string) {

	var pp *pagecontract.PreviewProblem
	if errors.As(err, &pp) {
		if msg := strings.TrimSpace(pp.Msg); msg != "" {
			return msg, ""
		}
	}

	if k, ok := PreviewCompileFacingKey(document, kind, err); ok {
		return "", k
	}
	return "", ""
}

func PreviewCompileFacingKey(document json.RawMessage, kind PreviewDocKind, compileErr error) (string, bool) {
	if errors.Is(compileErr, builder.ErrPublishedActionUnavailable) {
		return workbenchenums.ErrPreviewActionTargetUnavailable, true
	}
	if errors.Is(compileErr, core.ErrContentResolverRequired) {
		if kind == PreviewDocStructureTemplate {
			return workbenchenums.ErrStructureTemplateFieldBinding, true
		}
		return workbenchenums.ErrPreviewFieldBindingUnsupported, true
	}
	page, err := builder.ParsePage(document)
	if err != nil || page == nil {
		return "", false
	}
	if _, verr := builder.ValidatePageTolerant(page); verr != nil {
		return workbenchenums.ErrPreviewDocumentInvalid, true
	}
	return "", false
}

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

type InspectorSchemaItem = editor.InspectorSchemaItem
type InspectorOption = editor.InspectorOption
type InspectorSubInput = editor.InspectorSubInput
type InspectorRangeRow = editor.InspectorRangeRow
type InspectorField = editor.InspectorField
type InspectorSection = editor.InspectorSection
type DocNode = editor.DocNode

// InspectorSchemas 返回组件 Inspector schema（含插件 schema 合并）。
func (s *Service) InspectorSchemas(ctx context.Context) (map[string]json.RawMessage, error) {
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return schemas, nil
	}
	asm := s.PluginAssembly(ctx)
	if asm == nil {
		return schemas, nil
	}
	for t, data := range asm.InspectorSchemas {
		schemas[t] = data
	}
	return schemas, nil
}

// InspectorSections 用工具内核构建字段；实体引用与导航候选等业务选项经窄端口注入。
func (s *Service) InspectorSections(ctx context.Context, node *DocNode, tab, projectID string, tr Translate) ([]InspectorSection, error) {
	schemas, err := s.InspectorSchemas(ctx)
	if err != nil {
		return nil, err
	}
	return editor.BuildInspectorSections(ctx, editor.InspectorInput{
		Node: node, Schemas: schemas, Tab: tab, ProjectID: projectID,
		Labels: inspectorLabels(tr),
		Ports: editor.Ports{
			EntityRefOptions: func(ctx context.Context, projectID, refKind, selected string) []editor.InspectorOption {
				return s.entityRefInspectorOptions(ctx, projectID, refKind, selected, tr)
			},
			NavigationKinds: func() []editor.InspectorOption { return navigationKindOptions(tr) },
		},
	})
}

func inspectorLabels(tr Translate) editor.Labels {
	return editor.Labels{
		Sections: map[string]string{
			"content": tr(workbenchenums.InspectorSectionContent), "style": tr(workbenchenums.InspectorSectionStyle),
			"layout": tr(workbenchenums.InspectorSectionLayout), "background": tr(workbenchenums.InspectorSectionBackground),
			"border": tr(workbenchenums.InspectorSectionBorder), "transform": tr(workbenchenums.InspectorSectionTransform),
			"motion": tr(workbenchenums.InspectorSectionMotion), "hover": tr(workbenchenums.InspectorSectionHover),
			"responsive": tr(workbenchenums.InspectorSectionResponsive), "advanced": tr(workbenchenums.InspectorSectionAdvanced),
		},
		Corners: tr(workbenchenums.InspectorCorners), TopLeft: tr(workbenchenums.InspectorCornerTopLeft),
		TopRight: tr(workbenchenums.InspectorCornerTopRight), BottomRight: tr(workbenchenums.InspectorCornerBottomRight),
		BottomLeft: tr(workbenchenums.InspectorCornerBottomLeft),
		BpDesktop:  tr(workbenchenums.InspectorBpDesktop), BpTablet: tr(workbenchenums.InspectorBpTablet),
		BpMobile: tr(workbenchenums.InspectorBpMobile),
		DirTop:   tr(workbenchenums.InspectorDirTop), DirRight: tr(workbenchenums.InspectorDirRight),
		DirBottom: tr(workbenchenums.InspectorDirBottom), DirLeft: tr(workbenchenums.InspectorDirLeft),
		PhClasses: tr(workbenchenums.InspectorPhClasses), PhCSSDecls: tr(workbenchenums.InspectorPhCSSDecls),
		PhDimension: tr(workbenchenums.InspectorPhDimension), NavAny: tr(workbenchenums.InspectorNavAny),
		MoveUp: tr(workbenchenums.InspectorRepeaterMoveUp), MoveDown: tr(workbenchenums.InspectorRepeaterMoveDown),
		Remove: tr(workbenchenums.InspectorRepeaterRemove), Matched: tr(workbenchenums.InspectorRepeaterMatched),
		Mismatch: tr(workbenchenums.InspectorRepeaterMismatch),
	}
}

// FindDocNode 在文档 root 中按 ID 深度优先查找。
func FindDocNode(doc json.RawMessage, nodeID string) (*DocNode, error) {
	return editor.FindDocNode(doc, nodeID)
}

func (s *Service) entityRefInspectorOptions(ctx context.Context, projectID, refKind, selected string, tr Translate) []InspectorOption {
	out := []InspectorOption{{Value: "", Label: tr(workbenchenums.InspectorNavAny), Selected: selected == ""}}

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

func navigationKindOptions(tr Translate) []InspectorOption {
	return []InspectorOption{
		{Value: "header", Label: NavigationKindLabel("header", tr), Selected: true},
		{Value: "header_mobile", Label: NavigationKindLabel("header_mobile", tr)},
		{Value: "footer", Label: NavigationKindLabel("footer", tr)},
		{Value: "footer_mobile", Label: NavigationKindLabel("footer_mobile", tr)},
	}
}

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
