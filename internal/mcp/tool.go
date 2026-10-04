package mcp

// tool.go — 一个「模型可调用工具」的形状。
//
// 形状刻意贴着 MCP（modelcontextprotocol）的 tools/list 条目走：同一份声明要服务两类消费者 ——
// 进程内的 AI 助手（内存调用）与外部客户端（经 /mcp 端点）。两套形状的失败模式是
// 「同一件事在两个入口有不同能力的工具」，而要发现这一点得同时读两处装配代码。

import (
	"bytes"
	"context"
	"encoding/json"

	"go_wp/internal/permission"
)

// Result 一次工具调用的结果。
//
// Text 给模型读（结论性正文），Data 给渲染层与外部客户端用（结构化）。
// 两者都要：只有 Text 时渲染层要重新解析自然语言；只有 Data 时模型看到的是一坨 JSON。
type Result struct {
	Text string
	Data any
}

// Tool 一个工具的声明与执行体。
//
// handler 已被类型擦除（注册表要存异构工具），所以执行体只能经 Invoke 进入 ——
// 调用方拿不到「绕过校验直接调」的入口。
type Tool struct {
	name    string
	title   string
	desc    string
	schema  Schema
	perm    permission.Perm
	handler func(ctx context.Context, args json.RawMessage) (Result, error)
}

// Tool 的只读访问器（声明信息对注册表、管理页与 /mcp 端点都要可见）。

func (t Tool) Name() string                { return t.name }
func (t Tool) Title() string               { return t.title }
func (t Tool) Description() string         { return t.desc }
func (t Tool) Schema() Schema              { return t.schema }
func (t Tool) Permission() permission.Perm { return t.perm }

// Invoke 执行工具：先按 schema 校验参数，再解码进 handler 的类型化入参。
func (t Tool) Invoke(ctx context.Context, args json.RawMessage) (Result, error) {
	return t.handler(ctx, args)
}

// New 声明一个工具。
//
// 泛型入参 Req 的意义：handler 里拿到的是**结构体**，字段名拼错是编译错误。
// 用 map[string]any 的失败模式是「读一个拼错的 key 永远得到零值」——
// 于是查询按空工程 / 空区间跑，返回一个看起来合理但错的数字。
//
// schema 是**唯一**的参数真源：校验按它做，/mcp 客户端的 tools/list 也按它出。
// 不要在 handler 里再判一次「字段是否为空」—— 那会变成第二份必填口径。
func New[Req any](
	name, title, description string,
	perm permission.Perm,
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
