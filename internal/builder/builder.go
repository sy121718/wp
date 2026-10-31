package builder

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"maps"
	"strings"
	"sync"

	"go_wp/pkg/i18n"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/source"

	contentcontract "go_wp/internal/module/content/contract"
	mediacontract "go_wp/internal/module/media/contract"
	productcontract "go_wp/internal/module/product/contract"
)

type Page struct {
	Settings PageSettings `json:"settings"`
	Root     []*core.Node `json:"root"`
}

func (p *Page) UnmarshalJSON(data []byte) (err error) {
	type pageAlias Page
	var a pageAlias
	if err = json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = Page(a)
	return nil
}

func ParsePage(data []byte) (p *Page, err error) {
	p = &Page{}
	if err = json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("页面文档解析失败: %w", err)
	}
	return p, nil
}

type CompiledPage struct {
	PreviewReadOnly bool
	PreviewState    source.PreviewState
	PreviewNotice   string

	Lang string

	Title string

	MetaDescription string

	SEOHead string

	BodyClasses []string

	HTML string

	CSS string

	ThemeVarsCSS string

	UISources map[string]string

	UIStyle string

	EnhanceSource string

	TrackSource string

	TrackConfig string

	GA4Head string

	SearchConsoleHead string

	HeadScripts string

	BodyScripts string

	Features *core.FeatureSet
}

type CompileOption func(*compileConfig)

type compileConfig struct {
	content    core.ContentResolver
	block      core.BlockResolver
	set        *jet.Set
	plugin     core.PluginResolver
	action     core.ActionResolver
	collection core.CollectionResolver

	product       productcontract.ProductDataSource
	contentSource contentcontract.ContentDataSource
	navigation    core.NavigationResolver
	projectID     string
	currentPath   string

	usage core.UsageRecorder

	archiveEntityType string
	archiveEntityID   string

	siteLinkResolver func(string) string

	assetProbe func(string) []mediacontract.VariantRef
	theme      *ThemeSettings
	ctx        context.Context

	alternates []Alternate

	locales []core.LocaleLink

	checkoutCountries []core.CheckoutCountry

	lang string

	translate func(key, fallback string) string

	contentTranslator *i18n.ContentTranslator

	extraCSS string

	uiSources map[string]string
	uiStyle   string

	ga4MeasurementID string

	searchConsoleVerification string

	headScripts    string
	bodyScripts    string
	structureSlots []StructureSlot

	canvasSlotFrames bool

	enhanceSource string

	trackSource string

	mode CompileMode

	degrade *DegradeCollector
}

func WithCompileMode(mode CompileMode) CompileOption {
	return func(c *compileConfig) { c.mode = mode }
}

func WithActionResolver(resolve core.ActionResolver) CompileOption {
	return func(c *compileConfig) { c.action = resolve }
}

func WithCanvasSlotFrames() CompileOption {
	return func(c *compileConfig) { c.canvasSlotFrames = true }
}

func WithDegradeCollector(collector *DegradeCollector) CompileOption {
	return func(c *compileConfig) { c.degrade = collector }
}

func WithContentResolver(r core.ContentResolver) CompileOption {
	return func(c *compileConfig) { c.content = r }
}

func WithSiteLinkResolver(fn func(logicalPath string) string) CompileOption {
	return func(c *compileConfig) {
		c.siteLinkResolver = fn
	}
}

func WithArchiveEntity(entityType, entityID string) CompileOption {
	return func(c *compileConfig) {
		c.archiveEntityType = entityType
		c.archiveEntityID = entityID
	}
}

func WithUsageRecorder(r core.UsageRecorder) CompileOption {
	return func(c *compileConfig) { c.usage = r }
}

func WithBlockResolver(r core.BlockResolver) CompileOption {
	return func(c *compileConfig) { c.block = r }
}

func WithComponentSet(set *jet.Set) CompileOption {
	return func(c *compileConfig) { c.set = set }
}

func WithExtraCSS(css string) CompileOption {
	cleaned := strings.ReplaceAll(css, "</style", "")
	return func(c *compileConfig) { c.extraCSS = cleaned }
}

