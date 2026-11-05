package aihttp

import (
	"errors"

	"github.com/gin-gonic/gin"

	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/facing"
	r "go_wp/pkg/response"
)

const aiErrScene = "ai"

// aiErrExit 是 ai 域唯一的失败出口：业务错误按声明里的状态码写真实状态，非业务错误
// 归口成 500 且不把内部原文交给前端。文案由 response 按请求语言从词库取，缺词时
// 退成归口提示 —— controller 不再自己拼句子。
func aiErrExit(c *gin.Context, err error) {
	r.ErrorStatusAuto(c, aiErrScene, aiErrFact(err))
}

// aiErrFact 把任意错误归一成「带声明状态的可出口错误」。
//
// 走 facing.FactOf 而不是自己判 key 形状：FactOf 认的不只是 `enums.Err(key)` 造出来的
// 错误，还包括 Source 接口（`Facing() (key, fallback)`），service 与 casbin 那类
// 带自己 Fact 的错误也能原样透出。
func aiErrFact(err error) error {
	if err == nil {
		return aienums.Err(aienums.ErrInternal)
	}
	fact, ok := facing.FactOf(err)
	if !ok || fact.Key == "" {
		// 不是业务声明：出口会给 500 与归口文案，原文只进日志（ErrorStatusAuto 负责记）。
		return err
	}
	return facing.WithStatus(errors.New(fact.Key), aiErrStatus(fact.Key))
}

// aiErrStatus 取声明里的状态码；未登记时按 400 —— 能走到业务出口的错误至少说明请求有问题。
func aiErrStatus(key string) int {
	if status := aienums.StatusOf(key); status != 0 {
		return status
	}
	return 400
}

// aiPageFail 用于页面 GET 失败：与 aiErrExit 同一条出口。单独留名字是为了让调用点
// 一眼看出「这是页面不是写动作」，出口实现没有第二份。
//
// 为什么不用 adminui.PageErrorInternal：它固定 500 + 归口文案，绕过了 StatusOf 的声明
// 状态；带声明的页面失败（比如 404 的会话不存在）会被压成 500。走 ErrorStatusAuto 才能
// 「声明写什么就回什么」，非业务错误它自己也是 500 + 归口，结果一致。
func aiPageFail(c *gin.Context, err error) {
	aiErrExit(c, err)
}
