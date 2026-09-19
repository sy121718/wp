package workbenchhttp

// inspector_navigation.go — 检查器的「具体菜单项」下拉数据源（nav 组件 Props.Navigation）。
//
// nav 组件有两种引用方式：按位置（Props.Menu，静态选项）与按**具体菜单项**
//（Props.Navigation，需要列出本工程的菜单项）。后者是动态数据，走 entityref 机制：
// 组件声明 ct:"entityref,navigation"，检查器按 refKind 找这里要选项。
//
// 未注入（测试装配 / 未接导航模块）时只返回「（不限）」，控件退化为空下拉而不是报错 ——
// 与 entityref 其它来源（category/brand/tag）同一降级口径。

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// NavigationPickerPort 导航菜单项能力（消费者侧最窄接口：列 + 建）。
//
// Create 供「检查器内就地新建菜单项」用：检查器此前只能从下拉里选已有项，想加一条就得
// 离开编辑器去 /admin/navigations 建完再回来，上下文全丢。写回走 navigation 模块的契约
// （不是直连它的 model），端口最小化仍按「检查器需要什么」判 —— 改 / 删 / 树都不进来。
type NavigationPickerPort interface {
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)
}

// SetNavigationPicker 注入导航菜单项列表端口（装配期调用；未注入时下拉为空）。
func (h *Handle) SetNavigationPicker(p NavigationPickerPort) {
	if h == nil {
		return
	}
	h.navigations = p
}

// navigationInspectorOptions 列出本工程全部菜单项（按位置分组排序）。
//
// 标签带位置前缀：同一个工程里「产品」这类标题在页眉与移动端各有一条，
// 只显示标题会让检查器里出现两个一模一样的选项，选错就静默绑到另一端的菜单上。
func navigationInspectorOptions(ctx context.Context, h *Handle, projectID, selected string) []inspectorOption {
	out := []inspectorOption{{Value: "", Label: "（不限）", Selected: selected == ""}}
	if h == nil || h.navigations == nil || projectID == "" {
		return out
	}
	rows, err := h.navigations.List(ctx, &navigationdto.ListReq{ProjectID: projectID})
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
		out = append(out, inspectorOption{
			Value:    row.ID,
			Label:    navigationKindLabel(row.Kind) + " · " + row.Title,
			Selected: row.ID == selected,
		})
	}
	return out
}

// InspectorNavigationCreate POST /workbench/navigation/create
//
// 检查器里就地新建菜单项（nav 组件的「具体菜单项」字段）：返回新项的 id 与展示标签，
// 客户端把它插入下拉并写回当前节点的 props —— 与手工选择走同一条提交流程。
//
// 边界：
//
//	· 写回走 navigation 契约的 Create，不直连它的 model；
//	· 权限点沿用 /api/navigation/create（路由上挂 CasbinMiddlewareForPath）；
//	· 只建**顶级**项（parentId 留空）：检查器没有层级上下文，随手挂到某一支下面
//	  比建错位置更难发现；要做子项去菜单页。
//	· 错误文案经 navigation 契约的 FacingText（命中白名单 → 可行动文案；未命中 → 归口
//	  文案 + 日志），不直出 err.Error()。
func (h *Handle) InspectorNavigationCreate(c *gin.Context) {
	if h == nil || h.navigations == nil {
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.MsgInternalError)
		return
	}
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	title := strings.TrimSpace(c.PostForm("title"))
	path := strings.TrimSpace(c.PostForm("path"))
	kind := strings.TrimSpace(c.PostForm("kind"))
	if projectID == "" || title == "" || path == "" || kind == "" {
		response.ParamError(c)
		return
	}
	created, err := h.navigations.Create(c.Request.Context(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: path, Kind: kind,
	})
	if err != nil || created == nil {
		logger.Scene("workbench").With("user_id", shell.CurrentUserID(c)).
			With("project_id", projectID).Error(err, "检查器新建菜单项失败")
		response.ErrorWithMessage(c, http.StatusBadRequest, h.navigationFacingText(c, err))
		return
	}
	response.Success(c, gin.H{
		"id": created.ID, "title": created.Title, "path": created.Path,
		"kind": created.Kind, "label": navigationKindLabel(created.Kind) + " · " + created.Title,
	})
}

// navigationFacingText 消费者侧的导航错误文案出口。
//
// 优先经契约的 FacingTexter（与导航模块自己的页面/接口出口同源：同一份白名单、同一份
// 取词）；未实现时落归口文案 —— **绝不**直出 err.Error()（那会把 PostgreSQL 原文
// 漏到检查器面板上）。
func (h *Handle) navigationFacingText(c *gin.Context, err error) string {
	if t, ok := h.navigations.(navigationcontract.FacingTexter); ok {
		return t.FacingText(response.RequestLanguage(c), err)
	}
	return workbenchenums.MsgInternalError
}

// navigationKindOptions 新建菜单项时的位置选项（与导航管理页的四个位置一致）。
func navigationKindOptions() []inspectorOption {
	return []inspectorOption{
		{Value: "header", Label: navigationKindLabel("header"), Selected: true},
		{Value: "header_mobile", Label: navigationKindLabel("header_mobile")},
		{Value: "footer", Label: navigationKindLabel("footer")},
		{Value: "footer_mobile", Label: navigationKindLabel("footer_mobile")},
	}
}

// navigationKindLabel 位置的中文名（与 admin 导航页的选项文案一致）。
func navigationKindLabel(kind string) string {
	switch kind {
	case "header":
		return "页眉"
	case "header_mobile":
		return "页眉（移动端）"
	case "footer":
		return "页脚"
	case "footer_mobile":
		return "页脚（移动端）"
	}
	return kind
}
