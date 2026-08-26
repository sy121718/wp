package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ControlKind 控件类型（声明式描述符，对标 WP register_controls 的控件体系）。
type ControlKind string

// 控件类型常量。
const (
	ControlString ControlKind = "string" // 字符串（maxlen 可选）
	ControlBool   ControlKind = "bool"
	ControlInt    ControlKind = "int"    // min/max 可选
	ControlSlider ControlKind = "slider" // 滑块：int 语义 + step（编辑器交互）
	ControlSelect ControlKind = "select"
	ControlSafe   ControlKind = "safe"  // CSS 值白名单校验（IsSafeCSSValue）
	ControlText   ControlKind = "text"  // 富文本/长文本（长度上限 maxlen，存原始 HTML 由组件自行处理）
	ControlRegex  ControlKind = "regex" // 正则校验（pattern 来自独立 ctRegex tag）
	ControlURL    ControlKind = "url"   // 链接：协议白名单（http/https/mailto/相对路径/#）
)

// ctTag / ctRegexTag 字段标签：
//
//	`ct:"select,h1,h2,default=h2"` 声明控件类型与参数；
//	`ctRegex:"^[a-z]{2,4}$"` 单独存放正则模式（模式内含逗号，不能走 ct 分片）。
const (
	ctTagName     = "ct"
	ctRegexTagKey = "ctRegex"
)

// Control 单个控件的规范化描述符（由结构体 tag 解析生成，组件作者不手动维护）。
type Control struct {
	Key     string      `json:"key"`
	Kind    ControlKind `json:"kind"`
	Default string      `json:"default,omitempty"`
	Min     int         `json:"min,omitempty"`
	Max     int         `json:"max,omitempty"`
	Step    int         `json:"step,omitempty"` // slider 步进（编辑器交互；默认 1）
	MaxLen  int         `json:"maxLen,omitempty"`
	Options []string    `json:"options,omitempty"` // select 选项
	Pattern string      `json:"pattern,omitempty"` // regex 模式
	// Section 面板分组：content / style / advanced（默认 content）。
	Section string `json:"section,omitempty"`
}

// SortKey 确定性输出键。
func (c Control) SortKey() string { return c.Key }

// ParseControls 从 props 结构体反射扫描 ct tag，生成控件描述符表。
// 排序规则：按字段声明序（结构体字段顺序），保证确定性。
func ParseControls(props any) (controls []Control, err error) {
	t := reflect.TypeOf(props)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup(ctTagName)
		if !ok || strings.TrimSpace(tag) == "" {
			continue
		}
		c, parseErr := parseControlTag(f.Name, tag)
		if parseErr != nil {
			return nil, fmt.Errorf("字段 %s tag 解析失败: %w", f.Name, parseErr)
		}
		// regex 模式来自独立 ctRegex tag（含逗号，无法走 ct 分片）。
		if c.Kind == ControlRegex {
			if pat, ok := f.Tag.Lookup(ctRegexTagKey); ok && pat != "" {
				c.Pattern = pat
			} else {
				return nil, fmt.Errorf("字段 %s: regex 控件必须提供 ctRegex tag", f.Name)
			}
		}
		controls = append(controls, c)
	}
	return controls, nil
}

// parseControlTag 解析单个字段的 ct tag。
func parseControlTag(fieldName, tag string) (c Control, err error) {
	parts := strings.Split(tag, ",")
	if len(parts) == 0 || parts[0] == "" {
		return c, fmt.Errorf("缺少控件类型")
	}
	c.Key = fieldName
	c.Kind = ControlKind(strings.TrimSpace(parts[0]))
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case strings.HasPrefix(part, "default="):
			c.Default = strings.TrimPrefix(part, "default=")
		case strings.HasPrefix(part, "min="):
			if c.Min, err = strconv.Atoi(strings.TrimPrefix(part, "min=")); err != nil {
				return c, fmt.Errorf("min 非法: %s", part)
			}
		case strings.HasPrefix(part, "max="):
			if c.Max, err = strconv.Atoi(strings.TrimPrefix(part, "max=")); err != nil {
				return c, fmt.Errorf("max 非法: %s", part)
			}
		case strings.HasPrefix(part, "maxlen="):
			if c.MaxLen, err = strconv.Atoi(strings.TrimPrefix(part, "maxlen=")); err != nil {
				return c, fmt.Errorf("maxlen 非法: %s", part)
			}
		case strings.HasPrefix(part, "step="):
			if c.Step, err = strconv.Atoi(strings.TrimPrefix(part, "step=")); err != nil || c.Step < 1 {
				return c, fmt.Errorf("step 非法: %s", part)
			}
		case strings.HasPrefix(part, "sec="):
			c.Section = strings.TrimPrefix(part, "sec=")
		default:
			c.Options = append(c.Options, part) // select 选项
		}
	}
	if c.Section == "" {
		c.Section = "content"
	}
	if c.Kind == ControlSelect && len(c.Options) == 0 {
		return c, fmt.Errorf("select 控件必须提供选项")
	}
	return c, nil
}

