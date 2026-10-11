// Package adminctx 装配后台页面请求的权限上下文：把当前用户的有效权限码集合、按钮码集合
// 与授权导航树写进 gin 上下文，供 adminui.Prepare（侧栏 / 按钮显隐）与模块 handler 读取。
//
// 为什么独立成包（不并进 internal/middleware/builtin）：权限上下文的契约
// （admincontract.AuthzContextService）是**后台页面组专属**的知识，而 builtin 装的是全局链
// 上的中间件（Recovery / CORS / 限流 / CSRF …）—— 它不必认识 admin 模块的契约。页面组的
// 装配点只有 internal/routers/assembly.go 的两条 group 链。
//
// 为什么这是**有意的行为变更**（fail-closed 修复，不是逐行等价搬迁）：
// 迁移前的实现在 EffectivePermissionCodes 返回「非空 codes + err」时两者并用 —— 一边记
// 「权限上下文查询失败，本页按空权限集渲染」，一边照 codes 填权限集、并把它传给按钮查询
// 与建树。日志宣称空权限、行为却是照授权，是一条真实的 fail-open 通道。本包把行为对齐注释：
// 查询失败即丢弃 codes、跳过按钮查询与建树（与 API 侧 Casbin 的 fail-closed 口径一致 ——
// 权限信息拿不到时按无权限处理，绝不因为「查不到」而放行）。
//
// 三个请求上下文键归写入方定义，adminui.Prepare 与模块 handler 共用本包的导出常量；
// 写入到页面装配的真实链路由 prepare_e2e_test.go 验证。
package adminctx

import (
	admincontract "go_wp/internal/module/admin/contract"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// 请求上下文键由写入方统一声明，供 Prepare 与其他读取方直接引用。
const (
	// PermSetKey 权限码集合（map[string]bool）。
	PermSetKey = "perm_set"
	// NavTreeKey 授权导航树（[]admindto.MenuTreeNode）。
	NavTreeKey = "nav_tree"
	// ButtonsKey 当前用户有权触发的按钮码集合（map[string]bool）。
	ButtonsKey = "button_set"
)

// PermContextMiddleware 把当前用户的有效权限码集合、按钮码集合与授权导航树写入请求上下文。
//
// 仅后台页面路由挂载。API 鉴权由 Casbin 中间件独立负责，两者共用同一权限来源
// （EffectivePermissionCodes），保证「API 放行的权限」与「页面/按钮显示的权限」同源。
//
// 失败一律降级（**fail-closed**）：权限码查询失败按空权限集渲染（并跳过按钮查询与建树），
// 按钮码查询失败则按钮全隐，菜单树构建失败则侧栏为空 —— 都不影响页面内容与鉴权判定，
// 但必须留痕才能定位。任何失败路径都不写「满权限」，末尾两个键恒写入（失败时是空 map）。
func PermContextMiddleware(authz admincontract.AuthzContextService) gin.HandlerFunc {
	return func(c *gin.Context) {
		set := map[string]bool{}
		buttons := map[string]bool{}
		if authz != nil {
			if v, ok := c.Get("user_id"); ok {
				if uid, ok := v.(int64); ok && uid > 0 {
					codes, aerr := authz.EffectivePermissionCodes(c.Request.Context(), uint64(uid))
					if aerr != nil {
						logger.Scene("dashboard").With("userId", uid).
							Error(aerr, "权限上下文查询失败，本页按空权限集渲染")
						// 有意丢弃：err 与非空 codes 并存时绝不照授权（旧实现在这里照用 codes）
						// —— 下游的按钮查询与建树一并跳过，见包注释「有意行为变更」。
						codes = nil
					} else {
						for _, code := range codes {
							set[code] = true
						}
						// 按钮码：由 type=3 节点按同一批有效权限码算出（同源，见契约注释）。
						if list, berr := authz.AuthorizedButtonCodes(c.Request.Context(), codes); berr != nil {
							logger.Scene("dashboard").With("userId", uid).
								Error(berr, "按钮码查询失败，本页按钮按无权限渲染")
						} else {
							for _, b := range list {
								buttons[b] = true
							}
						}
						// 侧栏导航树与上面那批权限码同源：菜单真源是 sys_menus 表，
						// admin 模块按权限码过滤后返回（自动补祖先目录、排除按钮与外链）。
						tree, terr := authz.BuildAuthorizedTree(c.Request.Context(), codes)
						if terr != nil {
							logger.Scene("dashboard").With("userId", uid).
								Error(terr, "导航菜单树构建失败，本页侧栏为空")
						} else {
							c.Set(NavTreeKey, tree)
						}
					}
				}
			}
		}
		c.Set(PermSetKey, set)
		c.Set(ButtonsKey, buttons)
		c.Next()
	}
}
