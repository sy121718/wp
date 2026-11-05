package builder

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"html/template"
	"io"
	"strconv"
	"strings"

	"github.com/CloudyKit/jet/v6"

	addtocartPkg "go_wp/internal/builder/extensions/addtocart"
	articlelistPkg "go_wp/internal/builder/extensions/articlelist"
	cardstackPkg "go_wp/internal/builder/extensions/cardstack"
	carticonPkg "go_wp/internal/builder/extensions/carticon"
	checkoutformPkg "go_wp/internal/builder/extensions/checkoutform"
	navPkg "go_wp/internal/builder/extensions/nav"
	orderlistPkg "go_wp/internal/builder/extensions/orderlist"
	productPkg "go_wp/internal/builder/extensions/product"
	productcardPkg "go_wp/internal/builder/extensions/productcard"
	productlistPkg "go_wp/internal/builder/extensions/productlist"
	productselectorPkg "go_wp/internal/builder/extensions/productselector"
	searchresultsPkg "go_wp/internal/builder/extensions/searchresults"
	userformsPkg "go_wp/internal/builder/extensions/userforms"
	"go_wp/internal/builder/productbinding"
	accordionPkg "go_wp/pkg/builder/components/accordion"
	badgePkg "go_wp/pkg/builder/components/badge"
	breadcrumbPkg "go_wp/pkg/builder/components/breadcrumb"
	buttonPkg "go_wp/pkg/builder/components/button"
	cardPkg "go_wp/pkg/builder/components/card"
	containerPkg "go_wp/pkg/builder/components/container"
	countdownPkg "go_wp/pkg/builder/components/countdown"
	counterPkg "go_wp/pkg/builder/components/counter"
	dividerPkg "go_wp/pkg/builder/components/divider"
	faqPkg "go_wp/pkg/builder/components/faq"
	formPkg "go_wp/pkg/builder/components/form"
	galleryPkg "go_wp/pkg/builder/components/gallery"
	globalrefPkg "go_wp/pkg/builder/components/globalref"
	headingPkg "go_wp/pkg/builder/components/heading"
	iconPkg "go_wp/pkg/builder/components/icon"
	imagePkg "go_wp/pkg/builder/components/image"
	infoboxPkg "go_wp/pkg/builder/components/infobox"
	languagesPkg "go_wp/pkg/builder/components/languages"
	layoutslotPkg "go_wp/pkg/builder/components/layoutslot"
	listPkg "go_wp/pkg/builder/components/list"
	loaderPkg "go_wp/pkg/builder/components/loader"
	marqueePkg "go_wp/pkg/builder/components/marquee"
	progressPkg "go_wp/pkg/builder/components/progress"
	quotePkg "go_wp/pkg/builder/components/quote"
	ratingPkg "go_wp/pkg/builder/components/rating"
	shapedividerPkg "go_wp/pkg/builder/components/shapedivider"
	sliderPkg "go_wp/pkg/builder/components/slider"
	socialbuttonsPkg "go_wp/pkg/builder/components/socialbuttons"
	spacerPkg "go_wp/pkg/builder/components/spacer"
	tablePkg "go_wp/pkg/builder/components/table"
	tabsPkg "go_wp/pkg/builder/components/tabs"
	textPkg "go_wp/pkg/builder/components/text"
	videoPkg "go_wp/pkg/builder/components/video"
	"go_wp/pkg/builder/core"
)

func nodeViewFromCore(v *core.NodeView) *nodeView {
	if v == nil {
		return nil
	}
	out := &nodeView{
		Type: v.Type, Template: v.Template, NodeID: v.NodeID, Classes: v.Classes,
		CustomID: v.CustomID, TopLevel: v.TopLevel, Props: v.Props, V: v.V,
		Tag: v.Tag, Attrs: v.Attrs, Text: v.Text, IconPrefix: v.IconPrefix,
		IconSuffix: v.IconSuffix, ShapeTop: v.ShapeTop, ShapeBottom: v.ShapeBottom,
		BgSlides: v.BgSlides,
	}
	for _, child := range v.Children {
		out.Children = append(out.Children, nodeViewFromCore(child))
	}
	return out
}

