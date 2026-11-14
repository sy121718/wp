package feature

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	pkgcasbin "go_wp/pkg/casbin"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/mcp"
	mw "go_wp/pkg/middleware"
	"go_wp/public/test/support"
)

// 两个执行入口（站内工具 Runner 与外部 MCP 端点）必须真的共用同一份授权闭包，
// 并且三种前提要**分别受控**，不能靠「环境里恰好没初始化」解释拒绝：
//
//  1. 索引未注入（装配未完成）；
//  2. 索引有绑定，但 Casbin 里没有这条策略；
//  3. 授权器不可用（Casbin 未初始化）。
//
// 另外补一条正向对照：策略存在时两条入口都必须放行 —— 只测拒绝时，
// 一个「永远返回 false」的授权器也能让三条都绿。

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

func dualEntryBindings() *mw.PermIndex {
	return mw.BuildPermIndex([]mw.RouteSpec{{
		Method: http.MethodPost, Path: "/api/order/list", Perm: "order:list",
		Kind: mw.RouteProtected, AuthObject: "/api/order/list", AuthMethod: http.MethodPost,
	}})
}

// dualEntryRunner 复制 ai_router.go 的 Runner 接线：同一个 RouteAuthorizer。
func dualEntryRunner(bindings **mw.PermIndex) *mcp.Runner {
	registry := mcp.NewRegistry()
	_ = registry.Register(dualEntryTool())
	return mcp.NewRunner(registry, mcp.Authorizer(aihttp.RouteAuthorizer(bindings)))
}

// newDualEntryMCPEngine 复制 ai_router.go 的 MCP 端点接线。
func newDualEntryMCPEngine(t *testing.T, db *gorm.DB, bindings **mw.PermIndex) (*gin.Engine, string) {
	t.Helper()
	applyProductionSchema(t, db)

	tokens := aiservice.NewAccessTokenService(aimodel.NewAccessTokenModel(db))
	tokens.SetScopeValidator(func(context.Context, string) bool { return true })
	created, err := tokens.Create(context.Background(), &aidto.TokenCreateReq{
		Name: "外部集成", Scopes: []string{"order:list"}, UserID: 9,
	})
	if err != nil {
		t.Fatalf("签发测试令牌失败：%v", err)
	}

	registry := mcp.NewRegistry()
	if err := registry.Register(dualEntryTool()); err != nil {
		t.Fatalf("注册工具失败：%v", err)
	}
	endpoint := aihttp.NewMcpEndpoint(tokens, registry, aiservice.NewToolCallRecorder(aimodel.NewToolCallLogModel(db)))
	endpoint.SetAccountAuthorizer(aihttp.RouteAuthorizer(bindings))
	endpoint.SetConfigReader(stubMCPConfig{enabled: true})

	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.POST("/mcp", endpoint.Handle)
	return e, created.Token
}

