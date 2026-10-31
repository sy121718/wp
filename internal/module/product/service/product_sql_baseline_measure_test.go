package productservice_test

// product_sql_baseline_measure_test.go — 商品模块的**执行计划与规模基线测量工件**（PRD-018）。
//
// 这不是判据用例，是**测量仪器**：默认跳过，只有显式打开才跑；跑完只记录，不断言耗时。
// 打开方式：
//
//	GOWP_SQL_BASELINE=1 go test ./internal/module/product/service/ -run TestProductSQLBaselineExplain -v -count=1 -timeout 30m
//
// 可选环境变量：
//
//	GOWP_SQL_BASELINE_ROWS=10000   规模（默认 2000；1 / 100 / 1000 / 10000 / 100000 都可以）
//	GOWP_SQL_BASELINE_OUT=/tmp/x.log  另存一份到文件（人工复核与留档都要能看）
//	GOWP_SQL_BASELINE_IDS=1000     集合源喂多少个上线 id（默认 500）
//
// **诚实边界（结果里也写一遍）**：
//   - 库来自 support.NewMigratedPGTestDB：生产迁移建出的**隔离副本**，不碰业务库、不写业务库、
//     不建索引、不改表；
//   - 缓存是**热的**（同一次运行里反复 EXPLAIN）。本工件不伪造冷缓存，也不据此宣称生产延迟；
//     冷缓存效应只能在真实环境另测，且必须写明怎么测的；
//   - 统计信息来自灌完语料后的 ANALYZE（不 ANALYZE 的计划只是在测 planner 什么都不知道时的行为）；
//   - 小表 SeqScan 是**正确**计划，不是缺陷；只有规模档上去后仍然整表扫才是线索；
//   - 任何「加索引」结论都必须先有本工件的前后对比，且迁移只追加。

import (
	"context"
	"database/sql/driver"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

// baselineEnv 读一档正整数环境变量；缺省或非法时用 fallback。
func baselineEnv(t *testing.T, key string, fallback int) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("%s=%q 不是正整数", key, raw)
	}
	return n
}

// baselineCase 一档测量：标签 + SQL + 参数（参数个数本身就是证据之一）。
type baselineCase struct {
	label string
	sql   string
	args  []any
}

