package routers

// routes_snapshot_test.go — 路由清单快照（审计 CQ-008 的安全网）。
//
// 为什么需要它：SetupRoutes 的装配是**约 700 行的线性过程**，人眼 review 看不出
// 「少挂了一行中间件」或「某个 group 换了父节点」这类漂移 —— 它们不会编译失败，
// 只在运行时表现为「某个接口突然不用鉴权」或「某条路径 404」。
// 拆段重构（assembly.go / assembly_publish.go）必须做到**路由表零漂移**，
// 这份快照就是那个判据：重构前 dump 一次，重构后必须逐字节一致。
//
// 用法（与 TestDumpRoutesForAudit 同一门槛：需要数据库与完整组件装配）：
//   WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestRouteSnapshotStable -count=1 -v
//     → 与 testdata/routes.snapshot 对比，不一致即失败并打印增删清单
//   WP_DUMP_ROUTES=write go test ./internal/routers/ -run TestRouteSnapshotStable -count=1 -v
//     → 用当前装配结果覆写快照
//
// **只在有意增删路由时**才写快照，且写入后要看 diff 是否就是你想加的那几条 ——
// 把「不小心多挂/少挂了路由」当成新基线写进去，这份快照就失去意义了。

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

// bootstrapRouter 装配一次完整路由并返回排序后的清单行（格式：METHOD PATH）。
//
// 两个测试共用：TestDumpRoutesForAudit（给 check-permission-gaps.sh 取路由）
// 与 TestRouteSnapshotStable（与快照比对）。
//
// ## 为什么必须**进程内只装配一次**
//
// SetupRoutes 会在装配末尾把片段层依赖快照提交给 runtimefragment.SetDependencies，
// 而那个入口用 depsSealed.CompareAndSwap 主动炸掉第二次调用（deps.go:111-116）。
// 这是**故意的护栏**，不是缺陷：装配链本来就是线性的只跑一次，「记录后忽略」会让
// 第二个调用点看起来生效、实际被丢弃。
//
// 所以跑两个测试（同进程、各自调一次 bootstrapRouter）时，第二个必然 panic。
// 修的是夹具、不是护栏：这里把装配结果缓存起来，两个测试共享同一份 dump。
// 独立进程跑单个测试时行为不变。
func bootstrapRouter(t *testing.T) []string {
	t.Helper()
	// 装配失败要在**调用它的那个测试**上失败，所以错误随缓存一起传出来重放；
	// 不能在 once 里 Fatalf（那只对一个测试生效，另一个测试会静默拿到空清单）。
	routeDumpOnce.Do(func() {
		routeDumpLines, routeDumpErr = assembleRouteDump()
	})
	if routeDumpErr != nil {
		t.Fatalf("%v", routeDumpErr)
	}
	// 缓存跨测试复用：返回副本，避免某个测试排序/裁剪时改到共享切片。
	out := make([]string, len(routeDumpLines))
	copy(out, routeDumpLines)
	return out
}

var (
	routeDumpOnce  sync.Once
	routeDumpLines []string
	routeDumpErr   error
)

// assembleRouteDump 真正做一次装配（只被 routeDumpOnce 调用一次）。
func assembleRouteDump() ([]string, error) {
	if err := config.Init("../../config.yaml"); err != nil {
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
	}
	sort.Strings(lines)
	return lines, nil
}

// TestRouteSnapshotStable 路由清单与 testdata/routes.snapshot 逐字节比对。
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
