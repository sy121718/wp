package aimcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/mcp"
)

func invokeGuide(t *testing.T, topic string) (mcp.Result, error) {
	t.Helper()
	tools := GuideTools()
	if len(tools) != 1 {
		t.Fatalf("工具数不对: %d", len(tools))
	}
	if tools[0].Name() != ToolNameGuide {
		t.Fatalf("工具名不对: %s", tools[0].Name())
	}
	raw, err := json.Marshal(map[string]string{"topic": topic})
	if err != nil {
		t.Fatal(err)
	}
	return tools[0].Invoke(context.Background(), raw)
}

func TestGuideReturnsManual(t *testing.T) {
	res, err := invokeGuide(t, "sales")
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if !strings.Contains(res.Text, "orders_top_products") {
		t.Error("手册正文没有原样返回")
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Error("返回了空正文 —— 模型会当成「这个领域没有口径」")
	}
}

func TestGuideUnknownTopicIsArgsError(t *testing.T) {
	_, err := invokeGuide(t, "nope")
	if err == nil {
		t.Fatal("未知手册名应报错")
	}
	var ae *mcp.ArgsError
	if !errors.As(err, &ae) {
		t.Fatalf("应是 ArgsError（参数类错误回给模型自行改正），实得 %T: %v", err, err)
	}
	if !strings.Contains(ae.Msg, "nope") {
		t.Errorf("错误里要点出收到的名字，实得 %q", ae.Msg)
	}
	if !strings.Contains(ae.Msg, "sales") {
		t.Errorf("错误里要列出可用手册名，实得 %q", ae.Msg)
	}
}

func TestGuideRejectsPathLikeNames(t *testing.T) {
	for _, bad := range []string{"", "  ", "../site_rules", "manual/sales", "sales.md"} {
		if _, err := invokeGuide(t, bad); err == nil {
			t.Errorf("%q 不该取到内容", bad)
		}
	}
}
