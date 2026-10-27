package builtin

// actor_context.go — 把「这次请求是谁」放进 context.Context（pkg/actor）。
//
// 为什么在中间件里做：发布历史的每一行都要记操作人，而 presentation 的 service 一路
// 不认识用户 —— 身份只在 gin.Context 里。让每个发布入口自己传 actor，意味着加一个新
// 入口就可能漏传，而漏传不会报错，只会让历史里少一列。挂在链上一次，整条 /api 调用面
// 自带。
//
// 位置要求：必须排在 SessionAuthMiddleware **之后**（身份由它写入），排在
// CasbinMiddleware 之后不额外损失什么 —— 权限拒绝的请求本来就不会走到 service。
//
// 未登录（无 user_id）时写入零值 Actor，service 侧按「系统触发」记账，不分支处理。

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/actor"
)

// ActorContextMiddleware 把当前登录管理员作为发起人注入请求 ctx。
func ActorContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request != nil {
			a := actor.Actor{
				ID:   int64(CurrentUserID(c)),
				Name: OperatorName(c),
			}
			c.Request = c.Request.WithContext(actor.WithActor(c.Request.Context(), a))
		}
		c.Next()
	}
}