// TestProductSQLBaselineExplain 灌到指定规模 → ANALYZE → 逐条 EXPLAIN (ANALYZE, BUFFERS) 记录。
func TestProductSQLBaselineExplain(t *testing.T) {
	if os.Getenv("GOWP_SQL_BASELINE") != "1" {
		t.Skip("规模基线测量默认跳过：显式 GOWP_SQL_BASELINE=1 才运行（见本文件头注释）")
	}
	rows := baselineEnv(t, "GOWP_SQL_BASELINE_ROWS", 2000)
	liveIDs := baselineEnv(t, "GOWP_SQL_BASELINE_IDS", 500)
	if liveIDs > rows {
		liveIDs = rows
	}

	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "SQL 规模基线")

	// 第二个工程当噪声：整表只有一个工程时 project_id 谓词零选择率，planner 顺序扫描
	// 本来就是正确计划 —— 那种报告无法回答「工程谓词有没有被用上」。
	// 噪声规模取十分之一（至少 1 行），足以让选择性可见，又不至于把语料时间翻倍。
	noiseRows := rows / 10
	if noiseRows < 1 {
		noiseRows = 1
	}
	noiseProjectID := uuid.NewString()
	support.SeedProjectRow(t, db, noiseProjectID, "SQL 规模基线-噪声工程")

	started := time.Now()
	seedBaselineCorpus(t, db, projectID, rows)
	seedBaselineCorpus(t, db, noiseProjectID, noiseRows)
	t.Logf("语料灌入完成：目标工程 %d 个商品 + 噪声工程 %d 个商品（每商品 2 变体 / 1 评分 / 分类与标签各挂一个），耗时 %s",
		rows, noiseRows, time.Since(started).Round(time.Millisecond))

	// 统计信息必须跟上语料：否则测到的是「planner 猜」，不是真实规模下的计划。
	for _, tbl := range []string{"products", "product_variants", "product_ratings",
		"product_categories", "product_tags"} {
		if err := db.Exec("ANALYZE " + tbl).Error; err != nil {
			t.Fatalf("ANALYZE %s 失败：%v", tbl, err)
		}
	}

	logBaselineRelationSizes(t, db)
	logBaselineProjectShares(t, db)
	logBaselineIndexInventory(t, db)

	liveList := baselineProductIDs(t, db, projectID, liveIDs)
	t.Logf("集合源上线 id 数 = %d（按数组参数发送：绑定 1 个参数、%d 个元素；用于观察候选全集 IN 的计划形态）", len(liveList), len(liveList))

	// 报告只打开一次：早先每档各 os.Create 一次（还会截断），结果文件里只剩最后一档。
	out, closeOut := baselineWriter(t)
	defer closeOut()
	if _, err := fmt.Fprintf(out, "# 商品模块 SQL 规模基线（rows=%d，集合源 id=%d）\n\n", rows, len(liveList)); err != nil {
		t.Fatalf("写基线报告头失败：%v", err)
	}

	cases := baselineCases(t, db, projectID, rows, liveList)
	for _, c := range cases {
		plan, elapsed := explainAnalyze(t, db, c.sql, c.args...)
		// 参数个数按**展开后**的绑定算：IN (?) 传 500 个 id 会展开成 500 个 $n，
		// 只看 len(c.args) 会把大 IN 报成 1 个参数，正是本工件要说清楚的那件事。
		header := fmt.Sprintf("## %s（绑定参数 %d 个，%s）", c.label, expandedBindings(c.args), elapsed)
		if _, err := fmt.Fprintf(out, "%s\n%s\n\n", header, plan); err != nil {
			t.Fatalf("写基线报告失败（%s）：%v", c.label, err)
		}
		t.Logf("%s\n%s", header, plan)
	}
	verifyBaselineReport(t, len(cases))
}

// verifyBaselineReport 报告完整性自检：给了 GOWP_SQL_BASELINE_OUT 时读回文件，
// 确认每一档计划的标题都在。
//
// 为什么要在工件内部再查一遍：报告被截断过一次（每档各 os.Create 一次），而当时
// 测试日志里 11 档一条不少 —— 只看日志永远发现不了「留档只剩最后一档」。自检把
// 「报告可用」变成工件自己会失败的事，不依赖复核的人记得去数。
func verifyBaselineReport(t *testing.T, wantPlans int) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("GOWP_SQL_BASELINE_OUT"))
	if path == "" {
		return // 没留档就没得查：t.Logf 那份仍在控制台输出里
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回基线报告 %s 失败：%v", path, err)
	}
	if got := strings.Count(string(data), "## "); got != wantPlans {
		t.Fatalf("基线报告只留下 %d/%d 档计划（%s）：写句柄必须在循环外只开一次", got, wantPlans, path)
	}
}

// keywordPattern 与生产 productKeywordFilter 同一个 pattern 构造（两端通配、不转义）。
func keywordPattern(keyword string) string {
	return "%" + keyword + "%"
}

