package navigationhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/response"
)

// Handle navigation 模块 HTTP 处理器。
type Handle struct {
	svc navigationcontract.NavigationService
}

// NewHandle 构造。
func NewHandle(svc navigationcontract.NavigationService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建导航项；同时是**来源批量加入菜单的唯一写入口**（严格 tagged union，见
// navigation_add_source_write.go）：缺 action 就是本来的创建请求，行为逐字不变
// （binding:"required"、指针字段、未知字段忽略都保持不变）。
func (h *Handle) Create(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}
	action, hasAction, shapeOK := navigationWriteAction(raw)
	if !shapeOK {
		response.ParamError(c)
		return
	}
	if hasAction {
		if action != navigationAddSourceAction {
			response.ParamError(c)
			return
		}
		h.createFromSource(c, raw)
		return
	}
	if navigationDeleteHasKey(raw, "ids") {
		// ids 是来源批量档的专属键：普通创建契约里没有它，出现即混形（见该文件头）。
		response.ParamError(c)
		return
	}
	req := &navigationdto.CreateReq{}
	if err := binding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgCreateSuccess, res)
}

// Update 更新导航项；同时是**译文整表提交**与**同级上移 / 下移**的唯一写入口
// （严格 tagged union，见 navigation_translation_write.go / navigation_move_write.go）：
// 缺 action 就是本来的更新请求，行为逐字不变。
func (h *Handle) Update(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}
	action, hasAction, shapeOK := navigationWriteAction(raw)
	if !shapeOK {
		response.ParamError(c)
		return
	}
	if hasAction {
		switch action {
		case navigationTranslationAction:
			h.updateTranslations(c, raw)
		case navigationMoveAction:
			h.moveNavigation(c, raw)
		default:
			// 未知标签不猜分支：它可能是任一条分支的残缺形状。
			response.ParamError(c)
		}
		return
	}
	if navigationWriteHasTranslationKeys(raw) {
		// 旧形状里混进译文分支的键：两条分支的语义完全不同，猜错会写错数据。
		response.ParamError(c)
		return
	}
	if navigationWriteHasMoveKeys(raw) {
		// 同理：move 分支的专属键出现却没有 action 标签，说明调用方按另一份契约在说话。
		response.ParamError(c)
		return
	}
	req := &navigationdto.UpdateReq{}
	// 走 gin 的 binding 而不是裸 json.Unmarshal：binding:"required" 的校验与指针字段的
	// nil / 空语义必须与迁移前一致（json.Unmarshal 会静默跳过 required）。
	if err := binding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgUpdateSuccess, res)
}

// Get 导航项详情。
func (h *Handle) Get(c *gin.Context) {
	req := &navigationdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDetailSuccess, res)
}

// List 导航项列表。
func (h *Handle) List(c *gin.Context) {
	req := &navigationdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgListSuccess, list)
}

// Delete 删除导航项。
func (h *Handle) Delete(c *gin.Context) {
	body, raw, ok := navigationWriteBody(c)
	if !ok {
		return
	}
	// 判别键是**形状**而不是 action 标签：单档只认 id（列表行内删除）、批档只认 ids
	// （勾选后批量删除）。见 navigation_delete_write.go 开头「为什么不拆成两条路径」。
	hasID := navigationDeleteHasKey(raw, "id")
	hasIDs := navigationDeleteHasKey(raw, "ids")
	if hasID && hasIDs {
		response.ParamError(c)
		return
	}
	if hasIDs {
		if !navigationDeleteShapeOK(raw, navigationDeleteBulkKeys) {
			response.ParamError(c)
			return
		}
		h.deleteBulk(c, raw)
		return
	}
	if !hasID || !navigationDeleteShapeOK(raw, navigationDeleteSingleKeys) {
		response.ParamError(c)
		return
	}
	req := &navigationdto.DeleteReq{}
	// 走 gin 的 binding 而不是裸 json.Unmarshal：binding:"required" 的校验必须与迁移前一致。
	if err := binding.JSON.BindBody(body, req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDeleteSuccess, nil)
}

// navigationErrorStatus 把业务错误映射为 HTTP 状态码。
func navigationErrorStatus(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, navigationenums.ErrNotFound):
		return http.StatusNotFound
	// 乐观锁冲突用 409：与「你填错了」（400）和「系统坏了」（500）都不同 ——
	// 这一次请求本身没问题，是数据在这期间被别人改过，客户端该刷新后重做。
	case strings.Contains(msg, navigationenums.ErrStaleVersion):
		return http.StatusConflict
	case strings.Contains(msg, navigationenums.ErrInvalidParam),
		strings.Contains(msg, navigationenums.ErrInvalidKind),
		strings.Contains(msg, navigationenums.ErrPathTaken):
		return http.StatusBadRequest
	// 同级排序并列同样是「你这次的请求有问题、但不是 500」：交换两行取值等于没换，
	// service 显式早退（零写）。落进 default 会把它报成系统故障，让用户以为重试能好。
	case strings.Contains(msg, navigationenums.ErrSortTied):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
