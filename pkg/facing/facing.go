// Package facing — 自带对外文案的哨兵错误。
//
// 业务错误的对外文案由两半组成：i18n key（决定语言）+ 中文兜底（词条缺失时用）。
// 原先兜底那一半写在 inbound/http 的错误映射函数里，每个模块一份 switch：
//
//	func pageLangErrMsg(c *gin.Context, err error) pageMsg {
//		switch {
//		case errors.Is(err, pageservice.ErrCannotExcludeDefaultLang):
//			return pageMsg{Key: pageenums.ErrCannotExcludeDefaultLang,
//				Fallback: "不能排除站点默认语言：它的产物承载 x-default…"}
//		case errors.Is(err, pageservice.ErrPageLangExcluded):
//			…
//		}
//		return pageErrMsg(c, err)
//	}
//
// 于是「加一个业务错误」要改**两个包**，而第二个包（http 层）在 service 里完全看不见 ——
// 二开的人加完 sentinel，页面上显示的是裸 key，还不知道该去哪补那句话。
//
// 绑在声明处之后：加错误 = 加一行，出口只调一次归口判据
// （adminui.PageErrorFact 经本包声明的 Facing() 取值，也认 `fmt.Errorf("%w: 明细")` 包装）。
package facing

// Error 一条自带对外文案的哨兵错误。
//
// Error() 返回的**就是** i18n key —— pkg/response.IsBusinessError 的形态判据照常命中，
// 于是 JSON 出口（response.ErrorAuto）与页面出口（adminui.PageErrorFact）读同一份事实，
// 不存在「接口返回的是 A、页面上写的是 B」这种分叉。
type Error struct {
	key      string
	fallback string
}

// New 声明一条自带文案的哨兵错误。
//
// key 按 enums 的 `模块.err.xxx` 形态给（与 migration seed 的 item_key 一致）；
// fallback 是一句完整的话 —— 词条缺失或新装库尚未 seed 时，用户看到的就是它。
// fallback 传空串时回落成 key：兜底为空会让失败在页面上**完全不可见**，
// 用户以为操作成功了（见 adminui 的 Msg 契约）。
func New(key, fallback string) *Error {
	if fallback == "" {
		fallback = key
	}
	return &Error{key: key, fallback: fallback}
}

// Error 返回 i18n key（哨兵错误的既有形态：整条消息就是一个 key）。
func (e *Error) Error() string { return e.key }

// Is 让同 key 的两条哨兵互相匹配（errors.Is 按 key 判，而不是按指针）。
//
// 为什么需要：`Err(key)` 每调用一次就是一个新指针，而 service 既可能导出一个哨兵常量
// （`var ErrInvalidRange = enums.Err(enums.ErrInvalidRange)`）、又可能在深处重新构造同一条
// （`return nil, enums.Err(enums.ErrInvalidRange)`）。没有 Is 时两句之间 errors.Is 永远 false，
// 调用方会静默走进 default 分支 —— 判据没错，比较方式错了。
func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}
	return target.Error() == e.key
}

// Facing 对外文案事实（key, 中文兜底）。
//
// 接口的形状在**消费端**（adminui）声明，本包不反过来依赖任何出口 —— 模块只要
// 用 New 声明错误，就自动被页面出口认出来，不需要 import 出口包。
func (e *Error) Facing() (key, fallback string) { return e.key, e.fallback }

// Labeled 一条带「哪一行」标签的文案事实（多行回执用：逐行结论、批量拒绝原因）。
//
// Label 是**已经定位到那一行**的名字（语境名 / SKU / 字段名），不是用户文案；Msg 是文案事实，
// 取词只在出口发生一次。放在 pkg/facing 是因为业务层要产出它，而它不能依赖 gin。
type Labeled struct {
	Label string
	Msg   Msg
}
