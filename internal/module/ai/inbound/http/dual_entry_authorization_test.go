package aihttp

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	mw "go_wp/pkg/middleware"
)

// 站内工具入口（Runner）必须真的接上授权闭包。
//
// 只断言 PermIndex.Has 证明不了执行期行为：真正的缺陷形态是「登记对了，
// 但入口没接授权」—— 那种情况索引查询全绿，工具却照跑。这里让调用真的进入
// Runner，观察它是否被拒绝。外部 MCP 端点的同形验证在 dual_entry_mcp_test.go
// 的 aihttp_test 包里（它需要导入 support 去建库，留在内部测试包会形成导入环）。

func dualEntryTool() mcp.Tool {
	return mcp.New("orders_summary", "订单摘要", "查询订单",
		"order:list",
		mcp.Object("参数", map[string]mcp.Schema{"projectId": mcp.String("工程 id")}, "projectId"),
		func(_ context.Context, args struct {
			ProjectID string `json:"projectId"`
		}) (mcp.Result, error) {
			return mcp.Result{Text: "RAN:" + args.ProjectID}, nil
		})
}

// dualEntryBindings 构造「已登记 order:list」的不可变索引。
// 授权闭包本身仍走真实 casbin：用例在 casbin 未初始化/无策略的环境里运行，
// 因此判定结果必然是拒绝 —— 正是要验证的行为。
func dualEntryBindings() *mw.PermIndex {
	return mw.BuildPermIndex([]mw.RouteSpec{{
		Method: "POST", Path: "/api/order/list", Perm: "order:list",
		Kind: mw.RouteProtected, AuthObject: "/api/order/list", AuthMethod: "POST",
	}})
}

func newDualEntryRunner(bindings **mw.PermIndex) *toolProvider {
	registry := mcp.NewRegistry()
	_ = registry.Register(dualEntryTool())
	return &toolProvider{runner: mcp.NewRunner(registry, mcp.Authorizer(RouteAuthorizer(bindings)))}
}

// 情形一：索引未注入（装配未完成）—— 必须拒绝。
func TestToolRunnerRejectsWhenBindingsMissing(t *testing.T) {
	res, err := newDualEntryRunner(nil).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if err != nil {
		t.Fatalf("站内入口不该把拒绝抛成协议错误：%v", err)
	}
	if strings.Contains(res.Text, "RAN:") {
		t.Fatalf("索引未注入时站内工具仍然执行了：%+v", res)
	}
}

// 情形二：索引有绑定但 Casbin 无策略 —— 必须拒绝。
func TestToolRunnerRejectsWhenPolicyMissing(t *testing.T) {
	bindings := dualEntryBindings()
	res, _ := newDualEntryRunner(&bindings).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if strings.Contains(res.Text, "RAN:") {
		t.Fatalf("没有策略时站内入口执行了工具：%+v", res)
	}
}

// 情形三：授权错误按拒绝折叠，不外抛内部错误。
//
// 注意本包不能初始化 Casbin（导入 support 会成环），所以「无策略」与「授权器不可用」
// 在这里是同一个环境状态；**分别受控**的三前提（索引未注入 / 已初始化但无策略 /
// 未初始化）在 public/test/ai/feature 的 TestDualEntryRejectsWhenPolicyMissing 与
// TestDualEntryTreatsAuthorizerErrorAsDenied 里，那里能真实建库并初始化。
// 这里只钉住 toolProvider 的折叠契约：错误变成拒绝事实，工具不执行。
func TestToolRunnerTreatsAuthorizerErrorAsDenied(t *testing.T) {
	bindings := dualEntryBindings()
	res, err := newDualEntryRunner(&bindings).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if err != nil {
		t.Fatalf("授权错误应折叠成拒绝事实，而不是把内部错误抛给调用方：%v", err)
	}
	if strings.Contains(res.Text, "RAN:") {
		t.Fatalf("授权器不可用时站内入口执行了工具：%+v", res)
	}
}
