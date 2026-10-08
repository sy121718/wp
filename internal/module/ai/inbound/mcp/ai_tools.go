package aimcp

// 为什么是工具而不是全塞进提示词：手册加起来有明显的体积，而一次对话通常只碰到一两个领域。
// 全量进前缀会让**每轮**都为所有领域付费，而前缀缓存只在内容不变时才命中 ——
// 手册恰恰是会被频繁修订的那类文本。所以：目录（几行）进稳定前缀，正文按需取。
//
// 这个工具是**只读且零依赖**的：它读的是编译进二进制的文本（internal/module/ai/prompt），
// 不碰数据库、不碰任何模块端口。所以它不需要注入任何东西，也不会因为某个模块没装配而消失。

// Package aimcp 声明 ai 模块自己的模型可调用工具。
//
// 目录约定（docs/17 决策 D1）：工具由**拥有数据的模块**在自己 inbound/mcp 下声明。
// ai 模块不拥有业务表，这里只有一类工具：把模型的展示意图变成受校验的「渲染指令」。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	aiprompt "go_wp/internal/module/ai/prompt"
	"go_wp/internal/permission"
	"go_wp/internal/uispec"
)

// ToolNameGuide 工具名（数据源清单里也用它）。
const ToolNameGuide = "guide"

// GuideTools 返回 guide 工具集。
func GuideTools() []mcp.Tool {
	return []mcp.Tool{guideTool()}
}

// guideArgs guide 的入参。
type guideArgs struct {
	// Topic 手册名（目录里列出的那些名字之一）。
	Topic string `json:"topic"`
}

func guideTool() mcp.Tool {
	return mcp.New(ToolNameGuide, "取领域手册",
		"按名字取一篇领域手册的全文，里面有该领域的口径（哪些订单算消费、金额单位、"+
			"新客/复购/留存怎么定义）与注意事项。可用名字见系统提示里的手册目录。"+
			"拿不准口径、或者要回答涉及金额与统计口径的问题时先取一篇，不要凭常识答。",
		// 与其它 AI 工具同一权限点：手册本身不含任何业务数据，
		// 但「能提问」与「能读手册」应该一起成立，否则会出现「能查数但读不到口径」的怪状态。
		permission.AIChat,
		mcp.Object("手册名", map[string]mcp.Schema{
			"topic": mcp.String("手册名（如 sales / customers / traffic）"),
		}, "topic"),
		func(_ context.Context, args guideArgs) (mcp.Result, error) {
			text, ok := aiprompt.Manual(args.Topic)
			if !ok {
				// 报错而不是回空串：回空串模型会当成「这个领域没有口径」，
				// 于是按自己的常识回答 —— 而那正是这个工具存在的理由。
				return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf(
					"没有名为 %q 的手册。可用的是：%s", strings.TrimSpace(args.Topic), availableTopics())}
			}
			return mcp.Result{Text: text}, nil
		})
}

// availableTopics 目录里的名字，用顿号连起来（错误信息里要用它把模型引回正轨）。
func availableTopics() string {
	topics := aiprompt.ManualTopics()
	if len(topics) == 0 {
		return "（当前没有任何手册）"
	}
	names := make([]string, 0, len(topics))
	for _, t := range topics {
		names = append(names, t.Name)
	}
	return strings.Join(names, "、")
}

// unknownSource 找出第一个不是已注册工具的 source（没有则回空串）。
//
// 跳过 ui_render 自身与 guide：两者都不是数据源（前者是展示指令的入口、
// 后者是手册查询），拿它们当 source 只会渲染出一片空白。
func unknownSource(spec *uispec.Spec, known func(string) bool) string {
	for _, b := range spec.Blocks {
		src := strings.TrimSpace(b.Source)
		if src == "" || src == ToolNameUIRender || src == ToolNameGuide {
			continue
		}
		if !known(src) {
			return src
		}
	}
	return ""
}

// knownSourceHints 回给模型的候选数据源（它据此改名重试，比一句「不存在」有用得多）。
func knownSourceHints() []string {
	return []string{"orders_summary", "orders_daily", "orders_top_products", "orders_status_counts",
		"product_find", "content_find", "media_find"}
}

// ui_render 的工具名。
//
// 会话层按这个名字认「需要渲染的结果」（`ai_session.go` 的 runTool）——
// 两处写死同一个字面量，改名时漏一处会让块静默不渲染，所以两边都有测试钉住。
const ToolNameUIRender = "ui_render"

// UIRenderTools ai 模块提供的工具。
//
// known 用来校验每个积木的 source 是不是**真实存在的已注册工具**（传 nil 则不校验，
// 只保留字符集检查）。它必须是一个**运行时查询**而不是一份快照：本函数在装配期被调用，
// 而那一刻其它模块的工具可能还没注册完 —— 拿快照会把一大半合法数据源误判成不存在。
func UIRenderTools(known func(name string) bool) []mcp.Tool {
	return []mcp.Tool{uiRenderTool(known)}
}

// uiRenderArgs ui_render 的入参。
type uiRenderArgs struct {
	// Spec 展示指令的 JSON 文本（不是嵌套对象：模型是按「一段 JSON」的形态产出它的，
	// 用字符串可以原样交给 uispec.Parse，不必在这里再定义一层会与 uispec 漂移的结构）。
	Spec string `json:"spec"`
}

// uiRenderTool 声明 ui_render。
func uiRenderTool(known func(name string) bool) mcp.Tool {
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
			// source 必须真的是一个已注册工具。
			//
			// 声明期只校验了名字的**字符集**（uispec.validate 的长度与非法字符），
			// 于是模型写一个不存在的名字（比如把 orders_range_summary 当成 orders_summary）
			// 时这里会回「展示指令已接受」，它在下一轮就按「已经展示给用户了」继续编话 ——
			// 而真正渲染时只有一句软失败提示，不会让任何人发现整个回答是建立在幻觉上的。
			if known != nil {
				if bad := unknownSource(spec, known); bad != "" {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf(
						"数据源 %q 不是已注册的工具名。source 只能填工具清单里的名字；"+
							"可用的一批是：%s", bad, strings.Join(knownSourceHints(), "、"))}
				}
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
