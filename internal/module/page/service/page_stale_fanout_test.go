package pageservice

import (
	"context"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/logger"
)

func TestStaleIDCollectorAggregatesAndDedupes(t *testing.T) {
	c := &staleIDCollector{}
	if got := c.list(); len(got) != 0 {
		t.Fatalf("空收集器应给出空集合，实际 %v", got)
	}
	c.add(nil)
	c.add([]string{})
	c.add([]string{"p2-b", "  ", "p1-a"})
	c.add([]string{"p2-b", "p1-a", "p3-c"})

	got := c.list()
	want := []string{"p2-b", "p1-a", "p3-c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("聚合结果 %v，期望 %v（首次出现次序 + 去重 + 跳过空白）", got, want)
	}
	for _, id := range got {
		if strings.TrimSpace(id) == "" {
			t.Fatal("空白 id 不应进入聚合结果：它会变成一个凭空多出来的「受影响页面」")
		}
	}
}

func TestStaleIDCollectorTrimsIDs(t *testing.T) {
	c := &staleIDCollector{}
	c.add([]string{" p1 "})
	c.add([]string{"p1"})
	if got := c.list(); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("TrimSpace 后相同 id 应合并为一条且取干净值，实际 %v", got)
	}
}

func TestChunkIDs(t *testing.T) {
	if got := chunkIDs(nil, 3); got != nil {
		t.Fatalf("空集合不应产生任何块，实际 %v", got)
	}

	for _, size := range []int{0, -1} {
		got := chunkIDs([]string{"a", "b"}, size)
		if len(got) != 1 || len(got[0]) != 2 {
			t.Fatalf("size=%d 应退回单块，实际 %v", size, got)
		}
	}
	if got := chunkIDs([]string{"a", "b"}, 2); len(got) != 1 {
		t.Fatalf("len==size 应为单块（不做无谓拆分），实际 %v", got)
	}

	for _, c := range []struct{ n, size int }{{6, 3}, {7, 3}, {1000, 500}, {1001, 500}, {1, 500}} {
		in := make([]string, 0, c.n)
		for i := 0; i < c.n; i++ {
			in = append(in, "id-"+strconv.Itoa(i))
		}
		chunks := chunkIDs(in, c.size)
		wantChunks := (c.n + c.size - 1) / c.size
		if len(chunks) != wantChunks {
			t.Fatalf("n=%d size=%d 应切成 %d 块，实际 %d 块", c.n, c.size, wantChunks, len(chunks))
		}
		var flat []string
		for _, ch := range chunks {
			if len(ch) == 0 {
				t.Fatalf("n=%d size=%d 出现了空块（会白白多一次查询）", c.n, c.size)
			}
			if len(ch) > c.size {
				t.Fatalf("n=%d size=%d 有块超出上限：%d", c.n, c.size, len(ch))
			}
			flat = append(flat, ch...)
		}
		if strings.Join(flat, ",") != strings.Join(in, ",") {
			t.Fatalf("n=%d size=%d 分块后次序 / 内容与输入不一致（分块不得丢 id）", c.n, c.size)
		}
	}
}

func TestPlanStaleImpactLog(t *testing.T) {
	plan := planStaleImpactLog(0, nil)
	if !plan.NoSample || plan.Sample != "" || plan.SampleShown != 0 {
		t.Fatalf("摘要不可用时应标记 NoSample 且不带样本，实际 %+v", plan)
	}

	impact := &pagedto.StaleImpactSummaryResp{
		Total: 9, Limit: 2, Truncated: true,
		Pages: []pagedto.StalePageResp{{Title: "关于我们", Path: "/about"}, {Path: "/contact"}},
	}
	plan = planStaleImpactLog(9, impact)
	if plan.NoSample {
		t.Fatal("摘要可用时不应退化为只记条数")
	}
	if plan.Sample != "关于我们 (/about); /contact" {
		t.Fatalf("样本格式化结果不符：%q", plan.Sample)
	}
	if plan.SampleShown != 2 || !plan.Truncated || plan.Total != 9 {
		t.Fatalf("样本条数 / 截断标记 / 总数不符：%+v", plan)
	}
	if !strings.Contains(plan.Message, "本次影响 9 个页面") || !strings.Contains(plan.Message, "前 2 个") {
		t.Fatalf("文案应含事实总数与样本条数：%q", plan.Message)
	}

	plan = planStaleImpactLog(9, &pagedto.StaleImpactSummaryResp{Total: 8})
	if plan.Total != 9 || plan.SampleShown != 0 || plan.Truncated {
		t.Fatalf("总数应取调用方给的事实值：%+v", plan)
	}
}

func TestLogStaleImpactWritesNothingOnEmptyIDs(t *testing.T) {
	baseDir := initTempLogger(t)
	svc := &Service{}
	ctx := context.Background()

	svc.logStaleImpact(ctx, "i18n", nil)
	svc.logStaleImpact(ctx, "i18n", []string{})
	svc.logStaleImpact(ctx, "i18n", []string{"", "   "})

	if _, err := os.Stat(filepath.Join(baseDir, "dependency")); !os.IsNotExist(err) {
		t.Fatalf("空集合不应产生任何日志行（dependency 场景目录已出现，err=%v）", err)
	}
}

func TestLogStaleImpactDegradesToCountOnly(t *testing.T) {
	baseDir := initTempLogger(t)
	svc := &Service{}
	svc.logStaleImpact(context.Background(), "theme:t-1", []string{"p1", "p2", "p1"})
	if err := logger.Close(); err != nil {
		t.Fatalf("关闭 logger 失败: %v", err)
	}

	lines := readSceneLog(t, baseDir, "dependency")
	if len(lines) != 1 {
		t.Fatalf("一次调用应写入 1 行，实际 %d 行：%v", len(lines), lines)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("日志行不是 JSON（%v）：%s", err, lines[0])
	}
	if got := entry["affected"]; got != float64(2) {
		t.Fatalf("affected 应为去重后的 2，实际 %v", got)
	}
	if got := entry["reason"]; got != "theme:t-1" {
		t.Fatalf("reason 应为 theme:t-1，实际 %v", got)
	}
	if got := entry["sample_limit"]; got != float64(staleImpactSampleLimit) {
		t.Fatalf("sample_limit 应为 %d，实际 %v", staleImpactSampleLimit, got)
	}
	msg, _ := entry["message"].(string)
	if !strings.Contains(msg, "本次影响 2 个页面") || !strings.Contains(msg, "摘要不可用") {
		t.Fatalf("降级文案不符：%q", msg)
	}
}

func initTempLogger(t *testing.T) string {
	t.Helper()
	baseDir := filepath.Join(t.TempDir(), "logs")
	v := viper.New()
	v.Set("log.base_dir", baseDir)
	if err := logger.Init(v); err != nil {
		t.Fatalf("初始化临时 logger 失败: %v", err)
	}

	t.Cleanup(func() { _ = logger.Close() })
	return baseDir
}

func readSceneLog(t *testing.T, baseDir, scene string) []string {
	t.Helper()
	dir := filepath.Join(baseDir, scene)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取场景日志目录失败（%s）: %v", dir, err)
	}
	var lines []string
	for _, e := range entries {
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("读取日志文件 %s 失败: %v", e.Name(), rerr)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}
