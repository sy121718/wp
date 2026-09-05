// storage_check.go 认证会话后端存储的可用性校验（H3 fail-fast）。
//
// 背景：pkg/CLAUDE.md 已声明 cache/lock 组件废弃，但认证域的会话（session.go）、
// 封禁标记、在线心跳仍全部走 pkg/cache（Redis）。config/register.go 中 cache 组件
// 由 redis.enabled 开关控制，若开关关闭后仍静默放行认证初始化，会出现
// 「启动正常、/readyz ready、登录后全站 503」的静默故障。
//
// 约束：本文件只新增校验入口，不改动 session.go 的既有逻辑。
package auth

import (
	"errors"

	"go_wp/pkg/cache"
)

// RequireSessionStorage 校验认证会话的 Redis 存储是否就绪，未就绪时返回错误，
// 由组件编排层（config）在 auth.Init 成功后调用，使启动阶段直接失败（fail-fast）。
//
// 之所以由编排层显式调用而非在 auth.Init 内无条件检查：
//  1. InitComponents 先初始化全部 Critical 组件（含 auth）、后初始化非 Critical 组件（含 cache），
//     auth.Init 执行时 cache 尚未到达就绪态，必须先调整 cache 的注册顺序/关键性；
//  2. 公共测试（public/test/pkg/auth）直接调用 auth.Init 且不装配 cache，
//     无条件检查会破坏其独立生命周期测试，编排层注入可让两者并存。
func RequireSessionStorage() error {
	if !cache.IsInited() {
		return errors.New("认证会话存储不可用：请在配置中启用 redis（redis.enabled=true）")
	}
	return nil
}
