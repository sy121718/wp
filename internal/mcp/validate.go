package mcp

// validate.go — 参数校验（**安全边界**，不是便利设施）。
//
// 工具参数是模型能直接控制的输入。这里守住三件事：只认声明过的字段、
// 必填必须有、类型必须对。守不住任何一条，模型就能通过参数把任意表达式带进业务查询
//（不变量 4「Binding 不是 Query DSL」的同源判据）。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ArgsError 参数不合规：缺必填 / 未知字段 / 类型不符 / 取值不在白名单。
//
// 它被单独塑造成一个类型，是因为**这类错误是能回给模型的**：模型据此改正参数重试是正常流程。
// 与之相对的是「查库失败」那类系统错误 —— 回给模型只会让它重复同样的调用。
// agent loop 用 errors.As 区分两者，决定「让它重试」还是「直接报错」。
type ArgsError struct {
	Msg string
}

func (e *ArgsError) Error() string { return e.Msg }

// UnknownToolError 工具名不存在（同样是可回给模型的错误：多半是拼错了）。
type UnknownToolError struct{ Name string }

func (e *UnknownToolError) Error() string {
	return fmt.Sprintf("工具 %q 不存在", e.Name)
}

// validate 按 schema 校验原始参数（必须是 JSON 对象）。
//
// 空参数视作 `{}`：零参数工具合法，调用方传 nil 与传 `{}` 应当等价
// （把 nil 判成错误会让每个零参数工具都要在 schema 里写一个假必填项）。
func validate(schema Schema, raw json.RawMessage) error {
	if schema.Type != "object" {
		return &ArgsError{Msg: fmt.Sprintf("工具参数 schema 必须是 object，实为 %q", schema.Type)}
	}
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		body = []byte("{}")
	}
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&fields); err != nil {
		return &ArgsError{Msg: "参数必须是 JSON 对象: " + err.Error()}
	}

	// 1) 未知字段：一律拒绝，而不是忽略。
	// 忽略的失败模式是「模型以为按某维度筛过了、实际没筛」，页面上的数字看起来完全正常。
	for name := range fields {
		if _, ok := schema.Properties[name]; !ok {
			return &ArgsError{Msg: fmt.Sprintf("未知参数 %q（本工具接受：%s）", name, strings.Join(sortedNames(schema.Properties), "、"))}
		}
	}

	// 2) 必填：显式传 null 等同于没传（JSON 里 null 是「没有值」的常见写法）。
	for _, name := range schema.Required {
		val, ok := fields[name]
		if !ok || isJSONNull(val) {
			return &ArgsError{Msg: fmt.Sprintf("缺少必填参数 %q", name)}
		}
	}

	// 3) 类型与取值白名单。
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

// validateValue 校验单个值。
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
		// 这里不需要 isJSONNumberLiteral 守卫：目标是 float64，encoding/json 自己
		// 就会拒掉字符串（只有 json.Number 那个 string-kind 的类型会被字符串骗过去）。
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

// isJSONNull 判断原始值是不是 JSON null。
func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// isJSONNumberLiteral 判断原始值是不是 JSON 数字字面量。
//
// 必须看原始字节，不能靠「unmarshal 到数字类型会不会报错」：json.Number 的底层
// Kind 是 string，`json.Unmarshal([]byte("\"10\""), &n)` 会把**带引号的字符串**
// 当作合法数字装进去 —— 边界在这里漏一个口子，模型就能拿字符串冒充整数。
// 而 service 侧对「字符串 10」与「数字 10」的处理并不相同（前者可能是它自己解析的时间或 id）。
func isJSONNumberLiteral(raw json.RawMessage) bool {
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		return false
	}
	c := body[0]
	return c == '-' || (c >= '0' && c <= '9')
}

// sortedNames 取字段名并排序（错误文案稳定：同一份错误每次输出同一个顺序，
// 日志与测试断言才有意义）。
func sortedNames(props map[string]Schema) []string {
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
