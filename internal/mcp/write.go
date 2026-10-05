package mcp

// write.go — 写工具的通用地基：显式确认 + 幂等键（docs/17 D8 / §P4）。
//
// 只读工具直接 New 就够了；写工具必须走这里，原因是三件事在工具层无法各自解决：
//
//  1. **确认**。工具一旦被调用就执行，而工具层看不到对话 —— 它没有任何办法知道
//     「用户真的点了同意」。「等客户端弹确认框」也不行：外部 /mcp 客户端是否弹框、
//     弹得对不对，都不由我们控制。唯一跨消费者一致的形态是**要求显式参数**：
//     调用方必须在参数里写 `confirm: true`，这个动作会留在调用流水里，可追、可审。
//     它与「用户可以点错」无关 —— 它挡的是「模型自己觉得该写就写了」。
//
//  2. **幂等**。模型会重试（超时、上下文重建、用户重发）。没有幂等键时一次
//     「新建文章」的意图可能落两篇，而两篇都合法、没有任何错误。
//     **幂等键由调用方给**，不在这里生成：生成权在服务端的话，每次重试都会拿到
//     新键，等于没有幂等。同一次意图必须复用同一个值，这件事只有调用方知道。
//
//  3. **顺序**。两项检查必须在**任何业务代码之前**做完，且由地基统一做 ——
//     让每个工具自己记得校验，结果是第一个忘写的工具就成了后门。
//
// 因此这两个参数字段由**地基注入**（见 NewWrite 里对 schema 的改写），
// 而不是让每个工具作者在自己的入参结构体里声明：作者漏声明的失败模式是
// 「这个工具没有确认位」，而它在接口列表里与他人长得一模一样。

import (
	"context"
	"encoding/json"
	"strings"

	"go_wp/internal/permission"
)

// 写工具的两个通用参数名。工具作者不要在自己的入参结构体里重复声明它们。
const (
	WriteArgConfirm = "confirm"
	WriteArgKey     = "idempotencyKey"
)

// IdempotencyStore 记「同一次意图上次的结果」。
//
// 只记**成功**的结果：失败不落库，于是重试能真的重试（否则一次网络抖动会让
// 这个幂等键永久的返回那次失败，而调用方以为重试过了）。
//
// 端口在这里而不是直接依赖某个 model：工具层的可测性靠它 —— 假实现可以让
// 「第二次调用返回上次结果且不碰业务代码」变成一个纯函数断言。
type IdempotencyStore interface {
	// Lookup 取同一次意图上次的结果。ok=false 表示没记录过（或已过期）。
	Lookup(ctx context.Context, tool, key string) (res Result, ok bool, err error)
	// Save 记下这次成功的结果。
	Save(ctx context.Context, tool, key string, res Result) error
}

// NewWrite 构造一个写工具（带确认位与幂等键）。
//
// schema 传的是**业务字段**那部分；confirm 与 idempotencyKey 由这里追加进
// properties 与 required —— 调用方在自己的结构体里**不要**再声明它们，
// 地基会先解析出来再解业务参数（见下面的 handler）。
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
			// ① 确认位。文案要告诉模型**怎么改**，而不只是「不允许」——
			// 只说「不允许」时它会换个说法再试，直到撞上这个错误若干次。
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

			// ② 校验业务字段（在碰业务代码之前）。
			if err := validate(schema, args); err != nil {
				return Result{}, err
			}

			// ③ 幂等：命中就返回上次结果，**不碰 handler**。
			if store != nil {
				prev, ok, err := store.Lookup(ctx, name, key)
				if err != nil {
					return Result{}, err
				}
				if ok {
					// 提示是给模型看的：它需要知道「这次没有真的再写一遍」，
					// 否则会把返回值当成「又改了一次」而向用户汇报两次改动。
					prev.Text = prev.Text + "\n（这个幂等键此前已经成功执行过，本次没有重复写入。）"
					return prev, nil
				}
			}

			// ④ 解业务参数。
			var req Req
			if err := json.Unmarshal(args, &req); err != nil {
				return Result{}, &ArgsError{Msg: "参数解析失败：" + err.Error()}
			}
			res, err := handler(ctx, req)
			if err != nil {
				// 失败不落幂等：让重试能真的重试。
				return Result{}, err
			}
			if store != nil {
				if err := store.Save(ctx, name, key, res); err != nil {
					// 结果已经写成功了，只是幂等记录没落上。**不能**把它当失败返回：
					// 那会让调用方以为没写成功而重试，反而写出第二条。
					return res, nil
				}
			}
			return res, nil
		},
	}
}
