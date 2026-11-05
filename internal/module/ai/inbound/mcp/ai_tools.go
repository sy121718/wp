package aimcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	aienums "go_wp/internal/module/ai/enums"
	aiprompt "go_wp/internal/module/ai/prompt"
	"go_wp/pkg/uispec"
)

const ToolNameGuide = "guide"

func GuideTools() []mcp.Tool {
	return []mcp.Tool{guideTool()}
}

type guideArgs struct {
	Topic string `json:"topic"`
}

func guideTool() mcp.Tool {
	return mcp.New(ToolNameGuide, "取领域手册",
		"按名字取一篇领域手册的全文，里面有该领域的口径（哪些订单算消费、金额单位、"+
			"新客/复购/留存怎么定义）与注意事项。可用名字见系统提示里的手册目录。"+
			"拿不准口径、或者要回答涉及金额与统计口径的问题时先取一篇，不要凭常识答。",

		aienums.AIChat,
		mcp.Object("手册名", map[string]mcp.Schema{
			"topic": mcp.String("手册名（如 sales / customers / traffic）"),
		}, "topic"),
		func(_ context.Context, args guideArgs) (mcp.Result, error) {
			text, ok := aiprompt.Manual(args.Topic)
			if !ok {

				return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf(
					"没有名为 %q 的手册。可用的是：%s", strings.TrimSpace(args.Topic), availableTopics())}
			}
			return mcp.Result{Text: text}, nil
		})
}

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

func knownSourceHints() []string {
	return []string{"orders_summary", "orders_daily", "orders_top_products", "orders_status_counts",
		"product_find", "content_find", "media_find"}
}

const ToolNameUIRender = "ui_render"

func UIRenderTools(known func(name string) bool) []mcp.Tool {
	return []mcp.Tool{uiRenderTool(known)}
}

type uiRenderArgs struct {
	Spec string `json:"spec"`
}

func uiRenderTool(known func(name string) bool) mcp.Tool {
	return mcp.New("ui_render", "渲染展示组件",
		"把一段展示指令渲染成指标卡 / 表格 / 列表 / 手风琴放进回答里。**只描述结构，不要写数字**："+
			"每个积木用 type（stat / table / list / accordion）+ source（数据源名字，就是本清单里其它工具的名字）"+
			"+ 可选 params（该数据源的参数，字符串值）+ 可选 limit。数字由服务端按 source 查出来，"+
			"你写进去的任何数字都会被拒绝。一次最多 8 个积木；source 必须是真实存在的工具名。",
		aienums.AIChat,
		mcp.Object("展示指令", map[string]mcp.Schema{
			"spec": mcp.String(`展示指令 JSON，形如 {"blocks":[{"type":"table","source":"orders_top_products","params":{"projectId":"...","from":"2026-10-01","to":"2026-10-07"},"limit":5}]}`),
		}, "spec"),
		func(_ context.Context, args uiRenderArgs) (mcp.Result, error) {
			spec, err := uispec.Parse([]byte(args.Spec))
			if err != nil {

				var rej *uispec.Reject
				if errors.As(err, &rej) {
					return mcp.Result{}, &mcp.ArgsError{Msg: fmt.Sprintf(
						"展示指令被拒绝（%s，位置 %s）：%s", rej.Kind, orRoot(rej.Where), rej.Msg)}
				}
				return mcp.Result{}, &mcp.ArgsError{Msg: err.Error()}
			}

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

func orRoot(where string) string {
	if strings.TrimSpace(where) == "" {
		return "根"
	}
	return where
}

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
