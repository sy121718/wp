package adminui

// page_error.go — **后台页面出口**的错误事实：判定一次，各模块共用。
//
// 判据与事实形状的唯一实现在 **pkg/facing**（IsBusinessError / FactOf）—— 「什么叫业务错误」
// 是跨出口的约定（JSON 接口、后台页面、访客页判同一件事），放在本包会让第二条出口自己再判一遍。
// 本包保留的是**出口那一半**：请求上下文（日志里的 path / user_id）、归口文案、
// 以及把事实取成当前语言文本。模块只把自己的 error 递进来。
//
// 业务错误的取值本来就是模块 enums 常量，且 enums 的值即 i18n key（迁移 058 起），
// **所以模块侧不需要再写一份 XxxFacingMessages 白名单**。
//
// 白名单为什么会坏：它是第二份真相 —— service 换了措辞、或某个 key 改由别的模块提供，白名单
// 就静默失配，而两种症状都不报错：有时把内部错误当业务文案透出，有时把业务文案吞成归口；
// 加上「明细不是词条就丢弃」那条协议，同一个错误在 JSON 出口与页面出口还会给出不同结论。
//
// 三段事实的由来：模块 service 常用 fmt.Errorf("%w: <ErrorDetail 编码>") 把「枚举 + 明细」
// 打成一条 error，所以事实天生是两段。Tail 原样带出、由模块自己解码 —— 明细词条的回落表在
// 模块自己的 enums 里（如 pageenums.ErrDetailFallbacks），共享层不替它保管。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
)

// ErrFact 页面出口的错误事实（形状定义见 pkg/facing.Fact）。
//
// 别名是迁移期兼容：新代码直接用 facing.Fact。
type ErrFact = facing.Fact

// PageErrorFact 判定 + 归口（**记日志**）：给「调用点没有更具体的日志」的地方用。
func PageErrorFact(c *gin.Context, scene string, err error) ErrFact {
	return pageErrorFact(c, scene, err, true)
}

// PageErrorFactQuiet 同 PageErrorFact，但**不记日志** —— 给「调用点已经记过一条更具体的日志
// （带 pageId / lang 等）」的地方用。再记一次会让同一个错误在日志里出现两遍。
func PageErrorFactQuiet(c *gin.Context, scene string, err error) ErrFact {
	return pageErrorFact(c, scene, err, false)
}

func pageErrorFact(c *gin.Context, scene string, err error, logIt bool) ErrFact {
	if err == nil {
		return ErrFact{}
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	if logIt {
		logger.Scene(scene).
			With("path", requestURIOf(c)).
			With("user_id", builtin.CurrentUserID(c)).
			Error(err, "后台页面操作失败（非业务错误，只对外给归口文案）")
	}
	return ErrFact{Key: MsgInternalError, Fallback: internalErrorFallback}
}

// BusinessErrFact 把一条业务错误拆成 key + 明细段。
//
// 判据与实现都在 **pkg/facing**（FactOf：自带文案的错误 → 单段 key → `key: 明细` 三档）。
// 保留本函数为转发：它已有若干外部调用点，改名没有收益；新代码请直接用 facing.FactOf。
func BusinessErrFact(err error) (ErrFact, bool) {
	return facing.FactOf(err)
}

// ErrorFactText 把一条事实取成当前语言文案（**不含明细**；明细由模块自己拼）。
//
// err 为 nil（零值事实）返回空串 —— 「没有错误」与「归口文案」必须区分得开。
func ErrorFactText(c *gin.Context, fact ErrFact) string {
	if fact.Key == "" {
		return ""
	}
	return translateFor(c)(fact.Key, fact.Fallback)
}

// PageErrorText 页面出口的成品文案：业务错误取当前语言，其余归口 + 日志。
func PageErrorText(c *gin.Context, scene string, err error) string {
	return ErrorFactText(c, PageErrorFact(c, scene, err))
}

// PageErrorTextQuiet 同 PageErrorText，但不记日志。
func PageErrorTextQuiet(c *gin.Context, scene string, err error) string {
	return ErrorFactText(c, PageErrorFactQuiet(c, scene, err))
}

// InternalErrorFact 归口事实（key = MsgInternalError）：给「拿不到 err 的失败分支」用 ——
// 提示页不能显示空串，而空 key + 空兜底会让整页提示变成空白。
func InternalErrorFact() ErrFact {
	return ErrFact{Key: MsgInternalError, Fallback: internalErrorFallback}
}

// TranslateArgs 取词 + `%s` 占位符填充（模板里注入的 tf 用同一实现）。
//
// 为什么参数是 []string 而不是 map：这批词条（283_bulk_notice_i18n.sql 起）用的就是 `%s`
// 位置占位符，调用点也一直是把计数 strconv 成字符串再拼（见迁移前各模块的 pageBulkTextOf）。
// 换成具名占位符要**改写已 seed 的词条**，而 seed 是运营可能改过的数据 —— 那属于独立的
// 文案迁移，不该夹在「控制器不取词」这一步里做。
//
// 词条里混进 %d 等协议外占位符时回落中文原文：Sprintf 会把参数渲染成 int 而这条路径
// 直接给运营看（原文见迁移前的 pageBulkTextOf）。参数为空时不走 Sprintf。
func TranslateArgs(c *gin.Context, key, fallback string, args []string) string {
	return text.TranslateArgs(c, key, fallback, args)
}

// TranslateFill 取词 + `{name}` 具名占位符填充（MsgText 的具名档走这里）。
//
// 与 TranslateArgs 的分工是**谁决定语序**：位置参数按调用方给的顺序填（词条把 `%s` 排错位
// 就渲染错），具名参数由词条自己排（运营把「{n} 个」改成「items: {n}」不用动调用方代码）。
// 两条并存的原因见 TranslateArgs 的注释：位置参数那批是已 seed、运营可能改过的数据。
//
// 降级同口径：词条缺席或占位符被改坏时回落中文兜底再填一次，绝不把 `{n}` 这样的字面量
// 摆给运营看（这层回落实现在 pkg/i18n.FillTranslate）。
func TranslateFill(c *gin.Context, key, fallback string, params map[string]string) string {
	return text.TranslateFill(c, key, fallback, params)
}

// toAnyStrings 把 []string 摊成 []any 交给 fmt.Sprintf（`%s` 对 string 原样输出）。
func toAnyStrings(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
