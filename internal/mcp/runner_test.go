package mcp

// runner_test.go — 执行器的判据：越权**不执行**、系统故障不放行、参数错误可回给模型。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go_wp/internal/permission"
)

// countingRegistry 一个记录调用次数的工具集，用来验证「被拒时执行体没被碰过」。
func countingRegistry(t *testing.T, calls *int) *Registry {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Register(New("orders_summary", "订单区间摘要", "测试", permission.OrderList,
		Object("参数", map[string]Schema{"projectId": String("工程 id")}, "projectId"),
		func(_ context.Context, args struct {
			ProjectID string `json:"projectId"`
		}) (Result, error) {
			*calls++
			return Result{Text: "ok:" + args.ProjectID}, nil
		})); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	return reg
}

func allowAll(context.Context, int64, permission.Perm) (bool, error) { return true, nil }
func denyAll(context.Context, int64, permission.Perm) (bool, error)  { return false, nil }
func authzFails(context.Context, int64, permission.Perm) (bool, error) {
	return false, errors.New("enforcer 未初始化")
}

func TestRunnerRun(t *testing.T) {
	ctx := context.Background()

	t.Run("权限通过则执行", func(t *testing.T) {
		calls := 0
		r := NewRunner(countingRegistry(t, &calls), allowAll)
		res, err := r.Run(ctx, 7, "orders_summary", `{"projectId":"p1"}`)
		if err != nil {
			t.Fatalf("执行失败: %v", err)
		}
		if res.Text != "ok:p1" || calls != 1 {
			t.Fatalf("res = %+v, calls = %d", res, calls)
		}
	})

	t.Run("越权：不执行、且是可回给模型的错误", func(t *testing.T) {
		calls := 0
		r := NewRunner(countingRegistry(t, &calls), denyAll)
		_, err := r.Run(ctx, 7, "orders_summary", `{"projectId":"p1"}`)
		var permErr *PermissionError
		if !errors.As(err, &permErr) {
			t.Fatalf("应是 *PermissionError，实得 %T（%v）", err, err)
		}
		// 顺序判据：先判权限再执行。反过来（执行完再判）在这里表现为 calls == 1 ——
		// 数据已经出库，对审计来说那次越权读取已经发生了。
		if calls != 0 {
			t.Fatalf("越权时执行体不该被调用，实际调用了 %d 次", calls)
		}
	})

	t.Run("未知工具", func(t *testing.T) {
		calls := 0
		r := NewRunner(countingRegistry(t, &calls), allowAll)
		_, err := r.Run(ctx, 7, "orders_delete", `{}`)
		var unknown *UnknownToolError
		if !errors.As(err, &unknown) {
			t.Fatalf("应是 *UnknownToolError，实得 %T（%v）", err, err)
		}
	})

	t.Run("权限判定自身出错：不放行", func(t *testing.T) {
		calls := 0
		r := NewRunner(countingRegistry(t, &calls), authzFails)
		if _, err := r.Run(ctx, 7, "orders_summary", `{"projectId":"p1"}`); err == nil {
			t.Fatal("判定出错必须报错，不能当成放行")
		}
		if calls != 0 {
			t.Fatalf("判定出错时执行体不该被调用，实际调用了 %d 次", calls)
		}
	})

	t.Run("空 arguments 视作空对象", func(t *testing.T) {
		calls := 0
		reg := NewRegistry()
		if err := reg.Register(New("pulse", "心跳", "无参数工具", permission.OrderList,
			Object("无参数", map[string]Schema{}),
			func(context.Context, struct{}) (Result, error) {
				calls++
				return Result{Text: "alive"}, nil
			})); err != nil {
			t.Fatalf("注册失败: %v", err)
		}
		r := NewRunner(reg, allowAll)
		if _, err := r.Run(ctx, 7, "pulse", ""); err != nil {
			t.Fatalf("空参数应被视作 {}: %v", err)
		}
		if calls != 1 {
			t.Fatalf("calls = %d", calls)
		}
	})

	t.Run("参数不合规：ArgsError 而不是 panic", func(t *testing.T) {
		calls := 0
		r := NewRunner(countingRegistry(t, &calls), allowAll)
		_, err := r.Run(ctx, 7, "orders_summary", `{"projectId":1}`)
		var argsErr *ArgsError
		if !errors.As(err, &argsErr) {
			t.Fatalf("应是 *ArgsError，实得 %T（%v）", err, err)
		}
	})

	t.Run("注册表缺失：装配缺陷要显式报错", func(t *testing.T) {
		r := NewRunner(nil, allowAll)
		if _, err := r.Run(ctx, 7, "orders_summary", `{}`); err == nil {
			t.Fatal("注册表为 nil 时应报错，而不是表现成「工具不存在」")
		}
	})

	t.Run("权限判定未接入：装配缺陷要显式报错", func(t *testing.T) {
		calls := 0
		r := NewRunner(countingRegistry(t, &calls), nil)
		if _, err := r.Run(ctx, 7, "orders_summary", `{"projectId":"p1"}`); err == nil {
			t.Fatal("authorizer 为 nil 时应报错，不能默认放行")
		}
		if calls != 0 {
			t.Fatalf("未接入判定时执行体不该被调用，实际调用了 %d 次", calls)
		}
	})
}

func TestRunnerToolsExposesDeclarations(t *testing.T) {
	calls := 0
	r := NewRunner(countingRegistry(t, &calls), allowAll)
	tools := r.Tools()
	if len(tools) != 1 || tools[0].Name() != "orders_summary" {
		t.Fatalf("Tools = %+v", tools)
	}
	raw, err := tools[0].SchemaJSON()
	if err != nil {
		t.Fatalf("SchemaJSON 失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("schema 不是合法 JSON: %v", err)
	}
	if decoded["type"] != "object" {
		t.Fatalf("schema 类型 = %v", decoded["type"])
	}
	if decoded["additionalProperties"] != false {
		t.Fatal("schema 必须带 additionalProperties=false（参数白名单是安全边界）")
	}
}
