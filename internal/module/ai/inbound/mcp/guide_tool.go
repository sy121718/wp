package aimcp

// guide_tool.go — `guide` 工具：按名字取一篇领域手册的全文。
//
// 为什么是工具而不是全塞进提示词：手册加起来有明显的体积，而一次对话通常只碰到一两个领域。
// 全量进前缀会让**每轮**都为所有领域付费，而前缀缓存只在内容不变时才命中 ——
// 手册恰恰是会被频繁修订的那类文本。所以：目录（几行）进稳定前缀，正文按需取。
//
// 这个工具是**只读且零依赖**的：它读的是编译进二进制的文本（internal/module/ai/prompt），
// 不碰数据库、不碰任何模块端口。所以它不需要注入任何东西，也不会因为某个模块没装配而消失。

import (
	"context"
	"fmt"
	"strings"

	aiprompt "go_wp/internal/module/ai/prompt"
	"go_wp/internal/mcp"
	"go_wp/internal/permission"
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