type nodeView struct {
	Type     string
	Template string
	NodeID   string
	Classes  string
	CustomID string
	TopLevel bool
	Props    any
	Children []*nodeView

	V any

	SlotFrame *SlotFrame

	Tag         string
	Attrs       string
	Text        string
	IconPrefix  string
	IconSuffix  string
	ShapeTop    string
	ShapeBottom string
	BgSlides    []string
}

func nodeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (view *nodeView, err error) {
	// 组件注册表里的可选 BuildNodeView 入口优先：通用组件与业务扩展都经它渲染，
	// 不再由本文件按类型硬编码分发。
	if component, lookupErr := core.Lookup(node.Type); lookupErr == nil {
		if builder, ok := component.(core.ViewBuilder); ok {
			built, buildErr := builder.BuildNodeView(node, topLevel, ctx)
			if buildErr != nil {
				return nil, buildErr
			}
			if built != nil {
				return nodeViewFromCore(built), nil
			}
		}
	}

	defer func() {
		if err != nil && ctx != nil && ctx.AllowIncomplete && errors.Is(err, core.ErrIncompleteNode) {
			view, err = incompleteViewOf(node, topLevel, ctx), nil
		}
	}()
	if ctx != nil && ctx.IncompleteNodes[node.ID] {
		return incompleteViewOf(node, topLevel, ctx), nil
	}

	if ctx != nil {
		node = applyContentTranslation(node, ctx.ContentTranslate)
	}

	switch node.Type {
	case buttonPkg.Type:
		return buttonViewOf(node, topLevel, ctx)
	case containerPkg.Type:
		return containerViewOf(node, topLevel, ctx)
	case headingPkg.Type:
		return headingViewOf(node, topLevel, ctx)
	case textPkg.Type:
		return textViewOf(node, topLevel, ctx)
	case imagePkg.Type:
		return imageViewOf(node, topLevel, ctx)
	case dividerPkg.Type:
		return dividerViewOf(node, topLevel, ctx)
	case spacerPkg.Type:
		return spacerViewOf(node, topLevel, ctx)
	case shapedividerPkg.Type:
		return shapedividerViewOf(node, topLevel, ctx)
	case loaderPkg.Type:
		return loaderViewOf(node, topLevel, ctx)
	case listPkg.Type:
		return listViewOf(node, topLevel, ctx)
	case infoboxPkg.Type:
		return infoboxViewOf(node, topLevel, ctx)
	case socialbuttonsPkg.Type:
		return socialbuttonsViewOf(node, topLevel, ctx)
	case videoPkg.Type:
		return videoViewOf(node, topLevel, ctx)
	case counterPkg.Type:
		return counterViewOf(node, topLevel, ctx)
	case galleryPkg.Type:
		return galleryViewOf(node, topLevel, ctx)
	case sliderPkg.Type:
		return sliderViewOf(node, topLevel, ctx)
	case navPkg.Type:
		return navViewOf(node, topLevel, ctx)
	case languagesPkg.Type:
		return languagesViewOf(node, topLevel, ctx)
	case tabsPkg.Type:
		return tabsViewOf(node, topLevel, ctx)
	case accordionPkg.Type:
		return accordionViewOf(node, topLevel, ctx)
	case marqueePkg.Type:
		return marqueeViewOf(node, topLevel, ctx)
	case layoutslotPkg.Type:
		return layoutSlotViewOf(node, topLevel, ctx)
	case globalrefPkg.Type:
		return globalrefViewOf(node, topLevel, ctx)
	case tablePkg.Type:
		return tableViewOf(node, topLevel, ctx)
	case cardPkg.Type:
		return cardViewOf(node, topLevel, ctx)
	case cardstackPkg.Type:
		return cardstackViewOf(node, topLevel, ctx)
	case faqPkg.Type:
		return faqViewOf(node, topLevel, ctx)
	case quotePkg.Type:
		return quoteViewOf(node, topLevel, ctx)
	case countdownPkg.Type:
		return countdownViewOf(node, topLevel, ctx)
	case iconPkg.Type:
		return iconViewOf(node, topLevel, ctx)
	case badgePkg.Type:
		return badgeViewOf(node, topLevel, ctx)
	case breadcrumbPkg.Type:
		return breadcrumbViewOf(node, topLevel, ctx)
	case progressPkg.Type:
		return progressViewOf(node, topLevel, ctx)
	case productPkg.Type:
		return productViewOf(node, topLevel, ctx)
	case productcardPkg.Type:
		return productCardViewOf(node, topLevel, ctx)
	case articlelistPkg.Type:
		return articleListViewOf(node, topLevel, ctx)
	case productlistPkg.Type:
		return productListViewOf(node, topLevel, ctx)
	case productselectorPkg.Type:
		return productSelectorViewOf(node, topLevel, ctx)
	case addtocartPkg.Type:
		return addToCartViewOf(node, topLevel, ctx)
	case carticonPkg.Type:
		return cartIconViewOf(node, topLevel, ctx)
	case orderlistPkg.Type:
		return orderListViewOf(node, topLevel, ctx)
	case searchresultsPkg.Type:
		return searchResultsViewOf(node, topLevel, ctx)
	case userformsPkg.Type:
		return userFormsViewOf(node, topLevel, ctx)
	case ratingPkg.Type:
		return ratingViewOf(node, topLevel, ctx)
	case formPkg.Type:
		return formViewOf(node, topLevel, ctx)
	case checkoutformPkg.Type:
		return checkoutFormViewOf(node, topLevel, ctx)
	default:

		if strings.HasPrefix(node.Type, pluginTypePrefix) {
			return pluginViewOf(node, topLevel, ctx)
		}
		return nil, fmt.Errorf("nodeView: 不支持的组件类型 %q", node.Type)
	}
}

