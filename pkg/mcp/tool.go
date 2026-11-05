package mcp

import (
	"bytes"
	"context"
	json "go_wp/pkg/jsonx"

)

type Result struct {
	Text string
	Data any
}

type Tool struct {
	name    string
	title   string
	desc    string
	schema  Schema
	perm    string
	handler func(ctx context.Context, args json.RawMessage) (Result, error)
}

func (t Tool) Name() string                { return t.name }
func (t Tool) Title() string               { return t.title }
func (t Tool) Description() string         { return t.desc }
func (t Tool) Schema() Schema              { return t.schema }
func (t Tool) Permission() string { return t.perm }

func (t Tool) SchemaJSON() (json.RawMessage, error) {
	return json.Marshal(t.schema)
}

func (t Tool) Invoke(ctx context.Context, args json.RawMessage) (Result, error) {
	return t.handler(ctx, args)
}

func New[Req any](
	name, title, description string,
	perm string,
	schema Schema,
	handler func(context.Context, Req) (Result, error),
) Tool {
	return Tool{
		name: name, title: title, desc: description, schema: schema, perm: perm,
		handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			if err := validate(schema, args); err != nil {
				return Result{}, err
			}
			var req Req
			if body := bytes.TrimSpace(args); len(body) > 0 {
				if err := json.Unmarshal(body, &req); err != nil {
					return Result{}, &ArgsError{Msg: "参数无法解码: " + err.Error()}
				}
			}
			return handler(ctx, req)
		},
	}
}