// ValidateSpec 反射校验 props：按 ct tag 声明的规则逐字段校验。
// nodeID 仅用于错误定位。嵌套结构、复杂关系（互斥/依赖）由组件 Validate 补充。
func ValidateSpec(props any, nodeID string) (err error) {
	controls, err := ParseControls(props)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", nodeID, err)
	}
	v := reflect.ValueOf(props)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	for _, c := range controls {
		fv := v.FieldByName(c.Key)
		if !fv.IsValid() {
			return fmt.Errorf("节点 %s: 字段 %s 不存在", nodeID, c.Key)
		}
		if err = validateControlValue(c, fv, nodeID); err != nil {
			return err
		}
	}
	return nil
}

// validateControlValue 单字段值校验。
func validateControlValue(c Control, fv reflect.Value, nodeID string) (err error) {
	msg := func(detail string, args ...interface{}) error {
		return fmt.Errorf("节点 %s: 字段 %s %s", nodeID, c.Key, fmt.Sprintf(detail, args...))
	}

	switch fv.Kind() {
	case reflect.String:
		s := fv.String()
		if s == "" {
			return nil // 未设置一律放行，默认值由渲染层处理
		}
		if c.MaxLen > 0 && len(s) > c.MaxLen {
			return msg("超长（上限 %d 字符）", c.MaxLen)
		}
		switch c.Kind {
		case ControlSelect:
			found := false
			for _, opt := range c.Options {
				if opt == s {
					found = true
					break
				}
			}
			if !found {
				return msg("值 %q 不在选项内（有效值: %s）", s, strings.Join(c.Options, "/"))
			}
		case ControlSafe:
			if !IsSafeCSSValue(s) {
				return msg("值非法: %q", s)
			}
		case ControlRegex:
			if c.Pattern != "" && !regexp.MustCompile(c.Pattern).MatchString(s) {
				return msg("值不匹配模式: %q", s)
			}
		case ControlURL:
			if !isSafeHrefURL(s) {
				return msg("链接协议非法: %q", s)
			}
		}
	case reflect.Bool:
		// bool 无值域。
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// slider 与 int 值域语义一致（step 仅编辑器交互元数据）。
		n := fv.Int()
		if n == 0 {
			return nil // 零值=未设置，放行（omitempty 语义）
		}
		if c.Min != 0 && n < int64(c.Min) {
			return msg("小于下限 %d: %d", c.Min, n)
		}
		if c.Max != 0 && n > int64(c.Max) {
			return msg("超出上限 %d: %d", c.Max, n)
		}
	}
	return nil
}

// urlSafeRe URL 白名单字符集（禁引号/尖括号/空白；@ 供 mailto）。
var urlSafeRe = regexp.MustCompile(`^[A-Za-z0-9./:?=&%~#+_@-]{1,500}$`)

// isSafeHrefURL 链接协议白名单：http/https/mailto/相对路径/# 锚点。
func isSafeHrefURL(s string) bool {
	if !urlSafeRe.MatchString(s) {
		return false
	}
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "/") {
		return true
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "mailto:")
}

// sectionOrder 面板分组固定输出顺序（content → style → advanced）。
var sectionOrder = map[string]int{"content": 0, "style": 1, "advanced": 2}

// SchemaJSON 生成 Inspector 面板 schema（供编辑器渲染控件表单）。
// 输出按分组（content/style/advanced）→ 字段声明序，确定性排序。
func SchemaJSON(props any) (data []byte, err error) {
	controls, err := ParseControls(props)
	if err != nil {
		return nil, err
	}
	type schemaItem struct {
		Key     string   `json:"key"`
		Kind    string   `json:"kind"`
		Section string   `json:"section,omitempty"`
		Default string   `json:"default,omitempty"`
		Min     int      `json:"min,omitempty"`
		Max     int      `json:"max,omitempty"`
		Step    int      `json:"step,omitempty"`
		MaxLen  int      `json:"maxLen,omitempty"`
		Options []string `json:"options,omitempty"`
	}
	// 按 section 分桶，桶内保持字段声明序。
	buckets := map[string][]schemaItem{}
	for _, c := range controls {
		buckets[c.Section] = append(buckets[c.Section], schemaItem{
			Key: c.Key, Kind: string(c.Kind), Section: c.Section, Default: c.Default,
			Min: c.Min, Max: c.Max, Step: c.Step, MaxLen: c.MaxLen, Options: c.Options,
		})
	}
	var items []schemaItem
	for sec := range sectionOrder {
		items = append(items, buckets[sec]...)
	}
	return json.Marshal(items)
}
