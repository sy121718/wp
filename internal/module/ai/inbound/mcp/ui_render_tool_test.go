package aimcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	"go_wp/pkg/uispec"
)

func knownSourcesForTest(name string) bool {
	switch name {
	case "orders_summary", "orders_daily", "orders_top_products", "orders_status_counts",
		"product_find", "content_find", "media_find":
		return true
	}
	return false
}

func invokeUI(t *testing.T, spec string) (mcp.Result, error) {
	t.Helper()
	tools := UIRenderTools(knownSourcesForTest)
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

	if strings.Contains(res.Text, "orders_top_products 的值") {
		t.Errorf("不该复述数据: %q", res.Text)
	}
}

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

func TestUIRenderTextIsEmptySafe(t *testing.T) {
	res, err := invokeUI(t, `{"text":"这周没有订单。"}`)
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if !strings.Contains(res.Text, "没有积木") {
		t.Errorf("文案应说明没有积木: %q", res.Text)
	}
}

func TestUIRenderRejectsUnknownSource(t *testing.T) {
	_, err := invokeUI(t, `{"blocks":[{"type":"table","source":"orders_range_summary"}]}`)
	if err == nil {
		t.Fatal("不存在的 source 应当被拒绝")
	}
	if !strings.Contains(err.Error(), "orders_range_summary") {
		t.Fatalf("错误信息要指明是哪个名字不对：%v", err)
	}

	if !strings.Contains(err.Error(), "orders_summary") {
		t.Fatalf("错误信息要给出可用的一批工具名：%v", err)
	}
}

func TestUIRenderSkipsNonSourceTools(t *testing.T) {
	for _, src := range []string{ToolNameUIRender, ToolNameGuide} {
		if _, err := invokeUI(t, `{"blocks":[{"type":"list","source":"`+src+`"}]}`); err != nil {
			t.Fatalf("source=%s 是工具名（只是不是数据源），不应被判成「不存在」：%v", src, err)
		}
	}
}
