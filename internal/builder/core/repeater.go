package core

import (
	"fmt"
	"reflect"
	"strings"
)

type AlignedRepeaterSpec struct {
	Type     string          `json:"type"`
	AlignKey string          `json:"alignKey"`
	Field    string          `json:"field"`
	Noun     string          `json:"noun"`
	Label    string          `json:"label"`
	AddText  string          `json:"addText"`
	Extra    []RepeaterExtra `json:"extra,omitempty"`
}

type RepeaterExtra struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type AlignedRepeaterProvider interface {
	AlignedRepeater() AlignedRepeaterSpec
}

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
