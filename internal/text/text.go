package text

// text.go — 文案线：把「一句文案的事实」取成当前语言的文本。
//
// 为什么单独成包（而不是留在 adminui）：取词不是渲染。控制器、service 的只读槽位、
// JSON 出口、模板注入都在取词，但只有其中一部分人在渲染；把取词挂在渲染包上，结果是
// **每个业务模块为了取一句话就得 import 整个后台壳**（adminui 连带 shell / 导航 / 权限码），
// 于是「控制器不碰 UI 线」这条边界永远合不上。
//
// 依赖方向：text → pkg/facing（文案事实）、pkg/i18n（词条）、internal/templates（取词函数）。
// 反方向不存在：adminui 反过来调本包，业务模块直接调本包。
//
// 与 pkg/facing 的分工：facing 定义**事实的形状**（key + 兜底 + 参数）且零依赖；
// 本包负责把事实按**请求语言**渲染成字符串 —— 那是只有 HTTP 出口才拿得到的东西。

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// InternalKey 页面级内部错误提示的 i18n key（词条见 058 / 407 seed）。
//
// i18n key 是字符串协议：模板与各模块自己的同名常量都按字面量取值。
const InternalKey = "MsgInternalError"

// InternalFallback 归口文案的中文原文（与 058 的 zh-CN 词条逐字相同）。
//
// 单独取名是因为它有两个消费者：Internal()，与「未命中业务错误判据」时构造的事实。
// 两处必须是同一句话，抄第二遍必然漂移。
const InternalFallback = "系统内部错误，请稍后重试"

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

// Translate 取一条词条（缺词条时用 fallback，绝不返回裸 key）。
func Translate(c *gin.Context, key, fallback string) string {
	return TranslateFunc(c)(key, fallback)
}

// TranslateArgs 取词 + `%s` 位置占位符填充（模板里注入的 tf 用同一实现）。
//
// 为什么参数是 []string 而不是 map：这批词条用的是 `%s` 位置占位符，调用点一直把计数
// strconv 成字符串再拼。换成具名占位符要改写已 seed 的词条，而 seed 是运营可能改过的数据。
//
// 词条里混进 %d 等协议外占位符时回落中文原文（Sprintf 会把参数渲染成 int，而这条路径
// 直接给运营看）。参数为空时不走 Sprintf。
func TranslateArgs(c *gin.Context, key, fallback string, args []string) string {
	text := Translate(c, key, fallback)
	if len(args) == 0 {
		return text
	}
	if !i18n.HasStringPlaceholdersOnly(text) {
		text = fallback
	}
	return fmt.Sprintf(text, toAnyStrings(args)...)
}

// TranslateFill 取词 + `{name}` 具名占位符填充（语序归词条）。
//
// 与 TranslateArgs 的分工是**谁决定语序**：位置参数按调用方给的顺序填，具名参数由词条
// 自己排（运营把「{n} 个」改成「items: {n}」不用动调用方代码）。
func TranslateFill(c *gin.Context, key, fallback string, params map[string]string) string {
	return i18n.FillTranslate(TranslateFunc(c), key, fallback, params)
}

// MsgText 把一句文案事实取成当前语言的文本。
//
// 局部槽位与 JSON 出口用它（这两类地方模板不再取词）。参数按**填了哪一种**分派：
// 有具名参数走 TranslateFill（语序归词条），否则走 TranslateArgs（`%s` 位置）。
// 分派不按 Key 是否为空判 —— 那样同一个事实会在两条出口给出不同的句子。
func MsgText(c *gin.Context, m facing.Msg) string {
	if len(m.Named) > 0 {
		return TranslateFill(c, m.Key, m.Fallback, m.Named)
	}
	return TranslateArgs(c, m.Key, m.Fallback, m.Args)
}

// Internal 归一后的内部错误文案（当前语言；缺词条回退中文原文）。
//
// 它是**受控文本 helper**，不是出口：只产文案，写不写响应、写什么状态码由调用方决定。
func Internal(c *gin.Context) string {
	return Translate(c, InternalKey, InternalFallback)
}

// InternalMsg 归口文案的**事实**（拿不到具体 err 的失败分支用）：页面不能显示空串。
func InternalMsg() facing.Msg {
	return facing.Msg{Key: InternalKey, Fallback: InternalFallback}
}

// toAnyStrings 把 []string 摊成 []any 交给 fmt.Sprintf（`%s` 对 string 原样输出）。
func toAnyStrings(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}