// baselineCases 代表性查询清单。每一条都对应生产里真实存在的读法，
// 并在标签里写明它服务于哪一屏（将来复核才知道为什么测这一条）。
func baselineCases(t *testing.T, db *gorm.DB, projectID string, rows int, liveList []string) []baselineCase {
	t.Helper()
	listProjection := "SELECT id, project_id, name, slug, sku_code, status, images, images_alt, " +
		"attribute_ids, category_ids, primary_category_id, brand_id, tag_ids, sort, " +
		"default_image, default_price, create_time, update_time FROM products " +
		"WHERE project_id = ? ORDER BY sort ASC, create_time DESC, id ASC"

	return []baselineCase{
		{
			label: "后台列表 count（工程）",
			sql:   "SELECT count(*) FROM products WHERE project_id = ?",
			args:  []any{projectID},
		},
		{
			label: "后台列表本页 LIMIT 20 OFFSET 0",
			sql:   listProjection + " LIMIT 20 OFFSET 0",
			args:  []any{projectID},
		},
		{
			label: "后台列表深页 LIMIT 20 OFFSET（总数一半）",
			sql:   listProjection + " LIMIT 20 OFFSET " + strconv.Itoa(rows/2),
			args:  []any{projectID},
		},
		{
			// name 有 trigram、sku_code 没有：这一档就是「一个字段有索引、另一个没有」的真实形态，
			// 别在报告里把它写成「关键词都能走索引」。
			//
			// pattern 必须与生产 productKeywordFilter（product_model.go:622-628）逐字同源：
			// 那里是 "%" + keyword + "%"、**不带** ESCAPE（PRD-005 待定口径）。这里改成
			// 转义版就会测出一条生产根本不发的 SQL，报告再漂亮也没意义。
			// 两个字段绑**同一个** keyword：生产 productKeywordFilter 就是这么发的
			// （一个输入框、两个字段）。早先这里一个 "%商品 1%" 一个 "%SKU-1%"，
			// 测出来的是「两个不同字面量」的合成形态，不能说成与生产同源。
			label: "后台列表关键词 count（name ILIKE OR sku_code ILIKE，无转义）",
			sql:   "SELECT count(*) FROM products WHERE project_id = ? AND (name ILIKE ? OR sku_code ILIKE ?)",
			args:  []any{projectID, keywordPattern("商品 1"), keywordPattern("商品 1")},
		},
		{
			label: "发布记录本页投影 LIMIT 20 OFFSET 0（只读四列）",
			sql: "SELECT id, project_id, name, slug FROM products WHERE project_id = ? " +
				"ORDER BY sort ASC, create_time DESC, id ASC LIMIT 20 OFFSET 0",
			args: []any{projectID},
		},
		{
			// 集合源 count：状态 + 大 IN。PRD-007 的成本就在这里（上线 id 全集进 IN）。
			label: "集合源 count（status + 上线 id 全集数组参数）",
			sql:   "SELECT count(*) FROM products WHERE project_id = ? AND status = ? AND products.id = ANY((?::text[])::uuid[])",
			args:  []any{projectID, "published", baselineUUIDArray(liveList)},
		},
		{
			// 集合源本页：含 min_price 与评分子查询（与 collectionQuery 同形）。
			label: "集合源本页（min_price + 评分子查询）LIMIT 20",
			sql: "SELECT id, project_id, name, subtitle, description, slug, status, sort, unit, " +
				"images, images_alt, attribute_ids, category_ids, tag_ids, brand_id, related_ids, " +
				"default_image, create_time, update_time, " +
				"(SELECT MIN(v.price) FROM product_variants v WHERE v.product_id = products.id AND v.enabled) AS min_price, " +
				"(SELECT AVG(r.score) FROM product_ratings r WHERE r.product_id = products.id) AS rating_avg, " +
				"(SELECT COUNT(*)::int FROM product_ratings r WHERE r.product_id = products.id) AS rating_count " +
				"FROM products WHERE project_id = ? AND status = ? AND products.id = ANY((?::text[])::uuid[]) " +
				// 排序与 ListForCollection 的 newest 档逐字相同（含唯一尾键 id）：
				// 少了 id ASC 就测不到生产那条 ORDER BY，深页计划也就没有可比性。
				"ORDER BY create_time DESC, id ASC LIMIT 20 OFFSET 0",
			args: []any{projectID, "published", baselineUUIDArray(liveList)},
		},
		{
			// jsonb 挂载：@> 必须走 GIN（jsonb_path_ops），计划里应看到 Bitmap Index Scan。
			// 工程谓词不能省：生产 collectionQuery 恒带 project_id（经 RLS 事务 + 显式条件）。
			// 少了它测到的是「全库按挂载筛」，计划与真实入口无关。
			label: "jsonb 分类挂载 @>",
			sql:   "SELECT count(*) FROM products WHERE project_id = ? AND category_ids @> jsonb_build_array(?::text)",
			args:  []any{projectID, baselineAttachID(t, db, "product_categories", projectID)},
		},
		{
			label: "jsonb 标签挂载 @>",
			sql:   "SELECT count(*) FROM products WHERE project_id = ? AND tag_ids @> jsonb_build_array(?::text)",
			args:  []any{projectID, baselineAttachID(t, db, "product_tags", projectID)},
		},
		{
			// 变体批量：发布记录 / 列表价格区间都走这一条（IN + product_id 索引）。
			label: "变体按商品 id 批量取全行",
			sql:   "SELECT * FROM product_variants WHERE product_id IN (?) ORDER BY sort ASC, id ASC",
			args:  []any{liveList},
		},
		{
			label: "变体启用价聚合（min / max 同批）",
			sql:   "SELECT product_id, MIN(price), MAX(price) FROM product_variants WHERE product_id IN (?) AND enabled GROUP BY product_id",
			args:  []any{liveList},
		},
	}
}

