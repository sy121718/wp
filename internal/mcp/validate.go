package mcp

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
)

type ArgsError struct {
	Msg string
}

func (e *ArgsError) Error() string { return e.Msg }

type UnknownToolError struct{ Name string }

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("工具 %q 不存在", e.Name)
}

func validate(schema Schema, raw json.RawMessage) error {
	if schema.Type != "object" {
		return &ArgsError{Msg: fmt.Sprintf("工具参数 schema 必须是 object，实为 %q", schema.Type)}
	}
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		body = []byte("{}")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return &ArgsError{Msg: "参数必须是 JSON 对象: " + err.Error()}
	}

	for name := range fields {
		if _, ok := schema.Properties[name]; !ok {
			return &ArgsError{Msg: fmt.Sprintf("未知参数 %q（本工具接受：%s）", name, strings.Join(sortedNames(schema.Properties), "、"))}
		}
	}

	for _, name := range schema.Required {
		val, ok := fields[name]
		if !ok || isJSONNull(val) {
			return &ArgsError{Msg: fmt.Sprintf("缺少必填参数 %q", name)}
		}
	}

	for name, val := range fields {
		if isJSONNull(val) {
			continue
		}
		prop := schema.Properties[name]
		if err := validateValue(prop, val); err != nil {
			return &ArgsError{Msg: fmt.Sprintf("参数 %q %s", name, err.Error())}
		}
	}
	return nil
}

func validateValue(schema Schema, raw json.RawMessage) error {
	switch schema.Type {
	case "string":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return errors.New("必须是字符串")
		}
		if len(schema.Enum) == 0 {
			return nil
		}
		for _, allowed := range schema.Enum {
			if allowed == s {
				return nil
			}
		}
		return fmt.Errorf("取值必须是 %s 之一，实得 %q", strings.Join(schema.Enum, " / "), s)
	case "integer":
		if !isJSONNumberLiteral(raw) {
			return errors.New("必须是整数")
		}
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return errors.New("必须是整数")
		}
		if _, err := n.Int64(); err != nil {
			return errors.New("必须是整数（不接受小数）")
		}
		return nil
	case "number":

		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return errors.New("必须是数字")
		}
		return nil
	case "boolean":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return errors.New("必须是布尔值")
		}
		return nil
	case "array":
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return errors.New("必须是数组")
		}
		if schema.Items == nil {
			return nil
		}
		for i, item := range items {
			if err := validateValue(*schema.Items, item); err != nil {
				return fmt.Errorf("第 %d 项 %s", i+1, err.Error())
			}
		}
		return nil
	case "object":
		return validate(schema, raw)
	default:
		return fmt.Errorf("schema 类型 %q 不受支持", schema.Type)
	}
}

func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func isJSONNumberLiteral(raw json.RawMessage) bool {
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		return false
	}
	c := body[0]
	return c == '-' || (c >= '0' && c <= '9')
}

func sortedNames(props map[string]Schema) []string {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
