package service

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
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

func StaticJSVersion() string {
	root := filepath.Join("internal", "templates", "static", "js", "workbench")
	var latest int64
	err := filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
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

type OutlineNode struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Name     string        `json:"name"`
	Hidden   bool          `json:"hidden"`
	Locked   bool          `json:"locked"`
	Children []OutlineNode `json:"children"`
}

func RenderOutlineHTML(nodes []OutlineNode, selectedID, filter string, tr Translate) string {
	var sb strings.Builder
	writeOutlineNodes(tr, &sb, nodes, selectedID, filter)
	return sb.String()
}

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

		sb.WriteString(`<div class="` + cls + `" role="treeitem" tabindex="0" data-id="` + html.EscapeString(n.ID) +
			`" data-type="` + html.EscapeString(n.Type) + `" draggable="true">`)
		caret := ""
		if len(n.Children) > 0 {
			caret = "▾"
		}
		sb.WriteString(`<button class="wb-caret" title="` +
			html.EscapeString(tr(workbenchenums.OutlineToggle)) + `">` + caret + `</button>`)

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

func outlineLabel(n *OutlineNode) string {
	if strings.TrimSpace(n.Name) != "" {
		return n.Name
	}
	if t := strings.TrimPrefix(n.Type, "core."); t != "" {
		return t
	}
	return n.ID
}

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

func boolFlag(v bool) string {
	if v {
		return "1"
	}
	return ""
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

type InspectorOption struct {
	Value    string
	Label    string
	Selected bool
}

type InspectorSubInput struct {
	Path        string
	Label       string
	Value       string
	Placeholder string
}

type InspectorRangeRow struct {
	Min string
	Max string
}

type InspectorField struct {
	Key   string
	Label string

	UI          string
	Value       string
	Bool        bool
	Options     []InspectorOption
	Min         int
	Max         int
	Step        int
	Placeholder string
	Inputs      []InspectorSubInput

	Rows []InspectorRangeRow

	Slot string

	HTML string

	NavNewRef string

	KindOptions []InspectorOption
}

type InspectorSection struct {
	Key    string
	Title  string
	Open   bool
	Used   int
	Fields []InspectorField
}

type DocNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []DocNode       `json:"children"`
}

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

func (s *Service) InspectorSections(ctx context.Context, node *DocNode, tab, projectID string, tr Translate) ([]InspectorSection, error) {

	schemas, err := s.InspectorSchemas(ctx)
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

	return AppendRepeaterPanel(sections, node, props, tab, tr), nil
}

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

func (s *Service) buildInspectorSections(ctx context.Context, items []InspectorSchemaItem, props map[string]any, tab, projectID string, tr Translate) []InspectorSection {
	buckets := map[string][]InspectorField{}
	used := map[string]int{}

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

			Open: used[sec.Key] > 0 || sec.Key == "content" || len(out) == 0,
		})
	}

	rest := make([]string, 0, 4)
	for k := range buckets {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {

		if !sectionInTab(k, tab) {
			continue
		}
		out = append(out, InspectorSection{Key: k, Title: k, Fields: buckets[k], Used: used[k], Open: used[k] > 0 || len(out) == 0})
	}
	return out
}

func sectionInTab(section, tab string) bool {
	switch tab {
	case "content":
		return section == "content"
	case "style":
		return section != "content"
	}
	return true
}

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

		if refKind == "navigation" && s.navigations != nil && projectID != "" {
			f.NavNewRef = refKind
			f.KindOptions = navigationKindOptions(tr)
		}
	case "multientityref":

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
					continue
				}
				f.Options = append(f.Options, InspectorOption{
					Value: o.Value, Label: o.Label, Selected: selected[o.Value],
				})
			}
		}
	case "rangelist":

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

		f.UI = "textarea"
		f.Value = value
	case "richtext":

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

		f.UI = ctl.Kind
		f.Slot = ctl.Kind
		f.Value = value
	default:
		f.UI = "text"
		f.Value = value
	}
	return f
}

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

func hasSubValue(inputs []InspectorSubInput) bool {
	for _, in := range inputs {
		if in.Value != "" {
			return true
		}
	}
	return false
}

func isCornerKey(key string) bool {
	return strings.HasSuffix(key, "radiusTL") || strings.HasSuffix(key, "radius.topLeft")
}

func isCornerTailKey(key string) bool {
	return strings.HasSuffix(key, "radiusTR") || strings.HasSuffix(key, "radiusBR") || strings.HasSuffix(key, "radiusBL") ||
		strings.HasSuffix(key, "radius.topRight") || strings.HasSuffix(key, "radius.bottomRight") || strings.HasSuffix(key, "radius.bottomLeft")
}

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

type repeaterRow struct {
	Value  string
	Extras map[string]bool
}

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

func fillRepeaterText(text string, values map[string]string) string {
	for k, v := range values {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}

func repeaterButton(text, title, op string, index int, to, cls string) string {
	s := `<button type="button" class="` + cls + `" data-wb-rep-op="` + op +
		`" data-wb-rep-index="` + fmt.Sprint(index) + `"`
	if to != "" {
		s += ` data-wb-rep-to="` + to + `"`
	}
	return s + ` title="` + html.EscapeString(title) + `">` + html.EscapeString(text) + `</button>`
}

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

	return append([]InspectorSection{{Key: "content", Title: tr(workbenchenums.InspectorSectionContent), Open: true, Fields: []InspectorField{field}}}, sections...)
}
