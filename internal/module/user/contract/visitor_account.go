package usercontract

// visitor_account.go — 访问面片段读取「当前访客自己的」账号事实的收窄端口。
//
// 为什么是两条方法，而不是把 UserService 整个交给片段层：
// 片段要做的事只有一件 —— 把**本人的**资料 / 偏好 / 登录设备渲染出来。
// 而 UserService 上还有注册、激活、改密码、踢出设备这些**写**能力。
// 依赖面越宽，越容易在某次顺手改动里被用出越权（「反正接口在手边」）。
//
// 与 SitePageResolver / VariantSnapshotPort 同一条思路：越权防护靠接口形状，
// 不靠调用方的自觉。这里连「不传 userID」这个选项都不给 —— userID 必须由
// 调用方从会话推出来，片段层没有任何办法读到别人的账号。

import (
	"context"

	userdto "go_wp/internal/module/user/dto"
)

// VisitorTokenContextKey 访客会话令牌在 gin context 里的键。
//
// 片段层需要令牌本身，只为一件事：判定登录设备列表里「哪一台是我现在用的」。
// 台账（user_sessions）里存的是令牌的 sha256，要比对就得有原值。
//
// **这个值只允许作为 VisitorAccountPort.SessionsOf 的入参**，
// 任何片段处理器都不得把它渲染进输出 —— 令牌能换一个已登录会话。
const VisitorTokenContextKey = "gowp_visitor_session_token"

// VisitorAccountPort 供访问面片段读取当前访客账号事实（只读）。
//
// 实现方是 user 模块的 service，装配期注入 runtimefragment；
// 未注入时（纯片段单测路径）片段渲染「账号功能暂不可用」而不是 500。
type VisitorAccountPort interface {
	// AccountOf 读账号概览与资料 / 偏好。userID 必须来自会话。
	AccountOf(ctx context.Context, userID uint64) (res *userdto.AccountResp, err error)
	// SessionsOf 读登录设备台账。
	//
	// currentToken 用于标记「当前设备」（界面上不给当前设备「踢出」按钮）。
	// 传空串时实现方按「都不标记」处理 —— 拿不到令牌不该让整个片段失败。
	SessionsOf(ctx context.Context, userID uint64, currentToken string) (items []*userdto.SessionItem, err error)
}