// TestProductSQLBaselineCasesMatchProduction 基线里的 SQL 必须与**生产模型真发的那条**同源。
//
// 为什么值得独立一条用例：手抄 SQL 的测量工件最容易失效的方式不是写错语法，而是慢慢漂——
// 生产改了排序尾键、改了 pattern，报告照样「全部 PASS」，因为它测的已经不是线上那条。
// 这里用 GORM 的 Query 回调抓生产真实的 Statement.SQL / Vars，逐项与 baselineCases 对照：
//
//   - 关键词：末两个绑定值必须与生产 productKeywordFilter 一致（同一 keyword、不转义）；
//   - 集合源本页 / 发布记录本页 / 变体批量：ORDER BY 子句必须逐字相同（含唯一尾键）。
//
// 抓的是**生产模型**（productmodel）而不是重新拼一遍字符串——那才是「同源」的定义。
// 它不灌大语料，20 个商品够用；PG 不可用时按仓规 t.Skip。
func TestProductSQLBaselineCasesMatchProduction(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "基线口径同源")
	seedBaselineCorpus(t, db, projectID, 20)
	ids := baselineProductIDs(t, db, projectID, 10)

	var capturedSQL string
	var capturedVars []any
	if err := db.Callback().Query().After("gorm:query").Register("baseline:capture",
		func(tx *gorm.DB) {
			capturedSQL = tx.Statement.SQL.String()
			capturedVars = append([]any(nil), tx.Statement.Vars...)
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}
	m := productmodel.NewModel(db)
	ctx := context.Background()
	cases := baselineCases(t, db, projectID, 20, ids)

	t.Run("关键词 pattern 与生产同源", func(t *testing.T) {
		c := baselineCaseByLabel(t, cases, "关键词 count")
		capturedSQL, capturedVars = "", nil
		if _, err := m.Count(ctx, projectID, "商品 1", ""); err != nil {
			t.Fatalf("生产 Count 失败：%v", err)
		}
		if capturedSQL == "" {
			t.Fatal("没抓到生产 SQL（回调没生效）")
		}
		// 工程作用域在生产里走 set_config，不进绑定参数；这里只比关键词那末两位。
		want, got := tailStrings(c.args, 2), tailStrings(capturedVars, 2)
		if !equalStrings(want, got) {
			t.Fatalf("关键词 pattern 不同源：基线 %v / 生产 %v", want, got)
		}
		if want[0] != want[1] {
			t.Fatalf("基线关键词两字段应是同一个 pattern（%v），当前不是", want)
		}
	})

	for _, tc := range []struct {
		label string
		run   func() error
	}{
		{label: "集合源本页",
			run: func() error {
				_, err := m.ListForCollection(ctx, productmodel.CollectionFilter{
					ProjectID: projectID, Status: "published", IDs: ids, Order: "newest",
				}, 20, 0)
				return err
			}},
		{label: "发布记录本页投影",
			run: func() error {
				_, err := m.ListForPublishRecordsPage(ctx, projectID, "", 20, 0)
				return err
			}},
		{label: "变体按商品 id 批量取全行",
			run: func() error {
				_, err := m.ListVariantsByProducts(ctx, ids)
				return err
			}},
	} {
		t.Run(tc.label+" 排序同源", func(t *testing.T) {
			c := baselineCaseByLabel(t, cases, tc.label)
			capturedSQL, capturedVars = "", nil
			if err := tc.run(); err != nil {
				t.Fatalf("生产查询失败：%v", err)
			}
			want, got := orderClause(c.sql), orderClause(capturedSQL)
			if want == "" || got == "" {
				t.Fatalf("ORDER BY 子句没解析出来：基线 %q / 生产 %q", want, got)
			}
			if want != got {
				t.Fatalf("排序不同源：基线 ORDER BY %q / 生产 ORDER BY %q", want, got)
			}
		})
	}
}

