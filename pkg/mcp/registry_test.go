package mcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"testing"

)

func echoTool(name string) Tool {
	return New(name, name, "测试用工具", "order:list",
		Object("回显参数", map[string]Schema{"name": String("要回显的名字")}, "name"),
		func(_ context.Context, args struct {
			Name string `json:"name"`
		}) (Result, error) {
			return Result{Text: "hello " + args.Name, Data: args.Name}, nil
		})
}

func TestRegistryListIsNameSorted(t *testing.T) {

	r := NewRegistry()
	if err := r.RegisterAll(echoTool("orders_summary"), echoTool("analytics_summary"), echoTool("pages_list")); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	got := make([]string, 0, 3)
	for _, tool := range r.List() {
		got = append(got, tool.Name())
	}
	want := []string{"analytics_summary", "orders_summary", "pages_list"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("列举顺序应为名称升序 %v，实得 %v", want, got)
		}
	}
	if r.Len() != 3 {
		t.Fatalf("工具数应为 3，实得 %d", r.Len())
	}
}

func TestRegistryRejectsDuplicateAndEmptyName(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(echoTool("orders_summary")); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := r.Register(echoTool("orders_summary")); err == nil {
		t.Fatal("重名注册应当报错（覆盖会让「谁生效」取决于装配顺序）")
	}
	noop := func(context.Context, struct{}) (Result, error) { return Result{}, nil }
	if err := r.Register(New("", "无名字", "", "order:list", Object("x", map[string]Schema{}), noop)); err == nil {
		t.Fatal("空名注册应当报错")
	}
}

func TestRegistryInvoke(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(echoTool("orders_summary")); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	ctx := context.Background()

	t.Run("正常调用", func(t *testing.T) {
		res, err := r.Invoke(ctx, "orders_summary", json.RawMessage(`{"name":"YG"}`))
		if err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if res.Text != "hello YG" {
			t.Fatalf("正文应为 hello YG，实得 %q", res.Text)
		}
	})

	t.Run("未知工具：可回给模型的错误", func(t *testing.T) {
		_, err := r.Invoke(ctx, "orders_deleted", nil)
		var unknown *UnknownToolError
		if !errors.As(err, &unknown) {
			t.Fatalf("应为 *UnknownToolError，实得 %T（%v）", err, err)
		}
	})

	t.Run("Invoke 也走参数校验", func(t *testing.T) {

		_, err := r.Invoke(ctx, "orders_summary", json.RawMessage(`{"nope":1}`))
		var argsErr *ArgsError
		if !errors.As(err, &argsErr) {
			t.Fatalf("应为 *ArgsError，实得 %T（%v）", err, err)
		}
	})
}
