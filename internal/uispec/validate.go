package uispec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ErrRejected 是所有「这条 spec 不能渲染」的归口错误。
//
// 上层只判这一个：命中它就是**降级为纯文本回答 + 落一条诊断事件**，
// 而不是白屏或 500（docs/17 P5 闸门）。
var ErrRejected = errors.New("uispec: 拒绝渲染")

// 拒绝原因标签（机器可读，写进诊断事件；不要用 Error() 的文案做判据）。
const (
	KindMalformed     = "malformed"      // JSON 本身不合法，或有多余内容
	KindUnknownField  = "unknown_field"  // 出现了白名单之外的字段
	KindUnknownType   = "unknown_type"   // 组件类型不在白名单
	KindEmpty         = "empty"          // 既没有文字也没有组件
	KindTooLarge      = "too_large"      // 超出规模上限
	KindMissingSource = "missing_source" // 块没有数据源
	KindBadValue      = "bad_value"      // 单个值不合规（超长、字符合法但不合约定）
)

// Reject 一次拒绝：原因标签 + 位置 + 人读的说明。
//
// 为什么不用「每种原因一个哨兵错误」：调用方需要的是**两个层次**的信息 ——
// 「要不要降级」（一个 ✓）与「为什么」（多种，进日志与诊断）。用 Is 认 ErrRejected、
// 用 As 取 Kind，比在调用点写一长串 errors.Is(err, A) || errors.Is(err, B) 更不容易漏。
type Reject struct {
	Kind  string
	Where string
	Msg   string
}

// Error 实现 error。
func (e *Reject) Error() string {
	if e.Where == "" {
		return fmt.Sprintf("uispec: %s: %s", e.Kind, e.Msg)
	}
	return fmt.Sprintf("uispec: %s: %s: %s", e.Kind, e.Where, e.Msg)
}

// Is 让 errors.Is(err, ErrRejected) 命中（降级判断的唯一入口）。
func (e *Reject) Is(target error) bool { return target == ErrRejected }

// reject 构造一条拒绝。
func reject(kind, where, format string, args ...any) *Reject {
	return &Reject{Kind: kind, Where: where, Msg: fmt.Sprintf(format, args...)}
}

// Parse 把模型输出的 JSON 解析成 Spec 并校验。
//
// 三层一起做，缺一层都会漏：
//
//	① 语法（JSON 合法、没有两个对象拼在一起）；
//	② 形状（字段名全在白名单里 —— DisallowUnknownFields，模型自创字段一律拒绝）；
//	③ 界限（组件数 / 长度 / 行数上限）。
func Parse(raw []byte) (*Spec, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, reject(KindMalformed, "", "输入为空")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		// DisallowUnknownFields 与语法错误共用同一个返回点，但它们是**两类**原因，
		// 上层要分开处置：语法错多半是模型输出截断（可重试），未知字段是协议外的东西
		// （要进诊断、看模型在发明什么）。所以这里按 encoding/json 的文案分流 ——
		// 这是唯一一处依赖标准库错误文案的地方，测试把它钉住了（改动会让用例红）。
		if strings.Contains(err.Error(), "unknown field") {
			return nil, reject(KindUnknownField, "", "出现白名单之外的字段: %v", err)
		}
		return nil, reject(KindMalformed, "", "JSON 解析失败: %v", err)
	}
	// 尾随内容：模型偶尔会「先出 spec 再补一段」，Go 的 Decode 只吃第一个值，
	// 不检查就会把后半段静默丢掉 —— 那半个回答去哪了将无从查起。
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, reject(KindMalformed, "", "JSON 之后还有多余内容")
	}
	if err := Validate(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate 只做语义校验（形状与界限），不做 JSON 解析。
//
// 与 Parse 分开是因为调用方可能已经持有一个 Spec（例如从事件里反序列化出来的历史 spec）。
func Validate(s *Spec) error {
	if s == nil {
		return reject(KindEmpty, "", "spec 为空")
	}
	if len(s.Blocks) == 0 {
		if strings.TrimSpace(s.Text) == "" {
			return reject(KindEmpty, "", "既没有文字也没有组件")
		}
		return nil
	}
	if len(s.Blocks) > MaxBlocks {
		return reject(KindTooLarge, "blocks", "组件数 %d 超过上限 %d", len(s.Blocks), MaxBlocks)
	}
	for i, b := range s.Blocks {
		if err := validateBlock(i, b); err != nil {
			return err
		}
	}
	return nil
}

// validateBlock 校验单个块。
func validateBlock(i int, b Block) error {
	where := fmt.Sprintf("blocks[%d]", i)
	if !knownType(b.Type) {
		return reject(KindUnknownType, where, "组件类型 %q 不在白名单（%s / %s / %s / %s）",
			b.Type, TypeStat, TypeTable, TypeList, TypeAccordion)
	}
	if n := utf8.RuneCountInString(b.Title); n > MaxTitleRunes {
		return reject(KindTooLarge, where+".title", "标题 %d 字超过上限 %d", n, MaxTitleRunes)
	}
	// 每个块都必须有数据源：块的内容全部来自服务端查询（决策 D5），
	// 没有 source 的块没有任何可渲染的东西 —— 放过去只会渲染出一个空壳。
	src := strings.TrimSpace(b.Source)
	if src == "" {
		return reject(KindMissingSource, where, "缺少 source（数据源 = 已注册工具名）")
	}
	if len(src) > MaxSourceLen {
		return reject(KindTooLarge, where+".source", "数据源名 %d 字节超过上限 %d", len(src), MaxSourceLen)
	}
	if !validSourceName(src) {
		return reject(KindBadValue, where+".source",
			"数据源名 %q 含非法字符（只允许小写字母、数字与下划线，且不以数字开头）", src)
	}
	if len(b.Params) > MaxParams {
		return reject(KindTooLarge, where+".params", "参数 %d 个超过上限 %d", len(b.Params), MaxParams)
	}
	for k, v := range b.Params {
		if len(k) > MaxParamKey {
			return reject(KindTooLarge, where+".params", "参数名 %d 字节超过上限 %d", len(k), MaxParamKey)
		}
		if n := utf8.RuneCountInString(v); n > MaxParamValue {
			return reject(KindTooLarge, where+".params."+k, "参数值 %d 字超过上限 %d", n, MaxParamValue)
		}
	}
	if b.Limit != 0 && (b.Limit < MinLimit || b.Limit > MaxLimit) {
		return reject(KindBadValue, where+".limit", "limit=%d 超出 [%d, %d]", b.Limit, MinLimit, MaxLimit)
	}
	return nil
}

// knownType 组件类型是否在白名单。
func knownType(t string) bool {
	switch t {
	case TypeStat, TypeTable, TypeList, TypeAccordion:
		return true
	default:
		return false
	}
}

// validSourceName 数据源名是否合法。
//
// 与 MCP 工具名同一套约定（小写字母、数字、下划线）。限定字符集不是因为这里会产生注入 ——
// 名字只用来查 map —— 而是因为这个名字会进日志、审计与诊断事件，混杂任意字符会让
// 「按工具名统计调用」这类后续分析变得不可靠。
func validSourceName(s string) bool {
	if s == "" {
		return false
	}
	if s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