const dualEntryCallBody = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"orders_summary","arguments":{"projectId":"p1"}}}`

// mcpRanTool 判断响应里是否出现工具真的执行过的痕迹。
func mcpRanTool(out map[string]any) bool {
	raw, _ := json.Marshal(out)
	return strings.Contains(string(raw), "RAN:")
}

// initDualEntryCasbin 在迁库上初始化 Casbin；返回的清理函数还原全局单例。
//
// 每个用例自行控制「有没有 order:list 策略」：初始化后默认没有任何策略，
// 需要放行的用例显式 AddPolicy。
func initDualEntryCasbin(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = pkgcasbin.Close()
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败：%v", err)
	}
	t.Cleanup(func() { _ = pkgcasbin.Close() })
}

// 情形一：索引未注入（装配未完成）—— 两条入口都必须拒绝。
func TestDualEntryRejectsWhenBindingsMissing(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	res, err := dualEntryRunner(nil).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if strings.Contains(res.Text, "RAN:") {
		t.Fatalf("索引未注入时站内工具仍执行了：%+v", res)
	}
	var permErr *mcp.PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("索引未注入必须判权限拒绝，实际 err=%v res=%+v", err, res)
	}

	e, token := newDualEntryMCPEngine(t, db, nil)
	_, out := rpcCall(t, e, token, dualEntryCallBody)
	if mcpRanTool(out) {
		t.Fatalf("索引未注入时外部 MCP 入口执行了工具：%v", out)
	}
}

// 情形二：索引有绑定，但 Casbin 里没有策略 —— 两条入口都必须拒绝。
//
// 必须先真实初始化 Casbin（再断言无策略）：否则「未初始化」会同样让用例变绿，
// 证明不了「已初始化但无策略」这条路径。
func TestDualEntryRejectsWhenPolicyMissing(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initDualEntryCasbin(t, db)
	bindings := dualEntryBindings()

	res, err := dualEntryRunner(&bindings).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if strings.Contains(res.Text, "RAN:") {
		t.Fatalf("没有策略时站内入口执行了工具：%+v", res)
	}
	var permErr *mcp.PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("已初始化但无策略必须判权限拒绝，实际 err=%v res=%+v", err, res)
	}

	e, token := newDualEntryMCPEngine(t, db, &bindings)
	_, out := rpcCall(t, e, token, dualEntryCallBody)
	if mcpRanTool(out) {
		t.Fatalf("没有策略时外部入口执行了工具：%v", out)
	}
}

// 情形三：授权器不可用（Casbin 未初始化）—— 两条入口都不得执行工具。
//
// 站在 mcp.Runner 这一层，授权错误会作为 error 返回（Run 的契约）；
// HTTP 面的 toolProvider 再把它折叠成失败事实。这里只断言最要紧的一条：
// **工具没有跑**，且错误确实来自「未初始化」而不是别的分支。
func TestDualEntryTreatsAuthorizerErrorAsDenied(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	_ = pkgcasbin.Close() // 显式保证未初始化，不依赖前一个用例的清理顺序
	t.Cleanup(func() { _ = pkgcasbin.Close() })
	bindings := dualEntryBindings()

	res, err := dualEntryRunner(&bindings).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if strings.Contains(res.Text, "RAN:") {
		t.Fatalf("授权器不可用时站内入口执行了工具：%+v err=%v", res, err)
	}
	var permErr *mcp.PermissionError
	if errors.As(err, &permErr) {
		// 索引里有绑定、拒绝来自策略层：这说明 Casbin 其实可用，前提不成立。
		t.Fatalf("前提不成立：授权器本应不可用，却走到了策略判定（err=%v）", err)
	}
	if err == nil {
		t.Fatalf("授权器不可用必须报错并拒绝，实际 err=nil res=%+v", res)
	}

	e, token := newDualEntryMCPEngine(t, db, &bindings)
	code, out := rpcCall(t, e, token, dualEntryCallBody)
	if code != http.StatusOK {
		t.Fatalf("JSON-RPC 协议层仍应回 200，实际 %d", code)
	}
	if mcpRanTool(out) {
		t.Fatalf("授权器不可用时外部入口执行了工具：%v", out)
	}
}

// 正向对照：策略存在时两条入口都必须真的放行。
//
// 只测拒绝的话，把 RouteAuthorizer 改成「永远 false」也能让上面三条全绿；
// 这条对应用例把「判定真的执行了」钉住。
func TestDualEntryRunsToolWhenPolicyAllows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	initDualEntryCasbin(t, db)
	if _, err := pkgcasbin.GetEnforcer().AddPolicy("9", "/api/order/list", "POST", "order:list"); err != nil {
		t.Fatalf("写入测试策略失败：%v", err)
	}
	bindings := dualEntryBindings()

	res, err := dualEntryRunner(&bindings).Run(context.Background(), 9, "orders_summary", `{"projectId":"p1"}`)
	if err != nil {
		t.Fatalf("策略存在时站内入口应放行，实际 err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Text, "RAN:p1") {
		t.Fatalf("策略存在时站内入口应执行工具，实际 res=%+v", res)
	}

	e, token := newDualEntryMCPEngine(t, db, &bindings)
	_, out := rpcCall(t, e, token, dualEntryCallBody)
	if !mcpRanTool(out) {
		t.Fatalf("策略存在时外部入口应执行工具：%v", out)
	}
}
