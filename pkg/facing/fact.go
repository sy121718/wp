// fact.go — 业务错误的判据与事实形状。
//
// 为什么判据在这里而不是 pkg/response：response 只管 **JSON 出口**，而「什么叫业务错误」
// 是**跨出口**的约定 —— JSON 接口、后台页面、访客页、AI 工具判的是同一件事。判据留在
// response 里，第二条出口就会自己再判一遍，两份判据漂移的症状是「同一条错误，
// 接口返回了具体原因、页面上只说『系统内部错误』」，而两边都不报错、也都不进日志。
//
// 本包**零业务依赖**（只有标准库）：出口包负责请求语言、结构化日志与响应写入，
// 本包只回答「这条错误是什么事实」。
package facing

import (
	"errors"
	"regexp"
	"strings"
)

// businessErrKey 匹配「业务错误」的消息形态：`模块.类别.语义`，例如 cart.err.outOfStock。
//
// **类别本身可以是多级**（`模块.类别.子类别.语义`）：`admin.navigation_translations.err.rowCountMismatch`
// 与 `admin.product_edit.err.defaultPriceInvalid` 都把类别写成 `xxx.err` 两级，全仓 33 个此类值。
// 因此判据是「三段起，可更长」而不是「恰好三段」：原分支只认恰好三段，把这 33 个里的 7 个判成
// 「不命中任何判据」，而它们的词条都在库里、运行时并不会退化成 500 —— 那是**判据的假阳性**，
// 不是命名违约。放宽到多段没有削弱它守的东西：内部错误（fmt.Errorf / SQL / os）不长成
// 点分小写的形状，多一级类别也不改变这个结论。
var businessErrKey = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.[a-zA-Z0-9_]+(\.[a-zA-Z0-9_]+)*(\|.*)?$`)

// businessErrConstant 未迁形态的 enums 常量名形态：ErrAttachmentNotFound / MsgListSuccess。
//
// 为什么需要这一层：CQ-010 把 170 处 handler 换成 ErrorAuto 时，只有 cart / mail / order / user
// 四个模块的 enums 迁到了 key 形态（`模块.err.语义`），其余模块仍是「常量名即消息」——
// media 的 `ErrAttachmentNotFound = "ErrAttachmentNotFound"`、inventory 的
// `ErrSourceFilterInvalid = "ErrSourceFilterInvalid"`。判据只认 key 形态时，这些模块的**全部**
// 业务错误都会被判成内部错误 → 一律 500 + 通用文案，与 CQ-010 自己声明的「业务错误透出消息」
// 恰好相反。这一层把常量名形态补上（全仓 429 个此类常量），形态判定零成本且不需要改动 170 处调用。
var businessErrConstant = regexp.MustCompile(`^(Err|Msg)[A-Z][A-Za-z0-9]*(\|.*)?$`)

// IsBusinessKey 一个**字符串**（错误消息 / enums 取值）是否是业务错误的形态。
//
// 判据只有两层，两者都是**形态确定**的（不依赖字符串特征的启发式）：
//  1. key 形态 `模块.类别.语义`（已迁模块，如 cart.err.outOfStock）；参数化协议（key|param）
//     也算业务错误 —— 翻译层会把 | 后面的部分当参数填进模板；
//  2. enums 常量名形态 `ErrXxx` / `MsgXxx`（仍是「常量名即消息」的模块，如 ErrSourceFilterInvalid）。
//
// 两层都接受参数化后缀 `|param`：翻译层把 | 后面当参数填模板。缺了它，未迁模块的
// `ErrInvalidParam|/about` 会被判成内部错误 —— 症状是该条业务文案被吞成 500 通用句。
func IsBusinessKey(msg string) bool {
	return businessErrKey.MatchString(msg) || businessErrConstant.MatchString(msg)
}

// IsBusinessError 判断一个错误的消息是否是业务错误（可对外展示）。
//
// 这是**唯一**一份判据：pkg/response 的 JSON 出口与 internal/adminui 的页面出口都调它。
func IsBusinessError(err error) bool {
	if err == nil {
		return false
	}
	return IsBusinessKey(err.Error())
}

// Fact 一条错误事实。
//
// 形状与「怎么展示」无关：Key 是 i18n key（决定语言）、Fallback 是词条缺失时的中文兜底、
// Tail 是「key：明细」里的明细段、Business 表示它命中了业务判据（未命中即归口）。
type Fact struct {
	Key      string // 业务 key；未命中时是出口包的归口 key（err 为 nil 时才是空串）
	Fallback string // 中文兜底（通常是 key 字面量：enums 常量名即 key）
	Tail     string // 「key：明细」里的明细段原文（可为空；未解码）
	Business bool   // 是否命中业务错误判据
}

// Source 自带对外文案的错误（本包的 Error 实现它）。
//
// 判据按**接口形状**给，不断言具体类型：模块只声明错误、不 import 出口包，
// 也能把「key + 中文兜底」一起交出来（见 Error 的注释）。
type Source interface {
	Facing() (key, fallback string)
}

// FactOf 把一条业务错误拆成 key + 明细段。
//
// 三种入参形态：
//   - 自带文案：`facing.New(key, "一句话")` —— 兜底跟着声明处走；
//   - 单段：`ErrInvalidDocument` / `cart.err.outOfStock` —— 整体就是 key；
//   - 复合：模块用 `fmt.Errorf("%w: <明细>")` 把「枚举 + 明细」打成一条 error，整体过不了
//     形态判据（正则是**锚定**的），所以按 ASCII 冒号切一刀、拿**前一段**判定。
//
// 为什么不把复合形态也放宽进 IsBusinessError：那会让 JSON 出口也把复合错误整条原样下发 ——
// 「枚举 key：明细」里的明细是模块自造的（page 用它装 ErrorDetail 编码，别的模块装过中文原文），
// 是否可展示属于**出口**的判断，不该由共享判据替所有出口做主。
func FactOf(err error) (Fact, bool) {
	if err == nil {
		return Fact{}, false
	}
	// ① 自带文案的错误优先：兜底跟着错误声明处走，不靠 key 的形态去猜该给用户哪句话。
	if f, ok := selfFacingFact(err); ok {
		return f, true
	}
	raw := strings.TrimSpace(err.Error())
	// ② 单段业务错误：整条就是 key。
	if IsBusinessKey(raw) {
		return Fact{Key: raw, Fallback: raw, Business: true}, true
	}
	// ③ 复合形态：`key: 明细`。
	idx := strings.IndexByte(raw, ':')
	if idx <= 0 {
		return Fact{}, false
	}
	key, tail := strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:])
	if key == "" || !IsBusinessKey(key) {
		return Fact{}, false
	}
	return Fact{Key: key, Fallback: key, Tail: tail, Business: true}, true
}

// selfFacingFact 从「自带文案的错误」取事实（含 `%w` 包装）。
func selfFacingFact(err error) (Fact, bool) {
	var src Source
	if !errors.As(err, &src) {
		return Fact{}, false
	}
	key, fallback := src.Facing()
	if strings.TrimSpace(key) == "" {
		return Fact{}, false
	}
	return Fact{
		Key:      key,
		Fallback: fallback,
		Tail:     tailAfterKey(err.Error(), key),
		Business: true,
	}, true
}

// tailAfterKey 取「key：明细」里的明细段（没有则空串）。分隔符只认 ASCII 冒号，
// 与 FactOf 的切分口径一致。
func tailAfterKey(raw, key string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), key))
	return strings.TrimSpace(strings.TrimPrefix(rest, ":"))
}
