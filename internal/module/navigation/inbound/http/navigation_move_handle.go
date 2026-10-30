package navigationhttp

// navigation_move_handle.go — 同级上移 / 下移的写入口（POST /api/navigation/update，action="move"）。
//
// 契约（迁移前逐字保留）：
//   - 顶层只认 action / id / projectId / dir（严格白名单，多一个键就 400）；
//   - id 与 dir 必填（非空字符串）；dir 的取值为 up / down，**枚举判据在 service**
//     （SwapMove 一份），handler 只判「是字符串且非空」，避免两份枚举出现静默分叉；
//   - projectId 可选（缺省 = 不校验工程归属）；显式 null / 非字符串仍拒；
//   - 版本冲突 → ErrStaleVersion（409，客户端刷新后重做）；
//   - move **不做客户端版本承诺**：请求里没有 update_time / expected_updated_at，
//     SwapMove 自己读树快照、按快照里的版本写，冲突时整事务回滚且零派发。

import (
	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
)

// navigationMoveKeys move 档顶层允许出现的键（**严格白名单**）。
//
// kind 刻意不在表内：SwapMove 只认 id / projectId / dir，行方向由 service 从导航项自身读
// （kind 在页面里是**筛选上下文**，不是这次写请求的作用域字段）。多发一个字段只会让严格
// 白名单把整条请求拒掉 —— 与 addSource 档同一个口径。
var navigationMoveKeys = map[string]bool{
	"action":    true,
	"id":        true,
	"projectId": true,
	"dir":       true,
}

// moveNavigationEnvelope 把已解析的 move 分支**结构映射**成 moveNavigation 的顶层键值。
//
// dto 的 MoveRequest() 已经判掉：id / dir 必填非空、projectId 显式 null 与非字符串一律
// 形状错（Route 阶段已拒）。这里只把已解析的字段拼回同形键值，白名单与调用仍在那份
// helper 里，不重复业务判据。
func (h *Handle) moveNavigationEnvelope(c *gin.Context, env *navigationdto.UpdateEnvelope) {
	if err := env.ValidateMove(); err != nil {
		response.ParamError(c)
		return
	}
	req, err := env.MoveRequest()
	if err != nil {
		response.ParamError(c)
		return
	}
	raw := map[string]json.RawMessage{}
	for key, value := range map[string]string{"id": req.ID, "dir": req.Dir} {
		encoded, merr := json.Marshal(value)
		if merr != nil {
			response.ParamError(c)
			return
		}
		raw[key] = encoded
	}
	// projectId 是可选键：缺省 = 不校验工程归属；dto 已把显式 null 与空串收敛成空串
	//（不校验），非空才写这一格 —— 空串与缺省在本分支语义相同。
	if req.ProjectID != "" {
		encoded, merr := json.Marshal(req.ProjectID)
		if merr != nil {
			response.ParamError(c)
			return
		}
		raw["projectId"] = encoded
	}
	h.moveNavigation(c, raw)
}

// moveNavigation 解析 move 档并调用 service 的原子交换（一次调用 = 一个事务）。
func (h *Handle) moveNavigation(c *gin.Context, raw map[string]json.RawMessage) {
	if !navigationDeleteShapeOK(raw, navigationMoveKeys) {
		response.ParamError(c)
		return
	}
	id, ok := navigationWriteRequiredString(raw, "id")
	if !ok {
		response.ParamError(c)
		return
	}
	// dir 这里只校验「是字符串且非空」：up / down 的枚举判据在 service（SwapMove 一份），
	// 在 handler 再存一份就会出现「service 认的档比 handler 多」的静默分叉。
	dir, ok := navigationWriteRequiredString(raw, "dir")
	if !ok {
		response.ParamError(c)
		return
	}
	// projectId 可选（缺省 = 不校验工程归属）：显式 null / 非字符串仍拒，
	// 与 addSource 档的 kind 同一条判据（「这一格没填」与「填了个不是类型值的东西」是两件事）。
	projectID, ok := navigationAddSourceOptionalString(raw, "projectId")
	if !ok {
		response.ParamError(c)
		return
	}
	res, err := h.svc.SwapMove(c.Request.Context(), &navigationdto.SwapMoveReq{
		ID:        id,
		ProjectID: projectID,
		Dir:       dir,
	})
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgUpdateSuccess, res)
}
