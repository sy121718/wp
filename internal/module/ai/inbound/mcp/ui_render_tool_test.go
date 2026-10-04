package aimcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	"go_wp/internal/uispec"
)

// invokeUI 调一次 ui_render。
func invokeUI(t *testing.T, spec string) (mcp.Result, error) {
	t.Helper()
	tools := UIRenderTools()
	if len(tools) != 1 {
		t.Fatalf("工具数不对: %d", len(tools))
	}
	if tools[0].Name() != ToolNameUIRender {
		t.Fatalf("工具名不对: %s", tools[0].Name())
	}
	raw, err := json.Marshal(map[string]string{"spec": spec})
	if err != nil {
		t.Fatal(err)
	}
	return tools[0].Invoke(context.Background(), raw)
}

// TestUIRenderAcceptsSpec 合法 spec 通过，且 Data 是**解析后的 spec**（不是原文）。
//
// 会话层靠 `res.Data.(*uispec.Spec)` 认出「这是一条展示指令」并据此取数；
// 换成字符串就没有这个类型信号了 —— 而按工具名判会退化成两处硬编码同一个字面量。
func TestUIRenderAcceptsSpec(t *testing.T) {
	res, err := invokeUI(t, `{"text":"见下表","blocks":[{"type":"table","source":"orders_top_products","limit":5}]}`)
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	spec, ok := res.Data.(*uispec.Spec)
	if !ok {
		t.Fatalf("Data 应是 *uispec.Spec，实际 %T", res.Data)
	}
	if len(spec.Blocks) != 1 || spec.Blocks[0].Source != "orders_top_products" {
		t.Fatalf("spec 没解析对: %+v", spec.Blocks)
	}
	if !strings.Contains(res.Text, "已接受") {
		t.Errorf("回给模型的文案不对: %q", res.Text)
	}
	// 文案里**不能**出现数字以外的业务值（这里断言的是「没有把 source 的参数回显成结论」的弱形态）
	if strings.Contains(res.Text, "orders_top_products 的值") {
		t.Errorf("不该复述数据: %q", res.Text)
	}
}

// TestUIRenderRejectsBadSpec 非法 spec 变成 ArgsError（模型能据此改正并重试）。
func TestUIRenderRejectsBadSpec(t *testing.T) {
	cases := []struct{ name, spec, wantKind string }{
		{"未知组件类型", `{"blocks":[{"type":"chart","source":"orders_daily"}]}`, "unknown_type"},
		{"偷带数据", `{"blocks":[{"type":"table","source":"x","rows":[["a"]]}]}`, "unknown_field"},
		{"缺数据源", `{"blocks":[{"type":"table"}]}`, "missing_source"},
		{"不是 JSON", `{`, "malformed"},
		{"空", ``, "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := invokeUI(t, tc.spec)
			if err == nil {
				t.Fatalf("应被拒绝")
			}
			var argsErr *mcp.ArgsError
			if !errors.As(err, &argsErr) {
				t.Fatalf("应是 ArgsError（模型能据此改正），实际 %T: %v", err, err)
			}
			if !strings.Contains(argsErr.Msg, tc.wantKind) {
				t.Errorf("文案里应带原因标签 %q: %s", tc.wantKind, argsErr.Msg)
			}
		})
	}
}

// TestUIRenderTextIsEmptySafe 只有文字（没有积木）也是合法回答。
func TestUIRenderTextIsEmptySafe(t *testing.T) {
	res, err := invokeUI(t, `{"text":"这周没有订单。"}`)
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if !strings.Contains(res.Text, "没有积木") {
		t.Errorf("文案应说明没有积木: %q", res.Text)
	}
}
