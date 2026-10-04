package builtin

// casbin_enforce.go — 非 HTTP 上下文的权限判定。
//
// 中间件只能挂在路由上，而「这个人能不能看这份数据」还会在其它地方被问到：
// AI 工具执行、后台任务、导出。判据必须与页面按钮的可见性同源 ——
// 两处口径一旦分叉，就会出现「页面上看不见的东西，AI 却查得到」，
// 而这种问题在界面上完全看不出来。

import (
	"errors"
	"strconv"

	"go_wp/pkg/casbin"
)

// EnforceForUser 判定某个后台账号是否持有权限点 obj 上的 act 动作。
//
// sub 的口径与 casbinMiddleware 完全一致（用户 ID 的十进制字符串）：这不是巧合，
// 而是「同一套策略、同一种主体表示」的要求 —— 换成用户名会让已有策略全部失配，
// 且失配的表现是「所有人突然都没权限了」或「全部放行」这种面状故障。
//
// enforcer 未初始化时回错误而不是 false：调用方需要能把「权限系统没起来」（装配/启动缺陷，
// 该报警）与「这个人确实没权限」（正常业务结论）分开处理。两者合并的失败模式是
// 一次启动故障被当成所有人权限被收走。
func EnforceForUser(userID int64, obj, act string) (bool, error) {
	enforcer := casbin.GetEnforcer()
	if enforcer == nil {
		return false, errors.New("权限系统未初始化")
	}
	return enforcer.Enforce(strconv.FormatInt(userID, 10), obj, act)
}