func WithPluginResolver(r core.PluginResolver) CompileOption {
	return func(c *compileConfig) { c.plugin = r }
}

func WithProductDataSource(ds productcontract.ProductDataSource) CompileOption {
	return func(c *compileConfig) { c.product = ds }
}

func WithContentDataSource(ds contentcontract.ContentDataSource) CompileOption {
	return func(c *compileConfig) { c.contentSource = ds }
}

func WithCollectionResolver(r core.CollectionResolver) CompileOption {
	return func(c *compileConfig) { c.collection = r }
}

func WithNavigationResolver(r core.NavigationResolver) CompileOption {
	return func(c *compileConfig) { c.navigation = r }
}

func WithProjectID(projectID string) CompileOption {
	return func(c *compileConfig) { c.projectID = projectID }
}

func WithAssetProbe(fn func(string) []mediacontract.VariantRef) CompileOption {
	return func(c *compileConfig) { c.assetProbe = fn }
}

func WithCurrentPath(path string) CompileOption {
	return func(c *compileConfig) { c.currentPath = path }
}

func WithThemeSettings(t *ThemeSettings) CompileOption {
	return func(c *compileConfig) { c.theme = t }
}

func WithEnhanceSource(js string) CompileOption {
	return func(c *compileConfig) { c.enhanceSource = js }
}

func WithTrackSource(js string) CompileOption {
	return func(c *compileConfig) { c.trackSource = js }
}

func WithUISources(sources map[string]string) CompileOption {
	return func(c *compileConfig) { c.uiSources = sources }
}

func WithUIStyle(css string) CompileOption {
	return func(c *compileConfig) { c.uiStyle = css }
}

func WithContext(ctx context.Context) CompileOption {
	return func(c *compileConfig) { c.ctx = ctx }
}

func WithLanguage(lang string) CompileOption {
	return func(c *compileConfig) { c.lang = strings.TrimSpace(lang) }
}

func WithAlternates(alternates []Alternate) CompileOption {
	return func(c *compileConfig) { c.alternates = alternates }
}

func WithLocaleLinks(links []core.LocaleLink) CompileOption {
	return func(c *compileConfig) { c.locales = links }
}

func WithCheckoutCountries(countries []core.CheckoutCountry) CompileOption {
	return func(c *compileConfig) { c.checkoutCountries = countries }
}

func WithTranslator(fn func(key, fallback string) string) CompileOption {
	return func(c *compileConfig) { c.translate = fn }
}

func WithContentTranslator(t *i18n.ContentTranslator) CompileOption {
	return func(c *compileConfig) { c.contentTranslator = t }
}

func contentTranslateFunc(t *i18n.ContentTranslator) func(sourceText, contentContext string) string {
	if t == nil {
		return nil
	}
	return t.TranslateContent
}

func resolveCompileI18n(cfg *compileConfig) (string, func(key, fallback string) string) {
	var lang string
	var fn func(key, fallback string) string
	if cfg != nil {
		lang = strings.TrimSpace(cfg.lang)
		fn = cfg.translate
	}
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	if fn == nil {
		fn = i18n.TranslateFunc(lang)
	}
	return lang, fn
}

const MaxNodeDepth = 10

func ValidatePage(p *Page) error {
	return validatePageNodes(p, nil)
}

func ValidatePageTolerant(p *Page) (incompleteIDs map[string]bool, err error) {
	incompleteIDs = map[string]bool{}
	if err = validatePageNodes(p, incompleteIDs); err != nil {
		return nil, err
	}
	return incompleteIDs, nil
}

func validatePageNodes(p *Page, incompleteIDs map[string]bool) error {
	if p == nil {
		return ErrPageDocumentEmpty
	}
	if err := validateSettings(&p.Settings); err != nil {
		return &PageSettingsError{Err: err}
	}
	for i, n := range p.Root {
		if d := nodeDepth(n); d > MaxNodeDepth {
			return &NodeDepthError{Index: i, Depth: d, Max: MaxNodeDepth}
		}
	}
	ids := map[string]bool{}
	for i, n := range p.Root {
		var err error
		if incompleteIDs == nil {
			err = core.ValidateNode(n, ids)
		} else {
			err = core.ValidateNodeTolerant(n, ids, incompleteIDs)
		}
		if err != nil {
			return &NodeInvalidError{Index: i, Err: err}
		}
	}
	return nil
}

