package core

import (
	"fmt"
	"strings"
)

// 视图构建辅助：组件骨架与业务扩展共用，纯渲染，不含业务语义。

func DeclareViewFeatures(view any, ctx *RenderContext) {
	if ctx == nil || ctx.Features == nil {
		return
	}
	declarer, ok := view.(ViewFeatureDeclarer)
	if !ok {
		return
	}
	attrs, classes := declarer.DeclareFeatures()
	ctx.UseAttr(attrs...)
	ctx.UseClass(classes...)
}

func AdvancedClasses[P any](node *Node, p *P, ctx *RenderContext) (classes []string, customID string) {
	classes = []string{NodeClass(node.ID)}
	if adv := AdvancedOf(p); adv != nil {

		if revealActive(ctx) {
			applyScrollReveal(&adv.Interaction, ctx.RevealDefaultEntrance)
		}
		extra, id := CompileAdvanced(node.ID, adv, ctx.CSS)
		classes = append(classes, extra...)
		customID = id
	}
	return classes, customID
}

func ApplyI18n(view any, ctx *RenderContext) {
	if aware, ok := view.(I18nAware); ok {
		aware.ApplyI18n(ctx.Text)
	}
}

func ContentAtomViewOf[P any, V any](
	node *Node,
	topLevel bool,
	ctx *RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *CSSBuckets),
	buildView func(p *P, content ContentResolver) (V, error),
) (*NodeView, error) {
	p, err := DecodeProps[P](node.Props)
	if err != nil {
		return nil, err
	}
	classes, customID := AdvancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view, err := buildView(&p, ctx.Content)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	ApplyI18n(&view, ctx)
	DeclareViewFeatures(&view, ctx)
	return &NodeView{
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

func AtomViewOf[P any, V any](
	node *Node,
	topLevel bool,
	ctx *RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *CSSBuckets),
	buildView func(p *P) V,
) (*NodeView, error) {
	p, err := DecodeProps[P](node.Props)
	if err != nil {
		return nil, err
	}
	classes, customID := AdvancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view := buildView(&p)

	if aware, ok := any(&view).(ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		AddImageSkeletonCSS(ctx.CSS)
	}

	ApplyI18n(&view, ctx)
	DeclareViewFeatures(&view, ctx)
	return &NodeView{
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

func LeafViewOf[P any, V any](
	node *Node,
	topLevel bool,
	ctx *RenderContext,
	typeName, template string,
	compileCSS func(id string, p *P, b *CSSBuckets),
	buildView func(p *P) V,
) (*NodeView, error) {
	p, err := DecodeProps[P](node.Props)
	if err != nil {
		return nil, err
	}

	classes, customID := AdvancedClasses(node, &p, ctx)
	compileCSS(node.ID, &p, ctx.CSS)
	view := buildView(&p)

	if aware, ok := any(&view).(ImageLoadingAware); ok && aware.ApplyImageLoading(ctx.ImageDefaults) {
		AddImageSkeletonCSS(ctx.CSS)
	}

	ApplyI18n(&view, ctx)
	DeclareViewFeatures(&view, ctx)
	return &NodeView{
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

// revealActive 判断是否应用滚动入场默认值。
func revealActive(ctx *RenderContext) bool {
	return ctx != nil && ctx.RevealInherit == "on" && ctx.RevealDefaultEntrance != ""
}

// applyScrollReveal 在未显式设置入场时套用默认入场。
func applyScrollReveal(inter *InteractionProps, defaultEntrance string) {
	if inter.Entrance != "" || inter.ScrollStory != "" || inter.ScrollReveal != "" {
		return
	}
	inter.Entrance = defaultEntrance
	inter.ScrollReveal = "reveal"
}