// baselineCaseByLabel 按标签片段取一档测量（标签是人工写的，同一片段必须唯一命中）。
func baselineCaseByLabel(t *testing.T, cases []baselineCase, substr string) baselineCase {
	t.Helper()
	var hits []baselineCase
	for _, c := range cases {
		if strings.Contains(c.label, substr) {
			hits = append(hits, c)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("按 %q 应唯一命中一档测量，实际 %d 档", substr, len(hits))
	}
	return hits[0]
}

// orderClause 取 SQL 里的 ORDER BY 子句（到 LIMIT / OFFSET / 结尾为止），去掉首尾空白。
func orderClause(sql string) string {
	_, tail, ok := strings.Cut(sql, "ORDER BY ")
	if !ok {
		return ""
	}
	upper := strings.ToUpper(tail)
	end := len(tail)
	for _, stop := range []string{" LIMIT ", " OFFSET "} {
		if i := strings.Index(upper, stop); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimSpace(tail[:end])
}

// tailStrings 取绑定参数末 n 个（转成字符串便于比对：类型差异不是本判断的对象）。
func tailStrings(args []any, n int) []string {
	if len(args) < n {
		return nil
	}
	out := make([]string, 0, n)
	for _, a := range args[len(args)-n:] {
		out = append(out, fmt.Sprint(a))
	}
	return out
}

// equalStrings 两个字符串切片逐项相等。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// explainAnalyze 跑 EXPLAIN (ANALYZE, BUFFERS) 并返回完整计划与 Execution Time 行。
//
// 为什么带 ANALYZE：只看 EXPLAIN 拿不到实际行数（rows 是估计值），而「估计 1 行、实际 5 万行」
// 正是最要紧的线索。代价是它真的执行这条读查询 —— 只在隔离副本上跑，所以可接受。
func explainAnalyze(t *testing.T, db *gorm.DB, sql string, args ...any) (string, string) {
	t.Helper()
	rows, err := db.Raw("EXPLAIN (ANALYZE, BUFFERS) "+sql, args...).Rows()
	if err != nil {
		t.Fatalf("EXPLAIN 失败（%s）：%v", firstLine(sql), err)
	}
	defer func() { _ = rows.Close() }()
	lines := make([]string, 0, 32)
	elapsed := "(计划里未出现 Execution Time)"
	for rows.Next() {
		var line string
		if serr := rows.Scan(&line); serr != nil {
			t.Fatalf("读取执行计划失败：%v", serr)
		}
		lines = append(lines, line)
		if strings.HasPrefix(strings.TrimSpace(line), "Execution Time:") {
			elapsed = strings.TrimSpace(line)
		}
	}
	return strings.Join(lines, "\n"), elapsed
}

// firstLine 取 SQL 首行用于失败信息（完整 SQL 太长，但首行够定位是哪一档）。
func firstLine(sql string) string {
	if i := strings.IndexByte(sql, '\n'); i >= 0 {
		return sql[:i]
	}
	if len(sql) > 120 {
		return sql[:120] + "…"
	}
	return sql
}

// baselineWriter 结果落盘：给了 GOWP_SQL_BASELINE_OUT 就另存一份文件，否则退化成 /dev/null
// （t.Logf 那份输出照样有）。文件打不开直接判失败 —— 静默丢档等于没测。
//
// **只调用一次**，把写句柄传下去复用：早先每档各调一次 os.Create（还会截断），
// 11 档计划写进同一个路径时只剩最后一档，报告看起来「测了」其实是残档。
// 关闭交给返回的 close（检查 flush/close 错误：磁盘满时错误只在这里冒出来）。
func baselineWriter(t *testing.T) (*os.File, func()) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("GOWP_SQL_BASELINE_OUT"))
	if path == "" {
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("打开 %s 失败：%v", os.DevNull, err)
		}
		return f, func() { _ = f.Close() }
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建基线输出文件 %s 失败：%v", path, err)
	}
	return f, func() {
		if cerr := f.Close(); cerr != nil {
			t.Fatalf("关闭基线输出文件 %s 失败：%v", path, cerr)
		}
	}
}