func declareViewFeatures(view any, ctx *core.RenderContext) {
	if ctx == nil || ctx.Features == nil {
		return
	}
	declarer, ok := view.(core.ViewFeatureDeclarer)
	if !ok {
		return
	}
	attrs, classes := declarer.DeclareFeatures()
	ctx.UseAttr(attrs...)
	ctx.UseClass(classes...)
}

func decodeProps[P any](node *core.Node) (P, error) {

	p, err := core.DecodeProps[P](node.Props)
	if err != nil {
		return p, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
	}
	if err := core.NormalizeRichMediaProps(&p); err != nil {
		return p, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return p, nil
}

func advancedClasses[P any](node *core.Node, p *P, ctx *core.RenderContext) (classes []string, customID string) {
	classes = []string{core.NodeClass(node.ID)}
	if adv := core.AdvancedOf(p); adv != nil {

		if revealActive(ctx) {
			applyScrollReveal(&adv.Interaction, ctx.RevealDefaultEntrance)
		}
		extra, id := core.CompileAdvanced(node.ID, adv, ctx.CSS)
		classes = append(classes, extra...)
		customID = id
	}
	return classes, customID
}

func applyI18n(view any, ctx *core.RenderContext) {
	if aware, ok := view.(core.I18nAware); ok {
		aware.ApplyI18n(ctx.Text)
	}
}

func atomViewOf[P any, V any](
	node *core.Node,
	topLevel bool,
	ctx *core.RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *core.CSSBuckets),
	buildView func(p *P) V,
) (*nodeView, error) {
	p, err := decodeProps[P](node)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view := buildView(&p)

	if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		core.AddImageSkeletonCSS(ctx.CSS)
	}

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     typeName,
		Template: template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func contentAtomViewOf[P any, V any](
	node *core.Node,
	topLevel bool,
	ctx *core.RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *core.CSSBuckets),
	buildView func(p *P, content core.ContentResolver) (V, error),
) (*nodeView, error) {
	p, err := decodeProps[P](node)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view, err := buildView(&p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     typeName,
		Template: template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func leafViewOf[P any, V any](
	node *core.Node,
	topLevel bool,
	ctx *core.RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *core.CSSBuckets),
	buildView func(p *P) V,
) (*nodeView, error) {
	p, err := decodeProps[P](node)
	if err != nil {
		return nil, err
	}

	classes, customID := advancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view := buildView(&p)

	if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		core.AddImageSkeletonCSS(ctx.CSS)
	}

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     typeName,
		Template: template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func buttonViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p buttonPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	buttonPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view, err := buttonPkg.BuildView(&p, ctx.Content, ctx.ResolveSiteLink)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		core.AddImageSkeletonCSS(ctx.CSS)
	}
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:       buttonPkg.Type,
		Template:   "button",
		NodeID:     node.ID,
		Classes:    strings.Join(classes, " "),
		CustomID:   customID,
		TopLevel:   topLevel,
		Props:      p,
		V:          view,
		Tag:        view.Tag,
		Attrs:      view.Attrs,
		Text:       view.Text,
		IconPrefix: view.IconPrefix,
		IconSuffix: view.IconSuffix,
	}, nil
}

func containerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := decodeProps[containerPkg.Props](node)
	if err != nil {
		return nil, err
	}

	if revealActive(ctx) {
		applyScrollReveal(&p.Interaction, ctx.RevealDefaultEntrance)
	}

	switch p.StyleEx.Reveal {
	case "on":
		old := ctx.RevealInherit
		ctx.RevealInherit = "on"
		defer func() { ctx.RevealInherit = old }()
	case "off":
		old := ctx.RevealInherit
		ctx.RevealInherit = "off"
		defer func() { ctx.RevealInherit = old }()
	}

	classes, customID := advancedClasses(node, &p, ctx)
	cls := strings.Join(classes, " ")
	if topLevel {
		cls += " " + core.SectionClass
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	containerPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view := containerPkg.BuildView(node, &p)
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:        containerPkg.Type,
		Template:    "container",
		NodeID:      node.ID,
		Classes:     cls,
		CustomID:    customID,
		TopLevel:    topLevel,
		Props:       p,
		Children:    children,
		Tag:         view.Tag,
		Attrs:       view.Attrs,
		ShapeTop:    view.ShapeTop,
		ShapeBottom: view.ShapeBottom,
		BgSlides:    view.BgSlides,
	}, nil
}

func productViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, productPkg.Type, "product", productPkg.CompileCSS,

		func(p *productPkg.Props, content core.ContentResolver) (productPkg.View, error) {
			return productPkg.BuildView(p, content, ctx.ProjectID, ctx.Lang, ctx.ResolveSiteLink)
		})
}

func productCardViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {

	return contentAtomViewOf(node, topLevel, ctx, productcardPkg.Type, "product_card", productcardPkg.CompileCSS, func(p *productcardPkg.Props, content core.ContentResolver) (productcardPkg.View, error) {
		return productcardPkg.BuildView(p, content, ctx.ResolveSiteLink)
	})
}

func headingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, headingPkg.Type, "heading", headingPkg.CompileCSS, headingPkg.BuildView)
}

func textViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, textPkg.Type, "text", textPkg.CompileCSS, textPkg.BuildView)
}

func imageViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := decodeProps[imagePkg.Props](node)
	if err != nil {
		return nil, err
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)
	classStr := strings.Join(classes, " ")

	imagePkg.CompileCSS(node.ID, &p, ctx.CSS)

	view, err := imagePkg.BuildView(node, &p, classStr, customID, ctx.Content, ctx.ImageDefaults, ctx.AssetProbe, ctx.ResolveSiteLink)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	if view.Skeleton {
		view.Class = core.ImageSkeletonClass(classStr, true)
		core.AddImageSkeletonCSS(ctx.CSS)
	}

	return &nodeView{
		Type:     imagePkg.Type,
		Template: "image",
		NodeID:   node.ID,
		Classes:  classStr,
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func dividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, dividerPkg.Type, "divider", dividerPkg.CompileCSS, dividerPkg.BuildView)
}

func spacerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, spacerPkg.Type, "spacer", spacerPkg.CompileCSS, spacerPkg.BuildView)
}

func shapedividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, shapedividerPkg.Type, "shapedivider", shapedividerPkg.CompileCSS, shapedividerPkg.BuildView)
}

func loaderViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, loaderPkg.Type, "loader", loaderPkg.CompileCSS, loaderPkg.BuildView)
}

func tableViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, tablePkg.Type, "table", tablePkg.CompileCSS, tablePkg.BuildView)
}

func cardViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {

	return atomViewOf(node, topLevel, ctx, cardPkg.Type, "card", cardPkg.CompileCSS, func(p *cardPkg.Props) cardPkg.View {
		return cardPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

type cardstackView struct {
	cardstackPkg.View

	CardNodes [][]*nodeView

	HasCardNodes bool
}

func cardstackViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := decodeProps[cardstackPkg.Props](node)
	if err != nil {
		return nil, err
	}

	base, err := cardstackPkg.BuildView(node, &p, ctx)
	if err != nil {
		return nil, err
	}
	view := cardstackView{View: base}

	children := make([]*nodeView, 0, len(node.Children))
	isCollection := cardstackPkg.IsCollection(&p)
	for _, child := range node.Children {

		if isCollection {
			continue
		}
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	if isCollection && len(node.Children) > 0 {
		items, ierr := cardstackPkg.CollectionItems(node, &p, ctx)
		if ierr != nil {
			return nil, ierr
		}
		view.CardNodes = make([][]*nodeView, 0, len(items))
		for _, item := range items {
			itemCtx := *ctx
			itemCtx.Content = core.ItemScope{Inner: ctx.Content, Item: item}
			group := make([]*nodeView, 0, len(node.Children))
			for _, child := range node.Children {
				cv, cerr := nodeViewOf(child, false, &itemCtx)
				if cerr != nil {
					return nil, cerr
				}
				group = append(group, cv)
			}
			view.CardNodes = append(view.CardNodes, group)
		}
		view.HasCardNodes = len(view.CardNodes) > 0
	}

	classes, customID := advancedClasses(node, &p, ctx)
	cardstackPkg.CompileCSS(node, &p, len(base.Cards), ctx.CSS)

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:     cardstackPkg.Type,
		Template: "cardstack",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

func productSelectorViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return contentAtomViewOf(node, topLevel, ctx, productselectorPkg.Type, "product_selector", productselectorPkg.CompileCSS,

		func(p *productselectorPkg.Props, content core.ContentResolver) (productselectorPkg.View, error) {
			content, err := productbinding.Resolve(ctx, p.ProductSource, p.ProductID)
			if err != nil {
				return productselectorPkg.View{}, err
			}
			return productselectorPkg.BuildView(p, content, ctx.ProjectID, ctx.Lang)
		})
}

func cartIconViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p carticonPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	carticonPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := carticonPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     carticonPkg.Type,
		Template: "cart_icon",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func searchResultsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p searchresultsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	searchresultsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := searchresultsPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     searchresultsPkg.Type,
		Template: "search_widget",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func orderListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := core.DecodeProps[orderlistPkg.Props](node.Props)
	if err != nil {
		return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
	}
	classes, customID := advancedClasses(node, &p, ctx)
	orderlistPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := orderlistPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{Type: orderlistPkg.Type, Template: "orders_widget", NodeID: node.ID, Classes: strings.Join(classes, " "), CustomID: customID, TopLevel: topLevel, Props: p, V: view}, nil
}

func userFormsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p userformsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	userformsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := userformsPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     userformsPkg.Type,
		Template: "user_forms_widget",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func addToCartViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := core.DecodeProps[addtocartPkg.Props](node.Props)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	entityType, entityID := ctx.ArchiveEntity()
	if entityType != "product" || entityID == "" {
		return nil, fmt.Errorf("%w: 请将加购按钮放在商品详情中，按钮会自动使用当前商品", core.ErrIncompleteNode)
	}
	classes, customID := advancedClasses(node, &p, ctx)
	addtocartPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view, err := addtocartPkg.BuildView(&p, ctx.Content, ctx.ProjectID, ctx.Lang)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     addtocartPkg.Type,
		Template: "add_to_cart",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func articleListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p articlelistPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	view, err := articlelistPkg.BuildView(node, &p, ctx)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	articlelistPkg.CompileCSS(node.ID, &p, ctx.CSS)
	return &nodeView{
		Type:     articlelistPkg.Type,
		Template: "article_list",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func productListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p productlistPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	view, err := productlistPkg.BuildView(node, &p, ctx)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	productlistPkg.CompileCSS(node.ID, &p, ctx.CSS)

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)
	return &nodeView{
		Type:     productlistPkg.Type,
		Template: "product_list",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func faqViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, faqPkg.Type, "faq", faqPkg.CompileCSS, faqPkg.BuildView)
}

func quoteViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {

	return atomViewOf(node, topLevel, ctx, quotePkg.Type, "quote", quotePkg.CompileCSS, func(p *quotePkg.Props) quotePkg.View {
		return quotePkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func countdownViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, countdownPkg.Type, "countdown", countdownPkg.CompileCSS, countdownPkg.BuildView)
}

func iconViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, iconPkg.Type, "icon", iconPkg.CompileCSS, iconPkg.BuildView)
}

func badgeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, badgePkg.Type, "badge", badgePkg.CompileCSS, badgePkg.BuildView)
}

func breadcrumbViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, breadcrumbPkg.Type, "breadcrumb", breadcrumbPkg.CompileCSS, func(p *breadcrumbPkg.Props) breadcrumbPkg.View {
		return breadcrumbPkg.BuildView(p, ctx)
	})
}

func progressViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, progressPkg.Type, "progress", progressPkg.CompileCSS, progressPkg.BuildView)
}

func ratingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, ratingPkg.Type, "rating", ratingPkg.CompileCSS, ratingPkg.BuildView)
}

func formViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return atomViewOf(node, topLevel, ctx, formPkg.Type, "form", formPkg.CompileCSS, formPkg.BuildView)
}

func listViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {

	return leafViewOf(node, topLevel, ctx, listPkg.Type, "list", listPkg.CompileCSS, func(p *listPkg.Props) listPkg.View {
		return listPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func infoboxViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {

	return leafViewOf(node, topLevel, ctx, infoboxPkg.Type, "infobox", infoboxPkg.CompileCSS, func(p *infoboxPkg.Props) infoboxPkg.View {
		return infoboxPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func socialbuttonsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, socialbuttonsPkg.Type, "socialbuttons", socialbuttonsPkg.CompileCSS, socialbuttonsPkg.BuildView)
}

func videoViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, videoPkg.Type, "video", videoPkg.CompileCSS, videoPkg.BuildView)
}

func counterViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	return leafViewOf(node, topLevel, ctx, counterPkg.Type, "counter", counterPkg.CompileCSS, counterPkg.BuildView)
}

func galleryViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := decodeProps[galleryPkg.Props](node)
	if err != nil {
		return nil, err
	}

	var extraClasses []string
	var customID string
	if adv := core.AdvancedOf(&p); adv != nil {
		extraClasses, customID = core.CompileAdvanced(node.ID, adv, ctx.CSS)
	}
	classes := []string{core.NodeClass(node.ID)}
	classes = append(classes, extraClasses...)

	view, err := galleryPkg.BuildView(node.ID, &p, ctx.Content, ctx.ResolveSiteLink)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)

	if view.Visible {
		galleryPkg.CompileCSS(node.ID, &p, ctx.CSS)

		if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
			core.AddImageSkeletonCSS(ctx.CSS)
		}
	}

	return &nodeView{
		Type:     galleryPkg.Type,
		Template: "gallery",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func sliderViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := decodeProps[sliderPkg.Props](node)
	if err != nil {
		return nil, err
	}

	classes, customID := advancedClasses(node, &p, ctx)
	cls := strings.Join(classes, " ")
	if topLevel {
		cls += " " + core.SectionClass
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	sliderPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := sliderPkg.BuildView(node, &p)
	applyI18n(&view, ctx)
	declareViewFeatures(&view, ctx)

	return &nodeView{
		Type:     sliderPkg.Type,
		Template: "slider",
		NodeID:   node.ID,
		Classes:  cls,
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

func languagesViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p languagesPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := advancedClasses(node, &p, ctx)
	languagesPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := languagesPkg.BuildView(node, &p, ctx)
	applyI18n(&view, ctx)
	return &nodeView{
		Type:     languagesPkg.Type,
		Template: "languages",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func checkoutFormViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	p, err := decodeProps[checkoutformPkg.Props](node)
	if err != nil {
		return nil, err
	}
	classes, customID := advancedClasses(node, &p, ctx)
	checkoutformPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view, err := checkoutformPkg.BuildView(&p, ctx)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	applyI18n(&view, ctx)
	declareViewFeatures(view, ctx)
	return &nodeView{
		Type:     checkoutformPkg.Type,
		Template: "checkoutform",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func navViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p navPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err := resolveNavMenu(node, &p, ctx); err != nil {
		return nil, err
	}

	navPkg.MarkCurrent(p.Items, ctx.CurrentPath)

	if err := renderNavPanels(p.Items, ctx); err != nil {
		return nil, err
	}

	classes, customID := advancedClasses(node, &p, ctx)
	navPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := navPkg.BuildView(node, &p)
	applyI18n(&view, ctx)
	return &nodeView{
		Type:     navPkg.Type,
		Template: "nav",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		V:        view,
	}, nil
}

func resolveNavMenu(node *core.Node, p *navPkg.Props, ctx *core.RenderContext) error {
	navID := strings.TrimSpace(p.Navigation)
	kind := strings.TrimSpace(p.Menu)
	if navID == "" && kind == "" {
		return nil
	}
	if ctx.Navigation == nil || strings.TrimSpace(ctx.ProjectID) == "" {
		return fmt.Errorf("节点 %s: 已绑定导航（位置 %q / 菜单项 %q），但构建期缺少导航解析器或工程 ID（装配未注入）",
			node.ID, kind, navID)
	}

	if navID != "" {
		items, err := ctx.Navigation.ResolveNavigation(ctx.ProjectID, navID)
		if err != nil {
			return fmt.Errorf("节点 %s: 菜单项 %q 解析失败: %w", node.ID, navID, err)
		}

		ctx.UseNavigation(navID)
		p.Items = navPkg.ItemsOf(items)
		return navPkg.ValidateItems(p.Items, node.ID)
	}
	items, err := ctx.Navigation.ResolveMenu(ctx.ProjectID, kind)
	if err != nil {
		return fmt.Errorf("节点 %s: 导航位置 %q 解析失败: %w", node.ID, kind, err)
	}

	ctx.UseMenu(kind)
	p.Items = navPkg.ItemsOf(items)
	return navPkg.ValidateItems(p.Items, node.ID)
}

func tabsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p tabsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	classes, customID := advancedClasses(node, &p, ctx)
	tabsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := tabsPkg.BuildView(node, &p)

	return &nodeView{
		Type:     tabsPkg.Type,
		Template: "tabs",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

func accordionViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p accordionPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	classes, customID := advancedClasses(node, &p, ctx)
	accordionPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := accordionPkg.BuildView(&p)

	return &nodeView{
		Type:     accordionPkg.Type,
		Template: "accordion",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

func marqueeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	var p marqueePkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*nodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := nodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	classes, customID := advancedClasses(node, &p, ctx)
	marqueePkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := marqueePkg.BuildView(&p)

	return &nodeView{
		Type:     marqueePkg.Type,
		Template: "marquee",
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		CustomID: customID,
		TopLevel: topLevel,
		Props:    p,
		Children: children,
		V:        view,
	}, nil
}

const maxBlockExpandDepth = 32

func globalrefViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	blockID, err := globalrefPkg.BlockIDOf(node)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	ctx.UseBlock(blockID)

	return blockRefViewOf(node, topLevel, ctx, globalrefPkg.Type, "globalref", blockID, "")
}

func layoutSlotViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	blockID, err := layoutslotPkg.BlockIDOf(node)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	slot, _ := layoutslotPkg.SlotOf(node)
	return blockRefViewOf(node, topLevel, ctx, layoutslotPkg.Type, "layoutslot", blockID, slot)
}

func blockRefViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext, refType, template, blockID, slot string) (*nodeView, error) {
	for _, id := range ctx.BlockStack {
		if id == blockID {
			return nil, fmt.Errorf("节点 %s: 全局块循环引用（%s → %s）", node.ID, strings.Join(ctx.BlockStack, " → "), blockID)
		}
	}
	if len(ctx.BlockStack) >= maxBlockExpandDepth {
		return nil, fmt.Errorf("节点 %s: 全局块嵌套超过 %d 层上限", node.ID, maxBlockExpandDepth)
	}

	view, roots, err := globalrefPkg.BuildView(node, ctx.Block)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	if view.IsPlaceholder {

		view.Slot = slot

		fail := core.RefFailure{
			NodeID: node.ID, NodeType: refType, BlockID: blockID, Slot: slot, Reason: view.Reason,
		}
		if ferr := ctx.RefFailed(fail); ferr != nil {
			return nil, fmt.Errorf("节点 %s: %w", node.ID, ferr)
		}
		return &nodeView{
			Type: refType, Template: template, NodeID: node.ID,
			Classes: core.NodeClass(node.ID), TopLevel: topLevel, V: view,
		}, nil
	}

	ctx.BlockStack = append(ctx.BlockStack, blockID)
	children := make([]*nodeView, 0, len(roots))
	for _, r := range roots {
		cv, cerr := nodeViewOf(r, false, ctx)
		if cerr != nil {
			ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1]
			return nil, cerr
		}
		children = append(children, cv)
	}
	ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1]

	frame := &nodeView{
		Type: refType, Template: template, NodeID: node.ID,
		Classes: core.NodeClass(node.ID), TopLevel: topLevel,
		Children: children, V: view,
	}

	if slot != "" && ctx.CanvasSlotFrames {
		frame.SlotFrame = &SlotFrame{
			Slot: slot, Ref: blockID, RefKind: SlotRefKind(blockID), NodeID: node.ID,
		}
	}
	return frame, nil
}

