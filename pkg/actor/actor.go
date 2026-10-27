// Package actor 把「这次请求是谁发起的」放进 context.Context。
//
// 为什么需要它：发布历史的每一行都要回答「谁点的」，但 presentation 模块的 service
// 从头到尾不认识用户 —— 身份只存在于 gin.Context（会话中间件 c.Set）与入参里。要让
// 8 个发布调用点（创建 / 重建 / 换模板 / 改 URL / 回滚 / 覆盖文档 / 自动重建）都带上
// 操作人，只有两条路：
//
//	① 给每个 contract 方法加 actor 参数 —— 要改 handler、product 的调用链、presentation
//	   契约与 dto，且自动重建（构建队列 executor）根本没有用户可传；
//	② 把身份塞进 ctx，service 在需要时读一次。
//
// 选 ②（与 product 模块的 WithTranslate / translateFrom 同一条先例）：
// internal/middleware/builtin 的 ActorContextMiddleware 在请求入口写一次，之后整条
// 调用链自带，加一个新的发布入口不需要再记得传参数 —— 漏传就是「谁点的」永久缺失，
// 而漏传不会报错。
//
// 零值是可用的：Actor{} 表示「没有操作人」，即系统触发（构建队列、定时重建）。读侧
// 不该把它当成错误 —— 自动重建本来就没有用户。
package actor

import "context"

// Actor 一次操作的发起人。
//
// ID 与 Name 都是快照：账号被删或改名后，历史记录仍要能回答「当时是谁」，
// 因此落库时写的是这两个值本身，不是外键。
type Actor struct {
	// ID 管理员 id；0 = 系统触发。
	ID int64
	// Name 可读的操作人（登录名优先，缺失时是 id 文本）；空串 = 系统触发。
	Name string
}

// System 该操作没有用户上下文（自动重建 / 定时任务 / 脚本）。
func (a Actor) System() bool { return a.ID == 0 && a.Name == "" }

type ctxKey struct{}

// WithActor 把发起人放进 ctx。重复调用后者覆盖前者（同一请求里只有一个身份）。
func WithActor(ctx context.Context, a Actor) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKey{}, a)
}

// From 取发起人；未注入时返回零值（= 系统触发），调用方不需要判 ok。
//
// 刻意不返回 (Actor, bool)：调用方拿到零值就该按「系统触发」记录，那是一个合法状态，
// 而不是「忘了注入」这种需要分支处理的异常。
func From(ctx context.Context) Actor {
	if ctx == nil {
		return Actor{}
	}
	if a, ok := ctx.Value(ctxKey{}).(Actor); ok {
		return a
	}
	return Actor{}
}