// expandedBindings 统计**展开后**的绑定参数个数。
//
// PG 的 IN 列表在 database/sql 里会展开成 $1..$n：args 里一个 []string{500 个 id}
// 到服务端是 500 个参数。只看 len(args) 会把「大 IN」这条最关键的规模事实抹平成 1。
func expandedBindings(args []any) int {
	n := 0
	for _, a := range args {
		switch v := a.(type) {
		case []string:
			n += len(v)
		case []any:
			n += len(v)
		case baselineUUIDArrayParam:
			// 数组参数在协议里是 **1 个**绑定参数，元素个数另算 —— 两个数都要报，
			// 否则「集合源喂了 500 个 id」会被读成「绑了 500 个参数」。
			n++
		default:
			n++
		}
	}
	return n
}

// baselineUUIDArrayParam 与生产 productmodel.textArrayParam **同形态**的数组入参。
//
// 为什么必须自己产出 PG 数组字面量：裸 []string 经 GORM/pgx 会被编码成**元组**
// ('a','b')，PG 报 malformed array literal (22P02)；而生产 collectionQuery 现在的写法
// 是 products.id = ANY((?::text[])::uuid[])，绑的是一个数组参数、不是 IN 列表。
// 基线这里要是继续用 []string 假装 IN，测到的就是一条生产**不会发**的 SQL，
// 报告再漂亮也不对应线上任何一次请求。
type baselineUUIDArrayParam []string

// Value 实现 driver.Valuer：产出 PG 数组字面量文本（{"a","b"}）。
func (a baselineUUIDArrayParam) Value() (driver.Value, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, id := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(id))
	}
	b.WriteByte('}')
	return b.String(), nil
}

// baselineUUIDArray 把 id 清单包成上面那种数组参数。
func baselineUUIDArray(ids []string) baselineUUIDArrayParam { return baselineUUIDArrayParam(ids) }

