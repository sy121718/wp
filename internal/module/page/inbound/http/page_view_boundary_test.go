package pagehttp

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var viewShapePattern = regexp.MustCompile(`^type \w+(View|Row|Item) struct`)

// TestViewShapesLiveInDTO 页面/片段的渲染视图形状必须放 dto（internal/module/CLAUDE.md
// §inbound/http「渲染视图与表单回显形状放 dto（第三类）」）。
//
// 判据：本目录不得再声明 `XxxView` / `XxxRow` / `XxxItem` 这类**纯数据行形状** ——
// 它们与 handle、模板、测试三处逐字段对齐，留在控制器里，任何想复用或断言它的地方都得
// import inbound/http（带着全部 handler）。合法形状是：handler 结构体、带装配方法的聚合
// （产出 gin.H 的 templateMap）、解析/装配选项、请求绑定结构、内部缓存与事实。
//
// 清单**只减不增**，两个方向都判失败（与 page_i18n_boundary_test.go 同一判据）：
//
//	· 新写一处行形状 → 失败（否则这条规则会在下一次并行改造里被悄悄加回来）；
//	· 登记项不再命中 → 也失败（登记表退化成注释就没人看它了）。
func TestViewShapesLiveInDTO(t *testing.T) {
	// 目前为空：page 模块的渲染行形状已全部搬进 dto/page_view.go。
	// 确属例外（例如给外部接口用的 JSON 响应行）就登记进这里并写明理由。
	allow := map[string]string{}

	hits := scanViewShapes(t)
	for file, lines := range hits {
		if _, ok := allow[file]; !ok {
			t.Errorf("%s 声明了渲染行形状（第 %s 行）—— 纯数据视图放 internal/module/page/dto/page_view.go，"+
				"确属例外就登记进 allow 并写明理由", file, strings.Join(lines, ", "))
		}
	}
	for file, reason := range allow {
		if _, ok := hits[file]; !ok {
			t.Errorf("allow 里的 %s 已不再声明行形状（搬完了就从清单删掉）", file)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allow 里的 %s 缺理由", file)
		}
	}
}

// scanViewShapes 扫本目录下非测试文件里的行形状声明，返回 文件 → 行号列表。
func scanViewShapes(t *testing.T) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读目录失败: %v", err)
	}
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("读 %s 失败: %v", name, rerr)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if viewShapePattern.MatchString(line) {
				out[name] = append(out[name], strconv.Itoa(i+1))
			}
		}
	}
	return out
}
