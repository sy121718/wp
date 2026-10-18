// navigation_move_handle.go — 同级上移 / 下移的**严格形状 union 分支**
// （POST /api/navigation/update 的 action="move"）。
//
// 为什么并进 update 而不是另开端点：权限锚点。sys_permission 每个 code 只有一列
// api_path / api_method，给「修改导航」再挂一条 /api/navigation/move 就会出现两套锚点 ——
// 角色授权只会授到其中一条，用户随即遇到「能改却不能挪」。页面这一侧原先的
// POST /navigations/move 也正是**借用** /api/navigation/update 的权限动作
// （builtin.CasbinMiddlewareForPath），并过来以后权限与 CSRF 链一字不变。
//
// 判别键是 action 标签（与 delete 的「按形状判别」不同：delete 两个分支各只有一个键，
// 形状本身就是判别键；move 与普通 update 共享 id 与若干可写字段，只能靠显式标签分清）。
// 白名单、null / 非字符串、未知键、以及「无 action 却带 move 专属键」的混形一律 400 ——
// 猜错分支会挪错位置，而顺序是持久化事实，没有撤销。
//
// 作用域与并发语义全部留在 service.SwapMove：
//   - projectId 只做 **locator 校验**（非空且与该导航项所属工程不一致 → ErrInvalidParam），
//     客户端作用域不在这里再判一次权限；
//   - 边界（首项上移 / 末项下移）**不是错误**：200 + {moved:false, reason:"boundary"}，
//     前端整页刷新后顺序不变；
//   - 同级 sort_order 并列 → ErrSortTied（业务拒绝，走 navigationErrorStatus 的 400 档）；
//   - 版本冲突 → ErrStaleVersion（409，客户端刷新后重做）；
//   - move **不做客户端版本承诺**：请求里没有 update_time / expected_updated_at，
//     SwapMove 自己读树快照、按快照里的版本写，冲突时整事务回滚且零派发。
package navigationhttp

import (
	json "go_wp/pkg/jsonx"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/response"
)

// navigationMoveAction action 键的 move 档取值。
const navigationMoveAction = "move"

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

// navigationWriteHasMoveKeys 旧分支的混形守卫：出现 move 专属键即拒。
//
// 与 navigationWriteHasTranslationKeys 同一理由：UpdateReq 里没有 projectId / dir，
// 带了它们却又不带 action，说明调用方按另一份契约在说话 —— 按旧语义默默忽略会让
// 「字段改生效了、作用域被丢掉」，而调用方还以为自己限定过一次工程。
func navigationWriteHasMoveKeys(raw map[string]json.RawMessage) bool {
	for _, key := range []string{"dir", "projectId"} {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
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
