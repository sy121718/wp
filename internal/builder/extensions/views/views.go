package views

import (
	"strings"

	"fmt"
	json "go_wp/pkg/jsonx"

	"html/template"

	"go_wp/pkg/builder/core"

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
)

func productViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.ContentAtomViewOf(node, topLevel, ctx, productPkg.Type, "product", productPkg.CompileCSS,

		func(p *productPkg.Props, content core.ContentResolver) (productPkg.View, error) {
			return productPkg.BuildView(p, content, ctx.ProjectID, ctx.Lang, ctx.ResolveSiteLink)
		})
}

func productCardViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {

	return core.ContentAtomViewOf(node, topLevel, ctx, productcardPkg.Type, "product_card", productcardPkg.CompileCSS, func(p *productcardPkg.Props, content core.ContentResolver) (productcardPkg.View, error) {
		return productcardPkg.BuildView(p, content, ctx.ResolveSiteLink)
	})
}

func cardstackViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := core.DecodeProps[cardstackPkg.Props](node.Props)
	if err != nil {
		return nil, err
	}

	base, err := cardstackPkg.BuildView(node, &p, ctx)
	if err != nil {
		return nil, err
	}
	view := cardstackView{View: base}

	children := make([]*core.NodeView, 0, len(node.Children))
	isCollection := cardstackPkg.IsCollection(&p)
	for _, child := range node.Children {

		if isCollection {
			continue
		}
		cv, err := core.NodeViewDispatch(child, false, ctx)
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
		view.CardNodes = make([][]*core.NodeView, 0, len(items))
		for _, item := range items {
			itemCtx := *ctx
			itemCtx.Content = core.ItemScope{Inner: ctx.Content, Item: item}
			group := make([]*core.NodeView, 0, len(node.Children))
			for _, child := range node.Children {
				cv, cerr := core.NodeViewDispatch(child, false, &itemCtx)
				if cerr != nil {
					return nil, cerr
				}
				group = append(group, cv)
			}
			view.CardNodes = append(view.CardNodes, group)
		}
		view.HasCardNodes = len(view.CardNodes) > 0
	}

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	cardstackPkg.CompileCSS(node, &p, len(base.Cards), ctx.CSS)

	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)

	return &core.NodeView{
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

func productSelectorViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	return core.ContentAtomViewOf(node, topLevel, ctx, productselectorPkg.Type, "product_selector", productselectorPkg.CompileCSS,

		func(p *productselectorPkg.Props, content core.ContentResolver) (productselectorPkg.View, error) {
			content, err := productbinding.Resolve(ctx, p.ProductSource, p.ProductID)
			if err != nil {
				return productselectorPkg.View{}, err
			}
			return productselectorPkg.BuildView(p, content, ctx.ProjectID, ctx.Lang)
		})
}

func cartIconViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p carticonPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	carticonPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := carticonPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)
	return &core.NodeView{
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

func searchResultsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p searchresultsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	searchresultsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := searchresultsPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)

	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)
	return &core.NodeView{
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

func orderListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := core.DecodeProps[orderlistPkg.Props](node.Props)
	if err != nil {
		return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	orderlistPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := orderlistPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)
	return &core.NodeView{Type: orderlistPkg.Type, Template: "orders_widget", NodeID: node.ID, Classes: strings.Join(classes, " "), CustomID: customID, TopLevel: topLevel, Props: p, V: view}, nil
}

func userFormsViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	var p userformsPkg.Props
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &p); err != nil {
			return nil, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	userformsPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := userformsPkg.BuildView(&p, ctx.ProjectID, ctx.Lang)
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)
	return &core.NodeView{
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

func addToCartViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := core.DecodeProps[addtocartPkg.Props](node.Props)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	entityType, entityID := ctx.ArchiveEntity()
	if entityType != "product" || entityID == "" {
		return nil, fmt.Errorf("%w: 请将加购按钮放在商品详情中，按钮会自动使用当前商品", core.ErrIncompleteNode)
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	addtocartPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view, err := addtocartPkg.BuildView(&p, ctx.Content, ctx.ProjectID, ctx.Lang)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)
	return &core.NodeView{
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

func articleListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
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
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	articlelistPkg.CompileCSS(node.ID, &p, ctx.CSS)
	return &core.NodeView{
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

func productListViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
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
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	productlistPkg.CompileCSS(node.ID, &p, ctx.CSS)

	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(&view, ctx)
	return &core.NodeView{
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

func checkoutFormViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
	p, err := core.DecodeProps[checkoutformPkg.Props](node.Props)
	if err != nil {
		return nil, err
	}
	classes, customID := core.AdvancedClasses(node, &p, ctx)
	checkoutformPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view, err := checkoutformPkg.BuildView(&p, ctx)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	if err := view.ResolveActions(node, ctx); err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	core.ApplyI18n(&view, ctx)
	core.DeclareViewFeatures(view, ctx)
	return &core.NodeView{
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

func navViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*core.NodeView, error) {
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

	classes, customID := core.AdvancedClasses(node, &p, ctx)
	navPkg.CompileCSS(node.ID, &p, ctx.CSS)
	view := navPkg.BuildView(node, &p)
	core.ApplyI18n(&view, ctx)
	return &core.NodeView{
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

type cardstackView struct {
	cardstackPkg.View

	CardNodes [][]*core.NodeView

	HasCardNodes bool
}

// RegisterAll 把全部业务组件的视图构建函数登记到工具内核。
//
// 应用装配时调用一次；工具内核按类型查表，不反向 import 任何业务扩展。
func RegisterAll() {
	core.RegisterNodeView(productPkg.Type, productViewOf)
	core.RegisterNodeView(productcardPkg.Type, productCardViewOf)
	core.RegisterNodeView(cardstackPkg.Type, cardstackViewOf)
	core.RegisterNodeView(productselectorPkg.Type, productSelectorViewOf)
	core.RegisterNodeView(carticonPkg.Type, cartIconViewOf)
	core.RegisterNodeView(searchresultsPkg.Type, searchResultsViewOf)
	core.RegisterNodeView(orderlistPkg.Type, orderListViewOf)
	core.RegisterNodeView(userformsPkg.Type, userFormsViewOf)
	core.RegisterNodeView(addtocartPkg.Type, addToCartViewOf)
	core.RegisterNodeView(articlelistPkg.Type, articleListViewOf)
	core.RegisterNodeView(productlistPkg.Type, productListViewOf)
	core.RegisterNodeView(checkoutformPkg.Type, checkoutFormViewOf)
	core.RegisterNodeView(navPkg.Type, navViewOf)
}
