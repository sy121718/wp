package text

// text.go — 请求级的取词**函数**与归口判定，不是取词本身。
//
// 本包只剩两件事：
//
//	① Inject：把 t / tf（取词函数）注入模板数据 —— 取词发生在渲染期，由模板调用；
//	② FactOf：把错误收敛成「业务事实 / 归口文案」，供出口记日志与判状态码。
//
// 取词的唯一实现在 pkg/response（MsgText / ArgsText / FillText / InternalText）：
// 响应出口按请求语言取一次词。控制器与 service 不取词、不拼句 —— 业务层只给
// facing.Msg 或 error（key + 兜底）。
//
// 依赖方向：text → internal/templates（取词函数）、pkg/response（语言与归口常量）、
// pkg/facing（事实形状）、pkg/logger。反方向不存在。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// InternalKey 页面级内部错误提示的 i18n key（词条见 058 / 407 seed）。
//
// i18n key 是字符串协议：模板与各模块自己的同名常量都按字面量取值。
const InternalKey = response.InternalKey

// InternalFallback 归口文案的中文原文（与 058 的 zh-CN 词条逐字相同）。
//
// 单独取名是因为它有两个消费者：Internal()，与「未命中业务错误判据」时构造的事实。
// 两处必须是同一句话，抄第二遍必然漂移。
const InternalFallback = response.InternalFallback

// FactOf 判定一次并归口：业务错误原样给出事实；非业务错误记一条结构化日志后给归口文案。
//
// 判据在 pkg/facing（形态判据，JSON 出口与页面出口同一份）。本函数补的是「出口那一半」：
// 请求上下文（日志里的 path / user_id）与归口文案。业务模块不再自己判、也不再自己拼归口。
//
// 为什么首页面出口在 adminui：出口不是渲染 —— 详情页的只读槽位、JSON 接口都会用到它，
// 而那些调用点不该 import 整个后台壳（与取词线上移到本包是同一个理由）。
func FactOf(c *gin.Context, scene string, err error) facing.Fact {
	if err == nil {
		return facing.Fact{}
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	logger.Scene(scene).
		With("path", RequestURI(c)).
		With("user_id", builtin.CurrentUserID(c)).
		Error(err, "操作失败（非业务错误，只对外给归口文案）")
	return facing.Fact{Key: InternalKey, Fallback: InternalFallback}
}

// Lang 当前请求语言（BCP-47 的短码，如 zh-CN / en-US）。
func Lang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// TranslateFunc 当前请求语言的取词函数：func(key, fallback) string。
//
// 模板侧的 t 用的是同一个函数（见 Inject），所以「Go 侧取词」与「渲染期取词」
// 对同一条词条给出同一个字符串。
func TranslateFunc(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(Lang(c))
}
