package aimcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
)

// invokeGuide 调一次 guide。
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

// TestGuideReturnsManual 取得到的手册原样返回。
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

// TestGuideUnknownTopicIsArgsError 取不到的手册必须报错，且错误里带可用名字。
//
// 回空串是最坏的做法：模型会把它读成「这个领域没有口径」，转而按常识回答 ——
// 而那正是 guide 存在的理由。错误里必须带上可用清单，模型才知道下一步该试什么。
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

// TestGuideRejectsPathLikeNames 形状不对的名字也不能取到东西。
func TestGuideRejectsPathLikeNames(t *testing.T) {
	for _, bad := range []string{"", "  ", "../site_rules", "manual/sales", "sales.md"} {
		if _, err := invokeGuide(t, bad); err == nil {
			t.Errorf("%q 不该取到内容", bad)
		}
	}
}
