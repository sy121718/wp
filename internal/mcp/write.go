package mcp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/permission"
)

const (
	WriteArgConfirm = "confirm"
	WriteArgKey     = "idempotencyKey"
)

type IdempotencyStore interface {
	Lookup(ctx context.Context, tool, key string) (res Result, ok bool, err error)

	Save(ctx context.Context, tool, key string, res Result) error
}

func NewWrite[Req any](
	name, title, description string,
	perm permission.Perm,
	schema Schema,
	store IdempotencyStore,
	handler func(context.Context, Req) (Result, error),
) Tool {
	if schema.Properties == nil {
		schema.Properties = map[string]Schema{}
	}
	schema.Properties[WriteArgConfirm] = Boolean(
		"写操作确认位：必须显式设为 true。这不是形式 —— 它让「这次改动是有人要的」" +
			"在调用流水里留下证据；缺省或 false 时本工具**不会执行任何写操作**。")
	schema.Properties[WriteArgKey] = String(
		"幂等键：同一次意图的所有重试必须复用同一个值（建议用 uuid）。" +
			"服务端按（工具名 + 这个值）记住上次成功的结果；重试时直接返回上次结果，不再写第二次。" +
			"**换一个新的键 = 表达一次新的意图**，所以不要为了「再试一次」而换键。")
	schema.Required = append(append([]string{}, schema.Required...), WriteArgConfirm, WriteArgKey)

	return Tool{
		name: name, title: title, desc: description, schema: schema, perm: perm,
		handler: func(ctx context.Context, args json.RawMessage) (Result, error) {
			var head struct {
				Confirm bool   `json:"confirm"`
				Key     string `json:"idempotencyKey"`
			}
			if err := json.Unmarshal(args, &head); err != nil {
				return Result{}, &ArgsError{Msg: "参数不是合法的 JSON 对象：" + err.Error()}
			}

			if !head.Confirm {
				return Result{}, &ArgsError{Msg: "这是写操作，需要显式确认：" +
					"把参数 confirm 设为 true 后重新调用。" +
					"在确认之前不要替用户做决定 —— 如果需要用户拍板，先把要改什么说清楚。"}
			}
			key := strings.TrimSpace(head.Key)
			if key == "" {
				return Result{}, &ArgsError{Msg: "缺少 idempotencyKey：" +
					"给它一个 uuid（同一次意图的重试要复用同一个值），否则重试会重复写入。"}
			}

			if err := validate(schema, args); err != nil {
				return Result{}, err
			}

			if store != nil {
				prev, ok, err := store.Lookup(ctx, name, key)
				if err != nil {
					return Result{}, err
				}
				if ok {

					prev.Text = prev.Text + "\n（这个幂等键此前已经成功执行过，本次没有重复写入。）"
					return prev, nil
				}
			}

			var req Req
			if err := json.Unmarshal(args, &req); err != nil {
				return Result{}, &ArgsError{Msg: "参数解析失败：" + err.Error()}
			}
			res, err := handler(ctx, req)
			if err != nil {

				return Result{}, err
			}
			if store != nil {
				if err := store.Save(ctx, name, key, res); err != nil {

					return res, nil
				}
			}
			return res, nil
		},
	}
}
