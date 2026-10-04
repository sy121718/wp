package mcp

// schema.go — 工具参数的 schema（JSON Schema 的**受限子集**）。
//
// 只支持工具参数真正用得到的形状：对象 + 基本类型 + 数组 + 枚举。
//
// **为什么不做完整 JSON Schema**：参数校验是安全边界（不变量 4「Binding 不是 Query DSL」
// 的同源判据）—— 支持得越多，能塞进参数里的表达式就越多。这里只回答三件事：
// 有哪些字段、各是什么类型、哪些必填；**未知字段一律拒绝**（additionalProperties: false），
// 模型不会因为「多传了一个 sortExpr」就获得排序表达式的能力。

// Schema 一个参数（或对象整体）的形状。
//
// 字段顺序与命名跟 MCP 客户端读到的 JSON 一致，便于对照。
type Schema struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	// Properties 仅 type=object 时有值。
	Properties map[string]Schema `json:"properties,omitempty"`
	// Required 仅 type=object 时有值（按声明顺序输出，便于人工对照）。
	Required []string `json:"required,omitempty"`
	// Items 仅 type=array 时有值。
	Items *Schema `json:"items,omitempty"`
	// Enum 仅 type=string 时有值（白名单取值）。
	Enum []string `json:"enum,omitempty"`
	// AdditionalProperties 恒为 false 且只在 object 上输出：多传的字段一律拒绝。
	// 用指针是刻意的 —— 值类型的 false 会被 omitempty 吃掉，而「没写」
	// 在 JSON Schema 里意味着**允许**任意字段，正好与这里要的相反。
	AdditionalProperties *bool `json:"additionalProperties,omitempty"`
}

// noExtraProps 返回指向 false 的指针（AdditionalProperties 的唯一取值）。
func noExtraProps() *bool {
	no := false
	return &no
}

// Object 构造一个对象 schema。required 是必填字段名（顺序即输出顺序）。
//
// 允许 required 为空：零参数工具合法（例如「当前站点概览」）。
func Object(desc string, props map[string]Schema, required ...string) Schema {
	return Schema{
		Type: "object", Description: desc,
		Properties: props, Required: required,
		AdditionalProperties: noExtraProps(),
	}
}

// String 构造一个字符串字段。
func String(desc string) Schema { return Schema{Type: "string", Description: desc} }

// Integer 构造一个整数字段。
func Integer(desc string) Schema { return Schema{Type: "integer", Description: desc} }

// Boolean 构造一个布尔字段。
func Boolean(desc string) Schema { return Schema{Type: "boolean", Description: desc} }

// Enum 构造一个受限取值的字符串字段（取值白名单）。
//
// 传进来的 values 就是全部合法值：模型给出白名单外的取值会被**拒绝**，
// 而不是被静默忽略 —— 静默忽略的失败模式是「用户以为按某维度筛了、其实没筛」。
func Enum(desc string, values ...string) Schema {
	return Schema{Type: "string", Description: desc, Enum: values}
}

// Array 构造一个数组字段。
func Array(desc string, items Schema) Schema {
	return Schema{Type: "array", Description: desc, Items: &items}
}
