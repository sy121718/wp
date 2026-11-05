package builder

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"io"
	"strconv"
	"strings"

	"github.com/CloudyKit/jet/v6"

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

func nodeViewFromCore(v *core.NodeView) *core.NodeView {
	if v == nil {
		return nil
	}
	out := &core.NodeView{
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


func init() { core.NodeViewDispatch = NodeViewOf }

func NodeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (view *core.NodeView, err error) {
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

	// 业务组件的视图由扩展在装配时登记；工具内核只查表，不认识具体扩展。
	if build, ok := core.NodeViewBuilderOf(node.Type); ok {
		return build(node, topLevel, ctx)
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
	case ratingPkg.Type:
		return ratingViewOf(node, topLevel, ctx)
	case formPkg.Type:
		return formViewOf(node, topLevel, ctx)
	default:

		if strings.HasPrefix(node.Type, pluginTypePrefix) {
			return pluginViewOf(node, topLevel, ctx)
		}
		return nil, fmt.Errorf("nodeView: 不支持的组件类型 %q", node.Type)
	}
}

func DecodeProps[P any](node *core.Node) (P, error) {

	p, err := core.DecodeProps[P](node.Props)
	if err != nil {
		return p, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
	}
	if err := core.NormalizeRichMediaProps(&p); err != nil {
		return p, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return p, nil
}

func buttonViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
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
	core.DeclareViewFeatures(&view, ctx)

	return &core.NodeView{
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

func containerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := DecodeProps[containerPkg.Props](node)
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

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	cls := strings.Join(classes, " ")
	if topLevel {
		cls += " " + core.SectionClass
	}

	children := make([]*core.NodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := NodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	containerPkg.CompileCSS(node.ID, &p, ctx.CSS)

	view := containerPkg.BuildView(node, &p)
	core.DeclareViewFeatures(&view, ctx)

	return &core.NodeView{
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

func headingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.ContentAtomViewOf(node, topLevel, ctx, headingPkg.Type, "heading", headingPkg.CompileCSS, headingPkg.BuildView)
}

func textViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.ContentAtomViewOf(node, topLevel, ctx, textPkg.Type, "text", textPkg.CompileCSS, textPkg.BuildView)
}

func imageViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := DecodeProps[imagePkg.Props](node)
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

	return &core.NodeView{
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

func dividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, dividerPkg.Type, "divider", dividerPkg.CompileCSS, dividerPkg.BuildView)
}

func spacerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, spacerPkg.Type, "spacer", spacerPkg.CompileCSS, spacerPkg.BuildView)
}

func shapedividerViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, shapedividerPkg.Type, "shapedivider", shapedividerPkg.CompileCSS, shapedividerPkg.BuildView)
}

func loaderViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, loaderPkg.Type, "loader", loaderPkg.CompileCSS, loaderPkg.BuildView)
}

func tableViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, tablePkg.Type, "table", tablePkg.CompileCSS, tablePkg.BuildView)
}

func cardViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {

	return core.AtomViewOf(node, topLevel, ctx, cardPkg.Type, "card", cardPkg.CompileCSS, func(p *cardPkg.Props) cardPkg.View {
		return cardPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func faqViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, faqPkg.Type, "faq", faqPkg.CompileCSS, faqPkg.BuildView)
}

func quoteViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {

	return core.AtomViewOf(node, topLevel, ctx, quotePkg.Type, "quote", quotePkg.CompileCSS, func(p *quotePkg.Props) quotePkg.View {
		return quotePkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func countdownViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, countdownPkg.Type, "countdown", countdownPkg.CompileCSS, countdownPkg.BuildView)
}

func iconViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, iconPkg.Type, "icon", iconPkg.CompileCSS, iconPkg.BuildView)
}

func badgeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, badgePkg.Type, "badge", badgePkg.CompileCSS, badgePkg.BuildView)
}

func breadcrumbViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.LeafViewOf(node, topLevel, ctx, breadcrumbPkg.Type, "breadcrumb", breadcrumbPkg.CompileCSS, func(p *breadcrumbPkg.Props) breadcrumbPkg.View {
		return breadcrumbPkg.BuildView(p, ctx)
	})
}

func progressViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, progressPkg.Type, "progress", progressPkg.CompileCSS, progressPkg.BuildView)
}

func ratingViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, ratingPkg.Type, "rating", ratingPkg.CompileCSS, ratingPkg.BuildView)
}

func formViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.AtomViewOf(node, topLevel, ctx, formPkg.Type, "form", formPkg.CompileCSS, formPkg.BuildView)
}

func listViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {

	return core.LeafViewOf(node, topLevel, ctx, listPkg.Type, "list", listPkg.CompileCSS, func(p *listPkg.Props) listPkg.View {
		return listPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func infoboxViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {

	return core.LeafViewOf(node, topLevel, ctx, infoboxPkg.Type, "infobox", infoboxPkg.CompileCSS, func(p *infoboxPkg.Props) infoboxPkg.View {
		return infoboxPkg.BuildView(p, ctx.ResolveSiteLink)
	})
}

func socialbuttonsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.LeafViewOf(node, topLevel, ctx, socialbuttonsPkg.Type, "socialbuttons", socialbuttonsPkg.CompileCSS, socialbuttonsPkg.BuildView)
}

func videoViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.LeafViewOf(node, topLevel, ctx, videoPkg.Type, "video", videoPkg.CompileCSS, videoPkg.BuildView)
}

func counterViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.LeafViewOf(node, topLevel, ctx, counterPkg.Type, "counter", counterPkg.CompileCSS, counterPkg.BuildView)
}

func galleryViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := DecodeProps[galleryPkg.Props](node)
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

	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)

	if view.Visible {
		galleryPkg.CompileCSS(node.ID, &p, ctx.CSS)

		if aware, ok := any(&view).(core.ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
			core.AddImageSkeletonCSS(ctx.CSS)
		}
	}

	return &core.NodeView{
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

func sliderViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := DecodeProps[sliderPkg.Props](node)
	if err != nil {
		return nil, err
	}

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	cls := strings.Join(classes, " ")
	if topLevel {
		cls += " " + core.SectionClass
	}

	children := make([]*core.NodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := NodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	sliderPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := sliderPkg.BuildView(node, &p)
	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)

	return &core.NodeView{
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

func languagesViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p languagesPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	languagesPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := languagesPkg.BuildView(node, &p, ctx)
	core.ApplyI18n(&view, ctx)
	return &core.NodeView{
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

func tabsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p tabsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*core.NodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := NodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	tabsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := tabsPkg.BuildView(node, &p)

	return &core.NodeView{
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

func accordionViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p accordionPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*core.NodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := NodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	accordionPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := accordionPkg.BuildView(&p)

	return &core.NodeView{
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

func marqueeViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p marqueePkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	children := make([]*core.NodeView, 0, len(node.Children))
	for _, child := range node.Children {
		cv, err := NodeViewOf(child, false, ctx)
		if err != nil {
			return nil, err
		}
		children = append(children, cv)
	}

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	marqueePkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := marqueePkg.BuildView(&p)

	return &core.NodeView{
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

func globalrefViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	blockID, err := globalrefPkg.BlockIDOf(node)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	ctx.UseBlock(blockID)

	return blockRefViewOf(node, topLevel, ctx, globalrefPkg.Type, "globalref", blockID, "")
}

func layoutSlotViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	blockID, err := layoutslotPkg.BlockIDOf(node)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	slot, _ := layoutslotPkg.SlotOf(node)
	return blockRefViewOf(node, topLevel, ctx, layoutslotPkg.Type, "layoutslot", blockID, slot)
}

func blockRefViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext, refType, template, blockID, slot string) (*core.NodeView, error) {
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
		return &core.NodeView{
			Type: refType, Template: template, NodeID: node.ID,
			Classes: core.NodeClass(node.ID), TopLevel: topLevel, V: view,
		}, nil
	}

	ctx.BlockStack = append(ctx.BlockStack, blockID)
	children := make([]*core.NodeView, 0, len(roots))
	for _, r := range roots {
		cv, cerr := NodeViewOf(r, false, ctx)
		if cerr != nil {
			ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1]
			return nil, cerr
		}
		children = append(children, cv)
	}
	ctx.BlockStack = ctx.BlockStack[:len(ctx.BlockStack)-1]

	frame := &core.NodeView{
		Type: refType, Template: template, NodeID: node.ID,
		Classes: core.NodeClass(node.ID), TopLevel: topLevel,
		Children: children, V: view,
	}

	if slot != "" && ctx.CanvasSlotFrames {
		frame.SlotFrame = &core.SlotFrame{
			Slot: slot, Ref: blockID, RefKind: SlotRefKind(blockID), NodeID: node.ID,
		}
	}
	return frame, nil
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
			rv, verr := NodeViewOf(r, false, ctx)
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

func renderView(set *jet.Set, root *core.NodeView, w io.Writer) error {
	tpl, err := set.GetTemplate(root.Template)
	if err != nil {
		return fmt.Errorf("获取组件模板 %q 失败: %w", root.Template, err)
	}
	return tpl.Execute(w, nil, root)
}
