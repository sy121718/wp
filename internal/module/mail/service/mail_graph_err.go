package mailservice

// mail_graph_err.go — 图校验错误的**可翻译明细**与写侧包装。
//
// 两条协议在这里汇合（见 pkg/i18n/errdetail.go 与 mailenums/mail_err_detail.go）：
//
//	① 图校验器（mail_automation_graph.go）不再直接产出中文 error，而是 GraphError ——
//	   它的 Error() 仍是那句中文（日志与 mail_automation_graph_test.go 的断言看的都是它），
//	   同时 Detail() 给出 i18n.ErrorDetail 编码（词条 key + 具名参数）；
//	② service 包装点用 graphInvalidError 把「业务 key：明细编码」拼成业务错误，
//	   读侧（mail_err.go 的 translateMailFacing）据此按当前语言取词。
//
// 为什么不让校验器直接产出编码串：图校验的文本是**运维日志**与**单测**的判据
//（「流程里有环」「有节点从入口走不到」），把它们换成控制字符 + key 会让日志不可读、
// 单测断言全部失效。两份文本各有判据，所以都用**同一个模板**生成，不各写一遍。

import (
	"errors"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/i18n"
)

// GraphError 一条图校验失败（中文原文 + 词条 key + 具名参数）。
type GraphError struct {
	key  string
	args []string // name, value 交替（i18n.ErrorDetail 的入参形态）
	text string   // 中文原文（含已填充的参数）
}

// Error 中文原文：日志与单测断言看这一份。
func (e *GraphError) Error() string {
	if e == nil {
		return ""
	}
	return e.text
}

// Detail i18n.ErrorDetail 编码：写侧拼进业务错误的 tail，读侧据此取词。
func (e *GraphError) Detail() string {
	if e == nil {
		return ""
	}
	return i18n.ErrorDetail(e.key, e.args...)
}

// graphErr 造一条 GraphError。
//
// tmpl 是**中文模板**（`{name}` 占位），kv 是 name, value 交替 —— 参数只写一遍，
// 中文原文由模板填充得到，编码串由同一组参数生成，两边不可能漂移。
func graphErr(key, tmpl string, kv ...string) *GraphError {
	params := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i]] = kv[i+1]
	}
	text, ok := i18n.FillNamedPlaceholders(tmpl, params)
	if !ok {
		// 模板与参数不匹配是**代码缺陷**（少传一个参数）：不让它静默 ——
		// 原文回落模板本身，日志里能一眼看出漏填的 `{name}`。
		text = tmpl
	}
	return &GraphError{key: key, args: append([]string(nil), kv...), text: text}
}

// graphInvalidError 图校验错误 → 业务错误（key + 可翻译明细）。
//
// 不是 GraphError 时只给业务 key、**不留 tail**：那种情况是 JSON / 装配层的原文，
// 词条化不了 —— 按读侧协议「不是词条就丢弃并落日志」，这里直接不编码，
// 免得读侧为它多记一条「形态不符」的日志。
func graphInvalidError(err error) error {
	var ge *GraphError
	if errors.As(err, &ge) {
		return errors.New(mailenums.ErrAutomationGraphInvalid + ": " + ge.Detail())
	}
	return errors.New(mailenums.ErrAutomationGraphInvalid)
}

// graphInvalidRequestError 请求体里的 definition 不是合法 JSON → 业务错误（带解析器原文）。
//
// reason 是 json 解码器给的位置说明（数据，不是内部细节），所以作为具名参数进词条，
// 让运营知道「哪一段不是 JSON」，而不是只看到一句「流程定义不合法」。
func graphInvalidRequestError(reason string) error {
	return errors.New(mailenums.ErrAutomationGraphInvalid + ": " +
		i18n.ErrorDetail(mailenums.DetailGraphRequestNotJSON, "reason", reason))
}