// logBaselineRelationSizes 记录表体量（行数与堆 / 索引字节）。没有这堆数字，计划没法被复核。
func logBaselineRelationSizes(t *testing.T, db *gorm.DB) {
	t.Helper()
	var got struct {
		Products   int64
		Variants   int64
		Ratings    int64
		HeapBytes  int64
		IndexBytes int64
	}
	err := db.Raw("SELECT (SELECT count(*) FROM products) AS products, " +
		"(SELECT count(*) FROM product_variants) AS variants, " +
		"(SELECT count(*) FROM product_ratings) AS ratings, " +
		"(SELECT pg_relation_size('products')) AS heap_bytes, " +
		"(SELECT pg_indexes_size('products')) AS index_bytes").Scan(&got).Error
	if err != nil {
		t.Fatalf("读取表体量失败：%v", err)
	}
	t.Logf("体量：products=%d 行（堆 %d B / 索引 %d B）、variants=%d 行、ratings=%d 行",
		got.Products, got.HeapBytes, got.IndexBytes, got.Variants, got.Ratings)
}

// logBaselineProjectShares 记录各工程占 products 的比例。
//
// 报告里每一条计划都带 project_id 谓词，读的人必须知道这个谓词的选择率才能判断计划是否合理：
// 目标工程占全表九成时 SeqScan 是对的，占一成时还 SeqScan 才是线索。
func logBaselineProjectShares(t *testing.T, db *gorm.DB) {
	t.Helper()
	var got []struct {
		ProjectID string
		N         int64
	}
	if err := db.Raw("SELECT project_id, count(*) AS n FROM products GROUP BY project_id ORDER BY n DESC").
		Scan(&got).Error; err != nil {
		t.Fatalf("读取工程分布失败：%v", err)
	}
	total := int64(0)
	for _, r := range got {
		total += r.N
	}
	if total == 0 {
		t.Fatalf("语料为空：products 一行都没有")
	}
	for _, r := range got {
		t.Logf("工程 %s：%d 行（占全表 %.1f%%）", r.ProjectID, r.N, float64(r.N)*100/float64(total))
	}
}

// logBaselineIndexInventory 记录 products / product_variants 当前索引清单：
// 报告里写「索引 X 覆盖了这个谓词」时，复核的人要能确认 X 当时真的存在、形态就是说那个。
func logBaselineIndexInventory(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		Tablename string
		Indexname string
		Indexdef  string
	}
	err := db.Raw("SELECT tablename, indexname, indexdef FROM pg_indexes " +
		"WHERE schemaname = 'public' AND tablename IN ('products', 'product_variants') " +
		"ORDER BY tablename, indexname").Scan(&rows).Error
	if err != nil {
		t.Fatalf("读取索引清单失败：%v", err)
	}
	for _, r := range rows {
		t.Logf("索引 %s.%s：%s", r.Tablename, r.Indexname, r.Indexdef)
	}
}