func nodeDepth(n *core.Node) int {
	if n == nil || len(n.Children) == 0 {
		return 1
	}
	max := 0
	for _, c := range n.Children {
		if d := nodeDepth(c); d > max {
			max = d
		}
	}
	return max + 1
}

func ComponentSchemas() (map[string]json.RawMessage, error) {
	componentSchemasOnce.Do(func() {
		componentSchemas, componentSchemasErr = buildComponentSchemas()
	})
	out := maps.Clone(componentSchemas)
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	return out, componentSchemasErr
}

var (
	componentSchemasOnce sync.Once
	componentSchemas     map[string]json.RawMessage
	componentSchemasErr  error
)

func buildComponentSchemas() (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, 8)
	for _, typeName := range core.Types() {
		comp, err := core.Lookup(typeName)
		if err != nil {
			continue
		}
		sp, ok := comp.(core.SpecProvider)
		if !ok {
			continue
		}
		schema, err := core.SchemaJSON(sp.PropsSpec())
		if err != nil {
			return nil, fmt.Errorf("组件 %s schema 生成失败: %w", typeName, err)
		}
		out[typeName] = schema
	}
	return out, nil
}

func Compile(p *Page, opts ...CompileOption) (res *CompiledPage, err error) {
	cfg := &compileConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	var incompleteIDs map[string]bool
	if cfg.mode == CompileModePublish {
		err = ValidatePage(p)
	} else {
		incompleteIDs, err = ValidatePageTolerant(p)
	}
	if err != nil {
		return nil, err
	}
	if cfg.set == nil {
		return nil, errors.New("编译缺少组件模板 Set：请通过 WithComponentSet 注入（templates.NewComponentSet）")
	}

	if err = ValidateThemeSettings(cfg.theme); err != nil {
		return nil, fmt.Errorf("主题设置: %w", err)
	}

	var b core.CSSBuckets
	compileSettingsCSS(&p.Settings, &b)

	features := core.NewFeatureSet()

	lang, translate := resolveCompileI18n(cfg)

	buildCtx := core.WithBuildProjectID(core.WithBuildLang(cfg.ctx, lang), cfg.projectID)

	var htmlBuf strings.Builder
	ctx := &core.RenderContext{
		CSS: &b, Context: buildCtx, Content: cfg.content, Block: cfg.block,
		IncompleteNodes: incompleteIDs,
		AllowIncomplete: cfg.mode != CompileModePublish,
		Plugin:          cfg.plugin, Collection: cfg.collection,
		Product: cfg.product, ContentSource: cfg.contentSource,
		Navigation: cfg.navigation, ProjectID: cfg.projectID, CurrentPath: cfg.currentPath,
		Lang: lang, Translate: translate, Locales: cfg.locales,

		Checkout: core.CheckoutInput{
			Countries:      cfg.checkoutCountries,
			DefaultCountry: i18n.GetDefaultCountry(),
		},
		ContentTranslate: contentTranslateFunc(cfg.contentTranslator),
		ImageDefaults: core.ImageDefaults{
			LazyLoad: cfg.theme.LazyLoadEnabled(),
			Skeleton: cfg.theme.SkeletonEnabled(),
		},
		RevealInherit:         cfg.theme.RevealInheritOf(),
		RevealDefaultEntrance: cfg.theme.RevealDefaultEntranceOf(),

		AssetProbe:       newAssetProbeMemo(cfg.assetProbe),
		Features:         features,
		CanvasSlotFrames: cfg.canvasSlotFrames,
	}
	ctx.Block = snapshotBlockReads(ctx.Block)
	ctx.SetUsageRecorder(cfg.usage)

	ctx.SetRefFailureHandler(refFailureHandler(cfg))
	ctx.SetArchiveEntity(cfg.archiveEntityType, cfg.archiveEntityID)
	ctx.SetSiteLinkResolver(cfg.siteLinkResolver)

	ctx.SetPanelRenderer(panelRenderer(ctx, ctx.Block, cfg.set))

	type rootView struct {
		view *nodeView
		tag  string

		isSlot bool
	}

	rootNodes := expandStructureSlots(p.Root, cfg.structureSlots)
	if err := configureActions(rootNodes, ctx, cfg.action); err != nil {
		return nil, err
	}
	roots := make([]rootView, 0, len(rootNodes))
	for _, n := range rootNodes {

		v, verr := nodeViewOf(n, true, ctx)
		if verr != nil {
			return nil, verr
		}
		roots = append(roots, rootView{
			view: v, tag: strings.ToLower(strings.TrimSpace(v.Tag)),
			isSlot: isLayoutSlotNode(n),
		})
	}

	mainStart, mainEnd := -1, -1
	if p.Settings.Layout.MainLandmark && len(roots) > 0 {
		mainStart, mainEnd = 0, len(roots)
		for mainStart < mainEnd && (roots[mainStart].isSlot || roots[mainStart].tag == "header" || roots[mainStart].tag == "footer") {
			mainStart++
		}
		for mainEnd > mainStart && (roots[mainEnd-1].isSlot || roots[mainEnd-1].tag == "footer" || roots[mainEnd-1].tag == "header") {
			mainEnd--
		}
	}
	for i, rv := range roots {
		if i == mainStart {
			htmlBuf.WriteString("<main id=\"main-content\">")
		}
		if i == mainEnd {
			htmlBuf.WriteString("</main>")
		}
		if verr := renderView(cfg.set, rv.view, &htmlBuf); verr != nil {
			return nil, verr
		}
	}
	if mainStart >= 0 && mainStart < mainEnd && mainEnd == len(roots) {
		htmlBuf.WriteString("</main>")
	}

	classes := []string{bodyClassPage}
	if p.Settings.Layout.Mode == LayoutBoxed {
		classes = append(classes, bodyClassBoxed)
	} else {
		classes = append(classes, bodyClassFull)
	}
	classes = append(classes, p.Settings.BodyClasses...)

	breadcrumbHome := "Home"
	if cfg.translate != nil {
		breadcrumbHome = cfg.translate("site.breadcrumb.home", "首页")
	}

	seoTrans := contentTranslateFunc(cfg.contentTranslator)
	if seoTrans == nil {
		seoTrans = func(sourceText, _ string) string { return sourceText }
	}
	seoTitle := seoTrans(p.Settings.SEO.Title, SEOTitleContext)
	seoDescription := seoTrans(p.Settings.SEO.Description, SEODescriptionContext)
	seoHead := BuildSEOHead(p.Settings.SEO, p.Settings.SEO.Canonical, seoTitle, seoDescription, breadcrumbHome, cfg.alternates)

	var cssParts []string
	cssParts = append(cssParts, "@layer sky-base, sky-plugin, sky-auto, sky-theme, sky-local;")
	cssParts = append(cssParts, "@layer sky-base {\n"+b.String()+"\n}")
	if cfg.extraCSS != "" {
		cssParts = append(cssParts, "@layer sky-plugin {\n"+cfg.extraCSS+"\n}")
	}

	if cq := b.ContainerQueryCSS(); cq != "" {
		cssParts = append(cssParts, cq)
	}

	if tl := b.TopLevelCSS(); tl != "" {
		cssParts = append(cssParts, tl)
	}
	css := strings.Join(cssParts, "\n\n")

	if rm := cfg.theme.ReducedMotionCSS(); rm != "" {
		css = css + "\n\n" + rm
	}

	if vt := cfg.theme.ViewTransitionsCSS(); vt != "" {
		css = css + "\n\n" + vt
	}

	if err := verifyAnimationRefs(css); err != nil {
		return nil, err
	}

	return &CompiledPage{
		PreviewNotice:     ctx.Text("workbench.preview.readonly", "模拟预览：不会提交真实数据，请在发布页面中操作。"),
		PreviewReadOnly:   cfg.mode != CompileModePublish,
		PreviewState:      source.PreviewStateFrom(cfg.ctx),
		Lang:              lang,
		Title:             seoTitle,
		MetaDescription:   seoDescription,
		SEOHead:           seoHead,
		GA4Head:           buildGA4Head(cfg.ga4MeasurementID),
		SearchConsoleHead: buildSearchConsoleHead(cfg.searchConsoleVerification),
		HeadScripts:       buildHeadScripts(cfg.headScripts),
		BodyScripts:       buildBodyScripts(cfg.bodyScripts),
		TrackConfig:       buildTrackConfig(cfg.projectID, lang),
		BodyClasses:       classes,
		HTML:              htmlBuf.String(),
		CSS:               css,
		ThemeVarsCSS:      ThemeVarsCSS(cfg.theme),
		EnhanceSource:     cfg.enhanceSource,
		TrackSource:       cfg.trackSource,
		UISources:         cfg.uiSources,
		UIStyle:           cfg.uiStyle,
		Features:          features,
	}, nil
}

