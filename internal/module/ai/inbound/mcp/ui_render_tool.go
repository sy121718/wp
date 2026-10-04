// Package aimcp 声明 ai 模块自己的模型可调用工具。
//
// 目录约定（docs/17 决策 D1）：工具由**拥有数据的模块**在自己 inbound/mcp 下声明。
// ai 模块不拥有业务表，这里只有一类工具：把模型的展示意图变成受校验的「渲染指令」。
package aimcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	"go_wp/internal/permission"
	"go_wp/internal/uispec"
)

// ui_render 的工具名。
//
// 会话层按这个名字认「需要渲染的结果」（`ai_session_chat.go` 的 runTool）——
// 两处写死同一个字面量，改名时漏一处会让块静默不渲染，所以两边都有测试钉住。
const ToolNameUIRender = "ui_render"

// UIRenderTools ai 模块提供的工具。
func UIRenderTools() []mcp.Tool {
	return []mcp.Tool{uiRenderTool()}
}

// uiRenderArgs ui_render 的入参。
type uiRenderArgs struct {
	// Spec 展示指令的 JSON 文本（不是嵌套对象：模型是按「一段 JSON」的形态产出它的，
	// 用字符串可以原样交给 uispec.Parse，不必在这里再定义一层会与 uispec 漂移的结构）。
	Spec string `json:"spec"`
}

// uiRenderTool 声明 ui_render。
func uiRenderTool() mcp.Tool {
	return mcp.New("ui_render", "渲染展示组件",
		"把一段展示指令渲染成指标卡 / 表格 / 列表 / 手风琴放进回答里。**只描述结构，不要写数字**："+
			"每个积木用 type（stat / table / list / accordion）+ source（数据源名字，就是本清单里其它工具的名字）"+
			"+ 可选 params（该数据源的参数，字符串值）+ 可选 limit。数字由服务端按 source 查出来，"+
			"你写进去的任何数字都会被拒绝。一次最多 8 个积木；source 必须是真实存在的工具名。",
		permission.AIChat,
		mcp.Object("展示指令", map[string]mcp.Schema{
			"spec": mcp.String(`展示指令 JSON，形如 {"blocks":[{"type":"table","source":"orders_top_products","params":{"projectId":"...","from":"2026-10-01","to":"2026-10-07"},"limit":5}]}`),
		}, "spec"),
		func(_ context.Context, args uiRenderArgs) (mcp.Result, error) {
			spec, err := uispec.Parse([]byte(args.Spec))
			if err != nil {
				// 参数类错误回给模型**原样**（ArgsError 的文案会被 failureText 透出去）：
				// 字段名与原因模型自己能看懂，据此改正并重试是它最该做的事。
				var rej *uispec.Reject
				if errors.As(err, &rej) {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf(
						"展示指令被拒绝（%s，位置 %s）：%s", rej.Kind, orRoot(rej.Where), rej.Msg)}
				}
				return mcp.Result{}, &mcp.ArgsError{Msg: err.Error()}
			}
			return mcp.Result{Text: uiRenderText(spec), Data: spec}, nil
		})
}

// orRoot 位置为空时给一个可读的占位（模型拿这句话要去改它的输出，空位置等于没给）。
func orRoot(where string) string {
	if strings.TrimSpace(where) == "" {
		return "根"
	}
	return where
}

// uiRenderText 回给模型的一句话。
//
// 刻意**不**把查到的数字写进这句话：那是渲染层的事，而这句话会进模型的上下文 ——
// 数字一旦进了上下文，后续轮次里模型就会把它当成自己已知的事实往外说，
// 于是「数字只来自查询」这条约束在第二轮就失效了。
func uiRenderText(spec *uispec.Spec) string {
	if spec == nil || len(spec.Blocks) == 0 {
		return "展示指令已接受（没有积木，只有文字）。"
	}
	byType := map[string]int{}
	for _, b := range spec.Blocks {
		byType[b.Type]++
	}
	parts := make([]string, 0, len(byType))
	for _, t := range []string{uispec.TypeStat, uispec.TypeTable, uispec.TypeList, uispec.TypeAccordion} {
		if n := byType[t]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d 个", t, n))
		}
	}
	return "展示指令已接受（共 " + fmt.Sprint(len(spec.Blocks)) + " 个积木：" +
		strings.Join(parts, "、") + "）。数据由服务端按 source 查询后渲染，不要在本轮回答里复述具体数字。"
}
