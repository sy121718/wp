package mcp

type Schema struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`

	Properties map[string]Schema `json:"properties,omitempty"`

	Required []string `json:"required,omitempty"`

	Items *Schema `json:"items,omitempty"`

	Enum []string `json:"enum,omitempty"`

	AdditionalProperties *bool `json:"additionalProperties,omitempty"`
}

func noExtraProps() *bool {
	no := false
	return &no
}

func Object(desc string, props map[string]Schema, required ...string) Schema {
	return Schema{
		Type: "object", Description: desc,
		Properties: props, Required: required,
		AdditionalProperties: noExtraProps(),
	}
}

func String(desc string) Schema { return Schema{Type: "string", Description: desc} }

func Integer(desc string) Schema { return Schema{Type: "integer", Description: desc} }

func Boolean(desc string) Schema { return Schema{Type: "boolean", Description: desc} }

func Enum(desc string, values ...string) Schema {
	return Schema{Type: "string", Description: desc, Enum: values}
}

func Array(desc string, items Schema) Schema {
	return Schema{Type: "array", Description: desc, Items: &items}
}
