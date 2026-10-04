package aihttp

// ai_tools_wire.go — 把「工具注册表 + 权限判定」接成会话层要的 ToolProvider。
//
// 落在装配层（inbound/http）而不是 service：会话层不该认识 internal/mcp，也不该认识 Casbin，
// 而这两个恰好都是「接线」才知道的东西。换一套工具实现（例如将来由插件提供的工具）
// 只需在这里再包一层，会话层一行都不用动。

import (
	"context"
	"errors"

	"go_wp/internal/mcp"
	"go_wp/internal/middleware/builtin"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/logger"
)

// casbinAuthorizer 按**权限点**判权限：把权限点换算成声明它的路由，再走与页面中间件
// 同一套 Casbin 策略（obj=路径, act=HTTP 方法）。
//
// 为什么不能拿权限点当 obj 直接 Enforce：策略里存的是路由路径（`/api/order/list`），
// 权限点（`order:list`）只是**声明侧的标识**。拿权限点当 obj 会永远匹配不到任何策略，
// 于是所有人调用所有工具都被判越权 —— 一个「看起来像权限配错了」的全量故障。
//
// 一个权限点通常有若干条路由（一条 API + 后台页面路由）：**任一条通过即可**。
// 反过来（要求全部通过）会把「页面路由漏 seed」之类历史问题变成工具不可用。
func casbinAuthorizer(_ context.Context, userID int64, perm permission.Perm) (bool, error) {
	routes := permission.RoutesOf(perm)
	if len(routes) == 0 {
		// 权限点没有任何路由声明：这是装配缺陷。按无权限处理（fail closed），
		// 并把问题暴露在日志里 —— 静默放行会让「工具权限没接上」表现成「AI 什么都能查」。
		logger.Scene("ai").With("perm", string(perm)).Warn("权限点没有声明路由，工具调用按无权限处理")
		return false, nil
	}
	for _, r := range routes {
		ok, err := builtin.EnforceForUser(userID, r.Path, r.Method)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// toolProvider 把 *mcp.Runner 适配成 aiservice.ToolProvider。
type toolProvider struct {
	runner *mcp.Runner
}

// Specs 工具声明。
//
// **每次重新转换**，不在装配期缓存一份：各模块自己装配，AI 模块的装配通常排在
// 领域模块（订单、分析）之前，缓存会让先装配的那一刻看到「一个工具都没有」，
// 而此后永远不会刷新 —— 表现是「AI 明明配了工具却从不调用」。
// 工具数量是个位数，每次重转的成本可以忽略。
func (p *toolProvider) Specs() []aidto.ToolSpec {
	tools := p.runner.Tools()
	if len(tools) == 0 {
		return nil
	}
	out := make([]aidto.ToolSpec, 0, len(tools))
	for _, t := range tools {
		raw, err := t.SchemaJSON()
		if err != nil {
			// 跳过这一个，而不是整轮不带工具：一个工具的参数声明序列化失败，
			// 不该让其它工具一起失效。兜底成空 schema 更糟 —— 模型会一直用空参数调它，
			// 而每次都被参数校验拒掉，看起来像这个工具自己坏了。
			logger.Scene("ai").With("tool", t.Name()).Error(err, "工具 schema 序列化失败，本轮不暴露该工具")
			continue
		}
		out = append(out, aidto.ToolSpec{Name: t.Name(), Description: t.Description(), Parameters: raw})
	}
	return out
}

// Run 执行一次工具调用。
//
// 契约（见 aiservice.ToolProvider）：**业务性失败以文本返回**（err = nil），
// 让模型能转述给用户或据此改正；error 只用于「这轮对话不该继续」。
// 这里把所有执行错误都翻成文本，因为它们无一例外都属于「模型该知道、用户该被告知」那一类。
func (p *toolProvider) Run(ctx context.Context, userID int64, name, arguments string) (string, error) {
	res, err := p.runner.Run(ctx, userID, name, arguments)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// 上下文取消由服务层处理（它知道该不该上抛），这里不吞。
			return "", ctxErr
		}
		return failureText(name, err), nil
	}
	return res.Text, nil
}

// failureText 把执行错误翻成能回给模型的一句话。
//
// 分档的判据是「模型拿这句话能做什么」：
//
//	· 参数错误 → **原样回**（字段名与缺项模型自己能看懂，据此改正并重试）；
//	· 越权     → 明确的「没有权限」（用户需要知道这不是故障，而是账号权限问题）；
//	· 其它     → 统一的失败文案。原文可能带表名、连接串、内部路径，
//	             而它最终会经模型的嘴出现在页面上。
func failureText(tool string, err error) string {
	var argsErr *mcp.ArgsError
	if errors.As(err, &argsErr) {
		return argsErr.Msg
	}
	var permErr *mcp.PermissionError
	if errors.As(err, &permErr) {
		return facingKey(aienums.ErrToolForbidden)
	}
	// 未知工具名归到这里：模型拼错了名字，对它来说「这次没查成」就够了 ——
	// 不必告诉用户「工具 orders_sumary 不存在」（那是模型的问题，不是他的）。
	logger.Scene("ai").With("tool", tool).Error(err, "工具执行失败")
	return facingKey(aienums.ErrToolRunFailed)
}

// facingKey 查面向用户的文案并保证非空（未登记时回 key 本身，缺陷可见）。
func facingKey(key string) string {
	if text, ok := aienums.FacingText(key); ok {
		return text
	}
	return key
}
