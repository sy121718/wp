package navigationhttp

// navigation_panel_page.go — 菜单项的悬浮面板（超级菜单，迁移 285）管理入口。
//
// 面板内容存**全局块**（一处改、多处复用），展示形态存菜单项（同一个块可被多项复用）。
// 两个写动作刻意分开，不在一次请求里做两处持久化：
//   · PanelSet：把已有块挂到菜单项 / 清除面板（单条 navigation 更新）；
//   · PanelCreate：只建块并跳块编辑器（单条 block 写入）。
// 「新建块并自动挂上」需要跨模块两次写（block 建 + navigation 改），按硬规则必须同事务
// 透传，而两个模块当前都没有可用的 Tx 变体 —— 本批不提供这个组合动作：先建块（跳编辑器
// 设计面板），保存后回列表在面板下拉里选它。两步各自都是单写，不需要跨模块事务。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	blockcontract "go_wp/internal/module/block/contract"
	navigationdto "go_wp/internal/module/navigation/dto"

	"go_wp/internal/web/shell"
)

// BlockPanelPort 面板所需的块能力（消费者侧最窄接口：只列与建）。
type BlockPanelPort interface {
	List(ctx ctxType, req *blockcontract.ListReq) ([]blockcontract.BlockResp, error)
	Create(ctx ctxType, req *blockcontract.CreateReq) (*blockcontract.BlockResp, error)
}

// SetBlockPanelPort 注入块能力（装配期调用；未注入时面板入口整体降级为不可用）。
func (h *navigationPageHandle) SetBlockPanelPort(p BlockPanelPort) {
	if h == nil {
		return
	}
	h.blocks = p
}

// PanelSet POST /admin/navigations/panel：把块挂到菜单项（或清除面板）。
func (h *navigationPageHandle) PanelSet(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, navFieldRequiredMsg)
		return
	}
	// 空串 = 清除面板（服务层把空块 id 归一成 NULL；nil 才是「不改动」）。
	blockID := strings.TrimSpace(c.PostForm("panelBlockId"))
	req := &navigationdto.UpdateReq{ID: id, PanelBlockID: &blockID}
	if w := strings.TrimSpace(c.PostForm("panelWidth")); w != "" {
		req.PanelWidth = &w
	}
	if _, err := h.navigations.Update(c.Request.Context(), req); err != nil {
		logger.Scene("page").With("id", id).Error(err, "设置菜单悬浮面板失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	c.Redirect(http.StatusSeeOther, navListURL(projectID, kind))
}

// PanelCreate POST /admin/navigations/panel/create：新建面板块并跳块编辑器。
//
// 命名带位置与项名（如「页眉菜单·产品」）：面板块在块列表里与其它全局块混排，
// 不带来源信息的名字过几天就没人知道它是干什么的、能不能删。
func (h *navigationPageHandle) PanelCreate(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	kind := normalizeNavKind(c.PostForm("kind"))
	title := strings.TrimSpace(c.PostForm("title"))
	if h.blocks == nil {
		shell.PageErrorBadRequest(c, "navigation", errPanelUnavailable)
		return
	}
	name := panelBlockName(kind, title)
	created, err := h.blocks.Create(c.Request.Context(), &blockcontract.CreateReq{
		ProjectID: projectID, Name: name, Kind: "block",
	})
	if err != nil {
		logger.Scene("page").With("project_id", projectID).Error(err, "新建面板块失败")
		shell.PageErrorBadRequest(c, "navigation", err)
		return
	}
	// 跳块编辑器：把面板结构画出来。保存后回列表在面板下拉里选它挂上。
	c.Redirect(http.StatusSeeOther, "/workbench?block="+created.ID)
}

// panelBlockName 面板块的默认名（可辨认来源）。
func panelBlockName(kind, title string) string {
	pos := "菜单"
	switch kind {
	case "header":
		pos = "页眉菜单"
	case "header_mobile":
		pos = "页眉移动菜单"
	case "footer":
		pos = "页脚菜单"
	case "footer_mobile":
		pos = "页脚移动菜单"
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = "面板"
	}
	return pos + "·" + t
}
