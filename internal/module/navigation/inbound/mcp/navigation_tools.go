package navigationmcp

// navigation_tools.go — 公开站点导航（页眉 / 页脚菜单）的读树与增改删。
//
// 菜单是**嵌套结构**：parentId 指向父项，服务端限制最大深度 64 层。
// 所以每一步都要先知道「现在这棵树长什么样」—— navigation_tree 不是可选的便利工具，
// 而是写路径的前置步骤（挂错父项会让一整块菜单跑到别人下面去）。
//
// kind 的白名单与 service 里的常量保持一致：header / header_mobile / footer / footer_mobile。
// 页眉与页脚是两棵独立的树，kind 传错会在另一个位置凭空长出一项。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/internal/permission"
)

// dirReader 导航的读能力。service 的方法集里 Get/List/Tree 都有，
// 这里只收窄到工具真正要用的四个。
type dirReader interface {
	List(ctx context.Context, req *navigationdto.ListReq) ([]*navigationdto.NavigationResp, error)
	Tree(ctx context.Context, projectID, kind string) ([]*navigationdto.NavigationNode, error)
}

type dirWriter interface {
	Create(ctx context.Context, req *navigationdto.CreateReq) (*navigationdto.NavigationResp, error)
	Update(ctx context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error)
	Delete(ctx context.Context, req *navigationdto.DeleteReq) error
}

// Tools 返回导航工具集（1 读树 + 3 写）。
func Tools(r dirReader, w dirWriter) ([]mcp.Tool, error) {
	if r == nil || w == nil {
		return nil, errors.New("navigationmcp: 导航读写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		navigationTree(r), navigationCreate(w), navigationUpdate(w), navigationDelete(w),
	}, nil
}

const (
	navKindHeader       = "header"
	navKindHeaderMobile = "header_mobile"
	navKindFooter       = "footer"
	navKindFooterMobile = "footer_mobile"
)

// navKind 校验位置。**不做兜底**：service 会把不认识的 kind 默认成 header，
// 而「我以为加在页脚，结果长在页眉上」是静默的 —— 多出来的那一项看起来很正常。
func navKind(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case navKindHeader, navKindHeaderMobile, navKindFooter, navKindFooterMobile:
		return strings.TrimSpace(raw), nil
	}
	return "", fmt.Errorf("kind 必须是 header / header_mobile / footer / footer_mobile 之一，实得 %q", raw)
}

func navKindText(kind string) string {
	switch kind {
	case navKindHeader:
		return "页眉"
	case navKindHeaderMobile:
		return "页眉（移动端）"
	case navKindFooter:
		return "页脚"
	case navKindFooterMobile:
		return "页脚（移动端）"
	}
	return kind
}

type navigationTreeArgs struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
}

func navigationTree(r dirReader) mcp.Tool {
	return mcp.New("navigation_tree", "读取站点导航树",
		"读一个位置的菜单树（含每项的 id、标题、链接、层级）。\n"+
			"**加/改/删菜单项之前必须先调它** —— parentId 与要改的 id 只能从这里拿，"+
			"而挂错父项会让一整块菜单跑到别人下面去。\n"+
			"kind 是位置：header 页眉 / header_mobile 页眉移动端 / footer 页脚 / footer_mobile 页脚移动端；"+
			"页眉与页脚是两棵独立的树。",
		permission.NavigationList,
		mcp.Object("读导航树参数", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id（用 site_projects 拿）"),
			"kind":      mcp.Enum("菜单位置", navKindHeader, navKindHeaderMobile, navKindFooter, navKindFooterMobile),
		}, "projectId", "kind"),
		func(ctx context.Context, args navigationTreeArgs) (mcp.Result, error) {
			kind, kerr := navKind(args.Kind)
			if kerr != nil {
				return mcp.Result{}, kerr
			}
			nodes, err := r.Tree(ctx, strings.TrimSpace(args.ProjectID), kind)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: navigationTreeText(nodes, kind)}, nil
		})
}