func RenderNodeHTML(set *jet.Set, node *core.Node, ctx *core.RenderContext) (string, error) {
	if node == nil {
		return "", fmt.Errorf("渲染节点为空")
	}
	if ctx == nil {
		return "", fmt.Errorf("渲染上下文为空")
	}

	fragCtx := *ctx
	fragCtx.CSS = core.DiscardCSS()

	fragCtx.Features = nil
	view, err := nodeViewOf(node, true, &fragCtx)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := renderView(set, view, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func RenderDocument(c *CompiledPage) (string, error) {
	scan := c.featureScan()
	uiCSS, uiScript, err := uiAssetsForScan(scan, c.UIStyle, c.UISources)
	if err != nil {
		return "", fmt.Errorf("组装文档控件资源失败: %w", err)
	}

	lang := strings.TrimSpace(c.Lang)
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	tracking := c.TrackConfig + c.TrackSource
	ga4Head := c.GA4Head
	if c.PreviewReadOnly {
		tracking = ""
		ga4Head = ""
	}
	v := documentView{
		PreviewNotice:     c.PreviewNotice,
		PreviewReadOnly:   c.PreviewReadOnly,
		PreviewState:      c.PreviewState,
		Lang:              lang,
		Dir:               DirAttr(lang),
		Title:             c.Title,
		MetaDescription:   c.MetaDescription,
		SEOHead:           c.SEOHead,
		GA4Head:           ga4Head,
		SearchConsoleHead: c.SearchConsoleHead,

		HeadScripts:  c.HeadScripts,
		BodyScripts:  c.BodyScripts,
		BodyClass:    strings.Join(c.BodyClasses, " "),
		HTML:         c.HTML,
		CSS:          c.CSS + uiCSS,
		ThemeVarsCSS: c.ThemeVarsCSS,

		EnhanceScript: tracking + enhanceScriptFor(scan.attrs, c.EnhanceSource) + uiScript,
	}
	var sb strings.Builder
	if err := documentTemplate().Execute(&sb, nil, v); err != nil {

		return "", fmt.Errorf("渲染 document.jet 失败: %w", err)
	}
	return sb.String(), nil
}

func DirAttr(lang string) string {
	if LocaleDirection(lang) == "rtl" {
		return "rtl"
	}
	return ""
}

type documentView struct {
	PreviewNotice     string
	PreviewReadOnly   bool
	PreviewState      source.PreviewState
	Lang              string
	Dir               string
	Title             string
	MetaDescription   string
	SEOHead           string
	GA4Head           string
	SearchConsoleHead string
	HeadScripts       string
	BodyScripts       string
	BodyClass         string
	HTML              string
	CSS               string
	ThemeVarsCSS      string
	EnhanceScript     string
}

var (
	documentTplOnce sync.Once
	documentTpl     *jet.Template
)

func documentTemplate() *jet.Template {
	documentTplOnce.Do(func() {
		loader := jet.NewInMemLoader()
		loader.Set("document", documentJetSrc)
		t, err := jet.NewSet(loader).GetTemplate("document")
		if err != nil {
			panic(fmt.Sprintf("加载 document.jet 失败: %v", err))
		}
		documentTpl = t
	})
	return documentTpl
}

//go:embed document.jet
var documentJetSrc string
