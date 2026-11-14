package routers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"go_wp/config"
	mw "go_wp/pkg/middleware"

	"github.com/gin-gonic/gin"
)

const routeSnapshotEnv = "WP_DUMP_ROUTES"

// auditRegistry / auditPermProblems 是 assembleRouteDump 那次装配的登记索引与
// 权限核对结果；审计输出读它们，不读包级默认实例。
var (
	auditRegistry     *mw.Registry
	auditPermProblems []string
	auditGateReady    bool
)

func bootstrapRouter(t *testing.T) []string {
	t.Helper()

	routeDumpOnce.Do(func() {
		routeDumpLines, routeDumpErr = assembleRouteDump()
	})
	if routeDumpErr != nil {
		t.Fatalf("%v", routeDumpErr)
	}

	out := make([]string, len(routeDumpLines))
	copy(out, routeDumpLines)
	return out
}

var (
	routeDumpOnce     sync.Once
	routeDumpLines    []string
	routeDumpHandlers []string
	routeDumpErr      error
)

func assembleRouteDump() ([]string, error) {
	configPath := strings.TrimSpace(os.Getenv("GOWP_AUDIT_CONFIG"))
	if configPath == "" {
		configPath = "../../config.yaml"
	}
	if err := config.Init(configPath); err != nil {
		return nil, fmt.Errorf("配置加载失败: %w", err)
	}
	if _, err := config.GetServer(); err != nil {
		return nil, fmt.Errorf("server 配置解析失败: %w", err)
	}
	if err := config.InitComponents(); err != nil {
		return nil, fmt.Errorf("组件初始化失败: %w", err)
	}
	gin.SetMode(gin.TestMode)
	// 审计装配跳过 seed：seed 会把被删掉的权限码补回来，
	// 「目录里根本没有这条权限点」就会被自动修复掩盖，漂移永远看不见。
	// 只读现有目录才能做真实对账（测试结束由进程退出，不写回任何东西）。
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WP_AUDIT_NO_SEED")), "1") {
		if v, verr := config.GetViper(); verr == nil && v != nil {
			v.Set("database.run_migrations", false)
		}
	}
	router := gin.New()
	// 与生产同一条装配路径（SetupRoutesWithRegistry 只交回 Registry，核对结果留在实例里），
	// 这里直接建装配实例，把「权限核对结论」和「闸门状态」一起取出来供断言。
	asm := newAssembly(context.Background(), router)
	runAssembly(asm, func() error { return nil })
	auditRegistry = asm.registry
	auditPermProblems = append([]string(nil), asm.permProblems...)
	auditGateReady = asm.gate.allow()

	lines := make([]string, 0, 512)
	for _, r := range router.Routes() {
		lines = append(lines, r.Method+" "+r.Path)
		routeDumpHandlers = append(routeDumpHandlers, "HANDLER "+r.Method+" "+r.Path+" "+r.Handler)
	}
	sort.Strings(routeDumpHandlers)
	sort.Strings(lines)
	return lines, nil
}

func TestRouteSnapshotStable(t *testing.T) {
	mode := os.Getenv(routeSnapshotEnv)
	if mode != "1" && mode != "write" {
		t.Skip("设置 " + routeSnapshotEnv + "=1（比对）或 =write（覆写快照）才运行")
	}

	lines := bootstrapRouter(t)
	got := strings.Join(lines, "\n")
	path := filepath.Join("testdata", "routes.snapshot")

	if mode == "write" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("创建 testdata 目录失败: %v", err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatalf("写入快照失败: %v", err)
		}
		t.Logf("已覆写路由快照：%d 条 → %s（请 review git diff）", len(lines), path)
		return
	}

	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取路由快照失败（首次使用请先跑 %s=write）: %v", routeSnapshotEnv, err)
	}
	want := strings.TrimRight(string(wantBytes), "\n")
	if want == got {
		t.Logf("路由快照一致：%d 条", len(lines))
		return
	}

	wantSet := map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		wantSet[l] = true
	}
	gotSet := map[string]bool{}
	for _, l := range lines {
		gotSet[l] = true
	}
	var added, removed []string
	for _, l := range lines {
		if !wantSet[l] {
			added = append(added, l)
		}
	}
	for _, l := range strings.Split(want, "\n") {
		if !gotSet[l] {
			removed = append(removed, l)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	t.Fatalf("路由清单与快照不一致（快照 %d 条 / 当前 %d 条）\n"+
		"新增 %d 条:\n    %s\n"+
		"消失 %d 条:\n    %s\n"+
		"若这是有意的路由变更，用 %s=write 更新快照并 review diff。",
		len(wantSet), len(lines),
		len(added), strings.Join(added, "\n    "),
		len(removed), strings.Join(removed, "\n    "),
		routeSnapshotEnv)
}