// navigationTreeText 用缩进表示层级 —— 位置关系是这棵树唯一重要的信息，
// 平铺成一列 id 会让人（和模型）看不出谁在谁下面。
func navigationTreeText(nodes []*navigationdto.NavigationNode, kind string) string {
	if len(nodes) == 0 {
		return fmt.Sprintf("%s还没有菜单项。用 navigation_create 加一项（不传 parentId 就是顶层项）。", navKindText(kind))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s菜单共 %d 项（缩进表示层级）：\n", navKindText(kind), countNavNodes(nodes))
	var walk func(items []*navigationdto.NavigationNode, depth int)
	walk = func(items []*navigationdto.NavigationNode, depth int) {
		for _, it := range items {
			if it == nil {
				continue
			}
			indent := strings.Repeat("  ", depth)
			extra := ""
			if strings.TrimSpace(it.Path) != "" {
				extra = " → " + it.Path
			}
			fmt.Fprintf(&b, "- %sid=%s「%s」%s\n", indent, it.ID, it.Title, extra)
			if len(it.Children) > 0 {
				walk(it.Children, depth+1)
			}
		}
	}
	walk(nodes, 0)
	b.WriteString("加子菜单就把父项的 id 传给 navigation_create 的 parentId。")
	return b.String()
}

func countNavNodes(items []*navigationdto.NavigationNode) int {
	n := 0
	for _, it := range items {
		if it == nil {
			continue
		}
		n++
		n += countNavNodes(it.Children)
	}
	return n
}

type navigationCreateArgs struct {
	ProjectID  string `json:"projectId"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	ParentID   string `json:"parentId"`
	SortOrder  int    `json:"sortOrder"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	Target     string `json:"target"`
	PanelWidth string `json:"panelWidth"`
	PanelBlock string `json:"panelBlockId"`
}

func navigationCreate(w dirWriter) mcp.Tool {
	return mcp.NewWrite("navigation_create", "新增站点导航项",
		"在一个菜单位置下加一项。不传 parentId 就是顶层项。\n"+
			"**加之前先调 navigation_tree**：parentId 只能从树里拿，"+
			"而页眉与页脚是两棵独立的树（kind 传错会在另一个位置凭空长出一项，看起来很正常）。\n"+
			"sourceType 决定这一项指向什么：`custom`（自己填路径）/ `page` / `article` / "+
			"`product` / `category` / `block`。选后几种时必须同时给 sourceId（用对应的\n"+
			"查询工具拿），否则会被拒。\n"+
			"path 是链接地址（custom 时必填）；target 是打开方式：self 当前窗口 / blank 新窗口。",
		permission.NavigationCreate,
		mcp.Object("新增导航项参数", map[string]mcp.Schema{
			"projectId":    mcp.String("工程 id（用 site_projects 拿）"),
			"title":        mcp.String("菜单文字（与用户看到的一致）"),
			"path":         mcp.String("链接地址（sourceType=custom 时必填，如 /about）"),
			"kind":         mcp.Enum("菜单位置", navKindHeader, navKindHeaderMobile, navKindFooter, navKindFooterMobile),
			"parentId":     mcp.String("父导航项 id（可选；不传 = 顶层项。用 navigation_tree 拿）"),
			"sortOrder":    mcp.Integer("排序权重（可选，越小越靠前）"),
			"sourceType":   mcp.Enum("来源类型（可选，默认 custom）", "custom", "page", "article", "product", "category", "block"),
			"sourceId":     mcp.String("来源实体 id（sourceType 不是 custom 时必填）"),
			"target":       mcp.Enum("打开方式（可选，默认 self）", "self", "blank"),
			"panelBlockId": mcp.String("悬浮面板引用的全局块 id（可选；超级菜单才用，一般不填）"),
			"panelWidth":   mcp.Enum("面板宽度（可选；只有配了 panelBlockId 才有意义）", "auto", "full"),
		}, "projectId", "title", "path", "kind"),
		nil,
		func(ctx context.Context, args navigationCreateArgs) (mcp.Result, error) {
			kind, kerr := navKind(args.Kind)
			if kerr != nil {
				return mcp.Result{}, kerr
			}
			req := &navigationdto.CreateReq{
				ProjectID:  strings.TrimSpace(args.ProjectID),
				Title:      strings.TrimSpace(args.Title),
				Path:       strings.TrimSpace(args.Path),
				Kind:       kind,
				SortOrder:  args.SortOrder,
				SourceType: strings.TrimSpace(args.SourceType),
				Target:     strings.TrimSpace(args.Target),
				PanelWidth: strings.TrimSpace(args.PanelWidth),
			}
			if p := strings.TrimSpace(args.ParentID); p != "" {
				req.ParentID = &p
			}
			if s := strings.TrimSpace(args.SourceID); s != "" {
				req.SourceID = &s
			}
			if b := strings.TrimSpace(args.PanelBlock); b != "" {
				req.PanelBlockID = &b
			}
			res, err := w.Create(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: navigationCreatedText(res, args)}, nil
		})
}

func navigationCreatedText(res *navigationdto.NavigationResp, args navigationCreateArgs) string {
	if res == nil {
		return "导航项已新增。"
	}
	where := navKindText(res.Kind)
	if res.ParentID != nil && strings.TrimSpace(*res.ParentID) != "" {
		where += "，挂在 " + strings.TrimSpace(*res.ParentID) + " 下面"
	} else {
		where += "，是顶层项"
	}
	return fmt.Sprintf("导航项「%s」已新增（id=%s，%s，链接 %s）。\n"+
		"它要等前台菜单缓存刷新后才会显示出来；如果立刻看不到，先去后台菜单页点一下刷新。",
		res.Title, res.ID, where, emptyAsDash(res.Path))
}

type navigationUpdateArgs struct {
	ID         string  `json:"id"`
	Title      *string `json:"title"`
	Path       *string `json:"path"`
	Kind       *string `json:"kind"`
	ParentID   *string `json:"parentId"`
	SortOrder  *int64  `json:"sortOrder"`
	SourceType *string `json:"sourceType"`
	SourceID   *string `json:"sourceId"`
	Target     *string `json:"target"`
	PanelBlock *string `json:"panelBlockId"`
	PanelWidth *string `json:"panelWidth"`
}

func navigationUpdate(w dirWriter) mcp.Tool {
	return mcp.NewWrite("navigation_update", "修改站点导航项",
		"改菜单项的文字、链接、层级或排序。**只改你传的字段**。\n"+
			"**parentId 是三态**：不传 = 层级不动；传空串 = **提升为顶层项**；传 id = 挂到那一项下面。\n"+
			"**panelBlockId 同样三态**：不传 = 不动；传空串 = **清除悬浮面板**（已有的面板要能撤销）。\n"+
			"移动菜单项时注意别把父项挂到自己的子孙下面（会形成环，服务端会拒）。",
		permission.NavigationUpdate,
		mcp.Object("修改导航项参数", map[string]mcp.Schema{
			"id":           mcp.String("导航项 id（用 navigation_tree 拿）"),
			"title":        mcp.String("新菜单文字（可选）"),
			"path":         mcp.String("新链接地址（可选）"),
			"kind":         mcp.String("新位置（可选；**换了位置等于搬家**，页眉与页脚是两棵树）"),
			"parentId":     mcp.String("父导航项 id（**三态**：不传=不改层级；传空串=\"\"=提升为顶层；传 id=挂到该项下）"),
			"sortOrder":    mcp.Integer("新排序权重（可选，越小越靠前）"),
			"sourceType":   mcp.String("新来源类型（可选：custom/page/article/product/category/block）"),
			"sourceId":     mcp.String("新来源实体 id（可选）"),
			"target":       mcp.String("新打开方式（可选：self/blank）"),
			"panelBlockId": mcp.String("悬浮面板块 id（**三态**：不传=不动；传空串=\"\"=清除面板）"),
			"panelWidth":   mcp.String("面板宽度（可选：auto/full）"),
		}, "id"),
		nil,
		func(ctx context.Context, args navigationUpdateArgs) (mcp.Result, error) {
			req := &navigationdto.UpdateReq{
				ID:           strings.TrimSpace(args.ID),
				Title:        args.Title,
				Path:         args.Path,
				ParentID:     args.ParentID,
				SortOrder:    nil,
				SourceType:   args.SourceType,
				SourceID:     args.SourceID,
				Target:       args.Target,
				PanelBlockID: args.PanelBlock,
				PanelWidth:   args.PanelWidth,
			}
			if args.SortOrder != nil {
				s := int(*args.SortOrder)
				req.SortOrder = &s
			}
			if args.Kind != nil {
				kind, kerr := navKind(*args.Kind)
				if kerr != nil {
					return mcp.Result{}, kerr
				}
				req.Kind = &kind
			}
			if req.Title == nil && req.Path == nil && req.Kind == nil && req.ParentID == nil &&
				req.SortOrder == nil && req.SourceType == nil && req.SourceID == nil &&
				req.Target == nil && req.PanelBlockID == nil && req.PanelWidth == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.Update(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "导航项已修改。"}, nil
			}
			level := navKindText(res.Kind) + "顶层项"
			if res.ParentID != nil && strings.TrimSpace(*res.ParentID) != "" {
				level = navKindText(res.Kind) + "，挂在 " + strings.TrimSpace(*res.ParentID) + " 下面"
			}
			return mcp.Result{Text: fmt.Sprintf(
				"导航项 %s 已更新，现在是「%s」（%s，链接 %s）。",
				res.ID, res.Title, level, emptyAsDash(res.Path))}, nil
		})
}

type navigationDeleteArgs struct {
	ID string `json:"id"`
}

func navigationDelete(w dirWriter) mcp.Tool {
	return mcp.NewWrite("navigation_delete", "删除站点导航项",
		"删除一个菜单项。**它下面的子菜单会一起消失**（整棵子树）。\n"+
			"删之前先调 navigation_tree 确认这一项底下挂了什么 —— 层级是缩进显示的，"+
			"看错一行就会连带删掉一整块菜单。\n"+
			"只是不想让它显示时，先跟用户确认是不是要连子菜单一起删。",
		permission.NavigationDelete,
		mcp.Object("删除导航项参数", map[string]mcp.Schema{
			"id": mcp.String("导航项 id（用 navigation_tree 拿）"),
		}, "id"),
		nil,
		func(ctx context.Context, args navigationDeleteArgs) (mcp.Result, error) {
			if err := w.Delete(ctx, &navigationdto.DeleteReq{ID: strings.TrimSpace(args.ID)}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"导航项 %s 已删除，它下面的子菜单也一并消失了。"+
					"前台要等菜单缓存刷新后才看不到它。", args.ID)}, nil
		})
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