// seedBaselineCorpus 灌一档规模的代表性语料：商品 + 每商品 2 个变体（一启用一停用）
// + 分类/标签各 20 个挂载目标（按序轮流挂）+ 每商品 1 条评分。
//
// 全部取值由序号决定（sort 递增、价格由序号推、挂载按序轮流）：同一规模跑两次得到同一个库形态。
// 这不是随机语料 —— 可复现是这类工件的前提。
func seedBaselineCorpus(t *testing.T, db *gorm.DB, projectID string, rows int) {
	t.Helper()
	// 挂载目标各 20 条：够覆盖 @> 的 GIN 形态（每目标约命中 rows/20 行），又不至让语料本身成瓶颈。
	if err := db.Exec("INSERT INTO product_categories (id, project_id, name, slug, sort, create_time, update_time) "+
		"SELECT gen_random_uuid(), ?::uuid, '分类 ' || i, 'cat-' || i, i, now(), now() "+
		"FROM generate_series(0, 19) AS i", projectID).Error; err != nil {
		t.Fatalf("灌分类失败：%v", err)
	}
	if err := db.Exec("INSERT INTO product_tags (id, project_id, name, slug, sort, create_time, update_time) "+
		"SELECT gen_random_uuid(), ?::uuid, '标签 ' || i, 'tag-' || i, i, now(), now() "+
		"FROM generate_series(0, 19) AS i", projectID).Error; err != nil {
		t.Fatalf("灌标签失败：%v", err)
	}

	// 商品：一条语句灌完（10000 行也只是几秒）；分类 / 标签挂载按 i%20 轮流。
	if err := db.Exec("WITH cats AS (SELECT array_agg(id ORDER BY sort) AS a FROM product_categories WHERE project_id = ?::uuid), "+
		"tags AS (SELECT array_agg(id ORDER BY sort) AS a FROM product_tags WHERE project_id = ?::uuid) "+
		"INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time, "+
		"default_price, category_ids, tag_ids) "+
		"SELECT gen_random_uuid(), ?::uuid, '商品 ' || i, 'p-' || i, 'SKU-' || i, 'published', i, "+
		"now() - (i || ' minutes')::interval, now(), 100 + i, "+
		"jsonb_build_array(cats.a[1 + (i % 20)]), jsonb_build_array(tags.a[1 + (i % 20)]) "+
		"FROM generate_series(1, ?) AS i, cats, tags", projectID, projectID, projectID, rows).Error; err != nil {
		t.Fatalf("灌商品 %d 行失败：%v", rows, err)
	}

	// 变体：每商品两行（启用 + 停用），用 products 做源，不在 Go 侧搬 id。
	if err := db.Exec("INSERT INTO product_variants (id, product_id, sku_code, price, compare_price, enabled, sort, create_time, update_time) "+
		"SELECT gen_random_uuid(), p.id, p.sku_code || '-A', p.default_price, p.default_price * 1.2, true, 1, now(), now() "+
		"FROM products p WHERE p.project_id = ?::uuid", projectID).Error; err != nil {
		t.Fatalf("灌启用变体失败：%v", err)
	}
	if err := db.Exec("INSERT INTO product_variants (id, product_id, sku_code, price, enabled, sort, create_time, update_time) "+
		"SELECT gen_random_uuid(), p.id, p.sku_code || '-B', p.default_price * 0.8, false, 2, now(), now() "+
		"FROM products p WHERE p.project_id = ?::uuid", projectID).Error; err != nil {
		t.Fatalf("灌停用变体失败：%v", err)
	}

	// 评分：每商品一条（评分聚合子查询要真能聚合到东西）。
	if err := db.Exec("INSERT INTO product_ratings (id, project_id, product_id, score, source, create_time, update_time) "+
		"SELECT gen_random_uuid(), ?::uuid, p.id, 1 + (p.sort % 5), 'manual', now(), now() "+
		"FROM products p WHERE p.project_id = ?::uuid", projectID, projectID).Error; err != nil {
		t.Fatalf("灌评分失败：%v", err)
	}
}

// baselineProductIDs 取本工程前 n 个商品 id（按模型排序口径，保证可复现）。
func baselineProductIDs(t *testing.T, db *gorm.DB, projectID string, n int) []string {
	t.Helper()
	var ids []string
	if err := db.Raw("SELECT id FROM products WHERE project_id = ? "+
		"ORDER BY sort ASC, create_time DESC, id ASC LIMIT ?", projectID, n).Scan(&ids).Error; err != nil {
		t.Fatalf("取上线 id 失败：%v", err)
	}
	return ids
}

// baselineAttachID 取一条挂载目标 id（@> 用例的参数）。
func baselineAttachID(t *testing.T, db *gorm.DB, table, projectID string) string {
	t.Helper()
	var id string
	if err := db.Raw("SELECT id FROM "+table+" WHERE project_id = ? ORDER BY sort ASC, id ASC LIMIT 1",
		projectID).Scan(&id).Error; err != nil {
		t.Fatalf("取 %s 挂载目标失败：%v", table, err)
	}
	if id == "" {
		t.Fatalf("%s 没有可用挂载目标（语料没灌上）", table)
	}
	return id
}
