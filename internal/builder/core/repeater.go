package core

import (
	"fmt"
	"reflect"
	"strings"
)

// AlignedRepeaterSpec 声明「属性数组与 children 一一对应」的编辑契约。
// 组件就近声明；注册时核对真实 Props JSON 字段，服务端面板与生成的 JS 共用它。
// 普通数组和递归数组不属于这个契约。
type AlignedRepeaterSpec struct {
	Type     string          `json:"type"`
	AlignKey string          `json:"alignKey"`
	Field    string          `json:"field"`
	Noun     string          `json:"noun"`
	Label    string          `json:"label"`
	AddText  string          `json:"addText"`
	Extra    []RepeaterExtra `json:"extra,omitempty"`
}

// RepeaterExtra 对齐条目上的布尔控件。
type RepeaterExtra struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// AlignedRepeaterProvider 是内置组件的可选编辑能力，不改变文档与产物协议。
type AlignedRepeaterProvider interface {
	AlignedRepeater() AlignedRepeaterSpec
}

// ValidateAlignedRepeater 使错误的数组键、文案键及附加字段在注册期失败。
// 这里核对 Go 类型，不能以第二份字符串表作为校验依据。
func ValidateAlignedRepeater(props any, spec AlignedRepeaterSpec) error {
	t := reflect.TypeOf(props)
	if t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return fmt.Errorf("对齐面板必须提供结构体 PropsSpec")
	}
	if spec.Noun == "" || spec.Label == "" || spec.AddText == "" {
		return fmt.Errorf("对齐面板的条目名、字段名和添加文案不能为空")
	}
	array := repeaterJSONField(t, spec.AlignKey)
	if array == nil || array.Kind() != reflect.Slice || array.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("props.%s 必须是结构体数组", spec.AlignKey)
	}
	entry := array.Elem()
	field := repeaterJSONField(entry, spec.Field)
	if field == nil || field.Kind() != reflect.String {
		return fmt.Errorf("props.%s[].%s 必须是字符串字段", spec.AlignKey, spec.Field)
	}
	seen := map[string]bool{spec.Field: true}
	for _, extra := range spec.Extra {
		if seen[extra.Key] || extra.Label == "" {
			return fmt.Errorf("附加字段 %s 重复或缺少名称", extra.Key)
		}
		seen[extra.Key] = true
		field := repeaterJSONField(entry, extra.Key)
		if field == nil || field.Kind() != reflect.Bool {
			return fmt.Errorf("props.%s[].%s 必须是布尔字段", spec.AlignKey, extra.Key)
		}
	}
	return nil
}

func repeaterJSONField(t reflect.Type, key string) reflect.Type {
	if key == "" || key == "-" {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.IsExported() && strings.Split(f.Tag.Get("json"), ",")[0] == key {
			return f.Type
		}
	}
	return nil
}

// AlignedRepeaterFor 获取组件声明；不支持该能力时返回 nil。
func AlignedRepeaterFor(typeName string) *AlignedRepeaterSpec {
	c, ok := registry[typeName]
	if !ok {
		return nil
	}
	p, ok := c.(AlignedRepeaterProvider)
	if !ok {
		return nil
	}
	spec := p.AlignedRepeater()
	spec.Type = typeName
	return &spec
}