func renderNavPanels(items []navPkg.Item, ctx *core.RenderContext) error {
	for i := range items {
		if err := renderNavPanels(items[i].Children, ctx); err != nil {
			return err
		}
		blockID := strings.TrimSpace(items[i].PanelBlockID)
		if blockID == "" {
			continue
		}

		ctx.UseBlock(blockID)
		html, err := ctx.RenderPanel(blockID)
		if err != nil {
			return fmt.Errorf("菜单项 %q 的悬浮面板渲染失败: %w", items[i].Label, err)
		}
		items[i].PanelHTML = template.HTML(html)
	}
	return nil
}

func panelRenderer(ctx *core.RenderContext, resolve core.BlockResolver, set *jet.Set) func(string) (string, error) {
	if resolve == nil || set == nil {
		return nil
	}
	return func(blockID string) (string, error) {
		id := strings.TrimSpace(blockID)
		if id == "" {
			return "", nil
		}
		for _, stacked := range ctx.BlockStack {
			if stacked == id {
				return "", fmt.Errorf("菜单悬浮面板引用的块存在循环引用（%s）", id)
			}
		}
		if len(ctx.BlockStack) >= maxBlockExpandDepth {
			return "", fmt.Errorf("菜单悬浮面板的块嵌套超过 %d 层上限", maxBlockExpandDepth)
		}
		node := &core.Node{
			ID:    "navpanel-" + id,
			Props: json.RawMessage(`{"blockId":` + strconv.Quote(id) + `}`),
		}
		view, roots, err := globalrefPkg.BuildView(node, resolve)
		if err != nil {
			return "", err
		}
		if view.IsPlaceholder {
			return "", nil
		}
		ctx.BlockStack = append(ctx.BlockStack, id)
		defer func() { ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1] }()
		var buf bytes.Buffer
		for _, r := range roots {
			rv, verr := nodeViewOf(r, false, ctx)
			if verr != nil {
				return "", verr
			}
			if verr := renderView(set, rv, &buf); verr != nil {
				return "", verr
			}
		}
		return buf.String(), nil
	}
}

func renderView(set *jet.Set, root *nodeView, w io.Writer) error {
	tpl, err := set.GetTemplate(root.Template)
	if err != nil {
		return fmt.Errorf("获取组件模板 %q 失败: %w", root.Template, err)
	}
	return tpl.Execute(w, nil, root)
}
