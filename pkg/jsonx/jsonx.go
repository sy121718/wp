// Package jsonx 是本仓唯一的 JSON 编解码入口。
//
// 底层用 Go 1.27 的 encoding/json/v2（原生语义引擎）与 encoding/json/jsontext（语法层），
// 默认选项逐条还原 encoding/json v1 的可观察行为 —— 「换引擎」不改变任何一处现有输出的字节，
// 也不改变解析的宽容度。需要 v2 的严格性与流式能力时，按调用点显式传选项（例如
// RejectUnknownMembers(true) + errors.Is(err, ErrUnknownName)）。
//
// v1 → v2 的默认差异（本机实测，逐条在此还原）：
//
//	nil slice    v1=null        v2=[]      → FormatNilSliceAsNull(true)
//	nil map      v1=null        v2={}      → FormatNilMapAsNull(true)
//	HTML 字符     v1=\u003c\u003e\u2028 v2=<>&     → jsontext.EscapeForHTML(true)（<>&）
//	行分隔符      v1=\u2028\u2029    v2=原样      → jsontext.EscapeForJS(true)（两者都必须加，缺一条即不一致）
//	重复键        v1=取最后一个   v2=报错      → jsontext.AllowDuplicateNames(true)
//	字段名大小写   v1=大小写不敏感 v2=敏感      → MatchCaseInsensitiveNames(true)
//	map 键顺序    v1=按键排序     v2=未定义    → Deterministic(true)
//
// 唯一无法还原的是**错误文案**：v1 报 `json: cannot unmarshal ... into Go struct field S.n of type int`，
// v2 报 `json: unable to unmarshal JSON string into Go int within "/n"`。错误文案不外泄
// （scripts/check-no-internal-error-leak.sh），需要按原因分流的调用点一律用
// errors.Is(err, jsonx.ErrUnknownName) 这类**错误值判定**，不要匹配文案。
package jsonx

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
)

// RawMessage 是原始 JSON 字节，与 v1 的 json.RawMessage 同底层类型（jsontext.Value）。
type RawMessage = jsontext.Value

// Number 是十进制数字字面量。保留 encoding/json 的别名（而不是自定义类型）：
// v2 原生识别该类型 —— 实测 marshal 原样保留字面量 `1.500`、unmarshal 得到精确十进制，
// 且 Int64/Float64 方法可继续使用。本包只为此别名引用 v1 包，编解码全部走 v2。
type Number = json.Number

// ErrUnknownName 在启用 RejectUnknownMembers 后命中未知字段时返回，供 errors.Is 分流。
var ErrUnknownName = jsonv2.ErrUnknownName

// Options 是 v2 的选项类型（jsontext 的语法层选项与它同型，可直接混用）。
type Options = jsonv2.Options

// v1Compat 还原 v1 默认行为的选项集合，见包注释的差异表。
var v1Compat = jsonv2.JoinOptions(
	jsonv2.Deterministic(true),
	jsonv2.MatchCaseInsensitiveNames(true),
	jsonv2.FormatNilSliceAsNull(true),
	jsonv2.FormatNilMapAsNull(true),
	jsontext.EscapeForHTML(true),
	jsontext.EscapeForJS(true),
	jsontext.AllowDuplicateNames(true),
)

// withV1 把调用点显式传入的选项叠加在 v1 兼容集合之上（后者优先被覆盖的语义由 v2 的 JoinOptions 决定：
// 后加入的选项生效）。
func withV1(opts ...jsonv2.Options) []jsonv2.Options {
	out := make([]jsonv2.Options, 0, len(opts)+1)
	out = append(out, v1Compat)
	out = append(out, opts...)
	return out
}

// RejectUnknownMembers 开启后，遇到结构体上不存在的字段返回 ErrUnknownName（v1 的
// DisallowUnknownFields 等价物，判定请用 errors.Is 而不是文案）。
func RejectUnknownMembers(v bool) jsonv2.Options { return jsonv2.RejectUnknownMembers(v) }

// Marshal 序列化 v；默认行为与 encoding/json.Marshal 逐字节一致。
func Marshal(v any, opts ...jsonv2.Options) ([]byte, error) {
	return jsonv2.Marshal(v, withV1(opts...)...)
}

// MarshalIndent 等价于 v1 的 json.MarshalIndent（含前缀语义）。
//
// 刻意不走 jsontext.Encoder：它在每个值后追加换行，会让输出比 v1 多一个字节（实测）。
func MarshalIndent(v any, prefix, indent string, opts ...jsonv2.Options) ([]byte, error) {
	raw, err := Marshal(v, opts...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := Indent(&buf, raw, prefix, indent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalWrite 把 JSON 写到 w（v2 的流式能力，v1 无对应 API）。
//
// 输出与 Marshal 逐字节一致（**不追加换行**）：jsonv2.MarshalWrite 会在值后写一个换行，
// 与本包「默认与 v1 对齐」的定位不符。需要 v2 原生流式语义（含尾换行、NDJSON 场景）时
// 请直接使用 encoding/json/v2。
func MarshalWrite(wr io.Writer, v any, opts ...jsonv2.Options) error {
	b, err := Marshal(v, opts...)
	if err != nil {
		return err
	}
	_, err = wr.Write(b)
	return err
}

// Unmarshal 反序列化；默认宽容度与 encoding/json.Unmarshal 一致。
func Unmarshal(data []byte, v any, opts ...jsonv2.Options) error {
	return jsonv2.Unmarshal(data, v, withV1(opts...)...)
}

// UnmarshalRead 从 r 读取 JSON 并反序列化。
func UnmarshalRead(r io.Reader, v any, opts ...jsonv2.Options) error {
	return jsonv2.UnmarshalRead(r, v, withV1(opts...)...)
}

// Valid 判断 b 是否是一个完整的 JSON 值（v1 的 json.Valid）。
func Valid(b []byte) bool { return jsontext.Value(b).IsValid() }

// Compact 压缩 dst 中的 JSON（v1 的 json.Compact）。
//
// 必须先复制 src：jsontext.Value 的 Compact/Indent 是**原地**操作，直接别名 src 会把调用方的
// 输入静默改写（v1 的 json.Compact 不改 src）。实测后果：同一个 src 连续调用 Compact 再 Indent 时，
// 后者报 `invalid character '[' after top-level value`。
func Compact(dst *bytes.Buffer, src []byte) error {
	v := jsontext.Value(bytes.Clone(src))
	if err := v.Compact(); err != nil {
		return err
	}
	_, err := dst.Write(v)
	return err
}

// Indent 按 prefix/indent 重排 JSON 到 dst（v1 的 json.Indent）。
//
// 与 Compact 同理，必须复制 src，不能别名调用方的输入。
func Indent(dst *bytes.Buffer, src []byte, prefix, indent string) error {
	v := jsontext.Value(bytes.Clone(src))
	if err := v.Indent(jsontext.WithIndent(indent), jsontext.WithIndentPrefix(prefix)); err != nil {
		return err
	}
	_, err := dst.Write(v)
	return err
}
