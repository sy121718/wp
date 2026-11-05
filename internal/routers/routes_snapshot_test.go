package routers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"go_wp/config"

	"github.com/gin-gonic/gin"
)

const routeSnapshotEnv = "WP_DUMP_ROUTES"

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
	router := gin.New()
	SetupRoutes(router, func() error { return nil })

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
