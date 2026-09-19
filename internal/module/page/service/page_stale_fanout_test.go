package pageservice

// page_stale_fanout_test.go — 逐工程扇出的命中集合聚合、分块与影响面日志决策（纯逻辑就近单测）。
//
// 为什么这三件事必须就地钉住：整站标记（主题 / 块 / 词条）的影响面回执**不进返回值**，
// 只进日志 —— 这层逻辑错了不会报错、不会有测试变红，只会在日志里多算 / 少算几页，
// 或者把样本静默截短。具体覆盖：
//   1. 逐工程命中集合的聚合去重（「本次影响 N 个页面」里的 N 就是它）；
//   2. IN 查询分块（切错了会让样本少几行，同样静默）；
//   3. 空集合不产生任何日志（连场景目录都不会被创建）；
//   4. 摘要取不到时降级为只记条数，且总数不跟着摘要一起变小。
//
// 门禁绿 ≠ 问题不存在：本文件不碰数据库，SQL 侧口径（RETURNING id 的命中集合、RLS 作用域、
// 分块查询的真实代价）由 public/test/page 与 public/test/rls 的真实 PG 用例覆盖。
//
// 本文件的最后两个用例会初始化**全局 logger**（指到 t.TempDir 下）以便断言「写了 / 没写」；
// 同包其它用例不使用 logger，互不干扰（Cleanup 里会 Close）。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/logger"
)

// TestStaleIDCollectorAggregatesAndDedupes 逐工程命中集合的聚合：去重、保序、跳过空白 id。
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

// TestStaleIDCollectorTrimsIDs 归一化口径：带空白的 id 与干净 id 是同一个页面，只能算一次。
func TestStaleIDCollectorTrimsIDs(t *testing.T) {
	c := &staleIDCollector{}
	c.add([]string{" p1 "})
	c.add([]string{"p1"})
	if got := c.list(); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("TrimSpace 后相同 id 应合并为一条且取干净值，实际 %v", got)
	}
}

// TestChunkIDs 分块的边界：空集合、非法 size、整除、带余数 —— 且**一个 id 都不能丢**。
func TestChunkIDs(t *testing.T) {
	if got := chunkIDs(nil, 3); got != nil {
		t.Fatalf("空集合不应产生任何块，实际 %v", got)
	}
	// size <= 0（调用方给了不合法的值）退回单块：静默丢 id 的语义是「这些页面不存在」，
	// 那属于最不该出现的失败形态，宁可退回不分块。
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

// TestPlanStaleImpactLog 两种日志形态：摘要可用（计数 + 样本 + 截断标记）与摘要不可用（只记条数）。
func TestPlanStaleImpactLog(t *testing.T) {
	plan := planStaleImpactLog(0, nil)
	if !plan.NoSample || plan.Sample != "" || plan.SampleShown != 0 {
		t.Fatalf("摘要不可用时应标记 NoSample 且不带样本，实际 %+v", plan)
	}

	impact := &pagedto.StaleImpactSummary{
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

	// 总数取调用方给的事实值，不跟着（只读反查的）摘要一起变小。
	plan = planStaleImpactLog(9, &pagedto.StaleImpactSummary{Total: 8})
	if plan.Total != 9 || plan.SampleShown != 0 || plan.Truncated {
		t.Fatalf("总数应取调用方给的事实值：%+v", plan)
	}
}

// TestLogStaleImpactWritesNothingOnEmptyIDs 空集合**不产生日志**。
//
// 断言形态只有这一种：logger 按场景懒创建（getSceneLogger 只在真正要写一行时被调用），
// 所以「文件系统里连场景目录都没有」就是「比没有日志」的硬证据。不初始化 logger 的话
// 写入会退化成 stderr 兜底，测不到「没写」。
func TestLogStaleImpactWritesNothingOnEmptyIDs(t *testing.T) {
	baseDir := initTempLogger(t)
	svc := &Service{} // model 为 nil：任何漏掉空集合早退、真去取数的实现都会在这里现形
	ctx := context.Background()

	svc.logStaleImpact(ctx, "i18n", nil)
	svc.logStaleImpact(ctx, "i18n", []string{})
	svc.logStaleImpact(ctx, "i18n", []string{"", "   "})

	if _, err := os.Stat(filepath.Join(baseDir, "dependency")); !os.IsNotExist(err) {
		t.Fatalf("空集合不应产生任何日志行（dependency 场景目录已出现，err=%v）", err)
	}
}

// TestLogStaleImpactDegradesToCountOnly 摘要取不到时降级为「只记条数」，且总数是**去重后**的页面数。
//
// model 为 nil 的 Service 就是「摘要取不到」的构造：StaleImpactOfIDs 在 s.model == nil 时
// 返回 nil。这里同时钉住两件事：降级路径不 panic、不阻断（本函数没有返回值），
// 以及 affected 用的是聚合去重后的条数（传 3 条、其中 1 条重复 → 2）。
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

// initTempLogger 把全局 logger 指到 t.TempDir 下，返回 base_dir（场景目录是它的子目录）。
//
// 为什么值得在单测里初始化一个真实 logger：本文件的两条日志判据（空集合不记 / 降级只记条数）
// 只有在「能看到写入落点」时才可断言；不初始化就写到了一处测试看不见的地方，
// 断言会退化成「什么也没验证」。
func initTempLogger(t *testing.T) string {
	t.Helper()
	baseDir := filepath.Join(t.TempDir(), "logs")
	v := viper.New()
	v.Set("log.base_dir", baseDir)
	if err := logger.Init(v); err != nil {
		t.Fatalf("初始化临时 logger 失败: %v", err)
	}
	// 用例内部可能已经 Close（读文件前需要落盘），这里再 Close 一次是幂等的。
	t.Cleanup(func() { _ = logger.Close() })
	return baseDir
}

// readSceneLog 读取某场景目录下所有日志文件的非空行（按文件名次序拼接）。
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
