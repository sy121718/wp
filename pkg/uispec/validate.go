package uispec

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"unicode/utf8"
)

var ErrRejected = errors.New("uispec: 拒绝渲染")

const (
	KindMalformed     = "malformed"
	KindUnknownField  = "unknown_field"
	KindUnknownType   = "unknown_type"
	KindEmpty         = "empty"
	KindTooLarge      = "too_large"
	KindMissingSource = "missing_source"
	KindBadValue      = "bad_value"
)

type Reject struct {
	Kind  string
	Where string
	Msg   string
}

func (e *Reject) Error() string {
	if e.Where == "" {
		return fmt.Sprintf("uispec: %s: %s", e.Kind, e.Msg)
	}
	return fmt.Sprintf("uispec: %s: %s: %s", e.Kind, e.Where, e.Msg)
}

func (e *Reject) Is(target error) bool { return target == ErrRejected }

func reject(kind, where, format string, args ...any) *Reject {
	return &Reject{Kind: kind, Where: where, Msg: fmt.Sprintf(format, args...)}
}

func Parse(raw []byte) (*Spec, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, reject(KindMalformed, "", "输入为空")
	}

	var s Spec
	if err := json.Unmarshal(trimmed, &s, json.RejectUnknownMembers(true)); err != nil {
		if errors.Is(err, json.ErrUnknownName) {
			return nil, reject(KindUnknownField, "", "出现白名单之外的字段: %v", err)
		}
		return nil, reject(KindMalformed, "", "JSON 解析失败: %v", err)
	}
	if err := Validate(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

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

func validateBlock(i int, b Block) error {
	where := fmt.Sprintf("blocks[%d]", i)
	if !knownType(b.Type) {
		return reject(KindUnknownType, where, "组件类型 %q 不在白名单（%s / %s / %s / %s）",
			b.Type, TypeStat, TypeTable, TypeList, TypeAccordion)
	}
	if n := utf8.RuneCountInString(b.Title); n > MaxTitleRunes {
		return reject(KindTooLarge, where+".title", "标题 %d 字超过上限 %d", n, MaxTitleRunes)
	}

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

func knownType(t string) bool {
	switch t {
	case TypeStat, TypeTable, TypeList, TypeAccordion:
		return true
	default:
		return false
	}
}

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
