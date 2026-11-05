package productservice_test

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

type baselineCase struct {
	label string
	sql   string
	args  []any
}

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

	out, closeOut := baselineWriter(t)
	defer closeOut()
	if _, err := fmt.Fprintf(out, "# 商品模块 SQL 规模基线（rows=%d，集合源 id=%d）\n\n", rows, len(liveList)); err != nil {
		t.Fatalf("写基线报告头失败：%v", err)
	}

	cases := baselineCases(t, db, projectID, rows, liveList)
	for _, c := range cases {
		plan, elapsed := explainAnalyze(t, db, c.sql, c.args...)

		header := fmt.Sprintf("## %s（绑定参数 %d 个，%s）", c.label, expandedBindings(c.args), elapsed)
		if _, err := fmt.Fprintf(out, "%s\n%s\n\n", header, plan); err != nil {
			t.Fatalf("写基线报告失败（%s）：%v", c.label, err)
		}
		t.Logf("%s\n%s", header, plan)
	}
	verifyBaselineReport(t, len(cases))
}

func verifyBaselineReport(t *testing.T, wantPlans int) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("GOWP_SQL_BASELINE_OUT"))
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回基线报告 %s 失败：%v", path, err)
	}
	if got := strings.Count(string(data), "## "); got != wantPlans {
		t.Fatalf("基线报告只留下 %d/%d 档计划（%s）：写句柄必须在循环外只开一次", got, wantPlans, path)
	}
}

func keywordPattern(keyword string) string {
	return "%" + keyword + "%"
}

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

			label: "后台列表关键词 count（name ILIKE OR sku_code ILIKE，无转义）",
			sql:   "SELECT count(*) FROM products WHERE project_id = ? AND (name ILIKE ? OR sku_code ILIKE ?)",
			args:  []any{projectID, keywordPattern("商品 1"), keywordPattern("商品 1")},
		},
		{
			label: "发布记录整页候选 LIMIT 20 OFFSET 0（只读六列）",
			sql: "SELECT id, project_id, name, slug, sort, create_time FROM products WHERE project_id = ? " +
				"ORDER BY sort ASC, create_time DESC, id ASC LIMIT 20 OFFSET 0",
			args: []any{projectID},
		},
		{

			label: "集合源 count（status + 上线 id 全集数组参数）",
			sql:   "SELECT count(*) FROM products WHERE project_id = ? AND status = ? AND products.id = ANY((?::text[])::uuid[])",
			args:  []any{projectID, "published", baselineUUIDArray(liveList)},
		},
		{

			label: "集合源本页（min_price + 评分子查询）LIMIT 20",
			sql: "SELECT id, project_id, name, subtitle, description, slug, status, sort, unit, " +
				"images, images_alt, attribute_ids, category_ids, tag_ids, brand_id, related_ids, " +
				"default_image, create_time, update_time, " +
				"(SELECT MIN(v.price) FROM product_variants v WHERE v.product_id = products.id AND v.enabled) AS min_price, " +
				"(SELECT AVG(r.score) FROM product_ratings r WHERE r.product_id = products.id) AS rating_avg, " +
				"(SELECT COUNT(*)::int FROM product_ratings r WHERE r.product_id = products.id) AS rating_count " +
				"FROM products WHERE project_id = ? AND status = ? AND products.id = ANY((?::text[])::uuid[]) " +

				"ORDER BY create_time DESC, id ASC LIMIT 20 OFFSET 0",
			args: []any{projectID, "published", baselineUUIDArray(liveList)},
		},
		{

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

func TestProductSQLBaselineCasesMatchProduction(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "基线口径同源")
	seedBaselineCorpus(t, db, projectID, 20)
	ids := baselineProductIDs(t, db, projectID, 10)

	var capturedSQL string
	var capturedVars []any
	capture := func(tx *gorm.DB) {
		capturedSQL = tx.Statement.SQL.String()
		capturedVars = append([]any(nil), tx.Statement.Vars...)
	}
	if err := db.Callback().Query().After("gorm:query").Register("baseline:capture", capture); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}
	// 整页语句使用 Raw.Scan，必须从 Row 回调抓取，不能只监听普通查询。
	if err := db.Callback().Row().After("gorm:row").Register("baseline:capture", capture); err != nil {
		t.Fatalf("注册整页 SQL 抓取回调失败：%v", err)
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

		want, got := tailStrings(c.args, 2), tailStrings(capturedVars, 2)
		if !equalStrings(want, got) {
			t.Fatalf("关键词 pattern 不同源：基线 %v / 生产 %v", want, got)
		}
		if want[0] != want[1] {
			t.Fatalf("基线关键词两字段应是同一个 pattern（%v），当前不是", want)
		}
	})

	for _, tc := range []struct {
		label   string
		aliases []string
		run     func() error
	}{
		{label: "集合源本页",
			run: func() error {
				_, err := m.ListForCollection(ctx, productmodel.CollectionFilter{
					ProjectID: projectID, Status: "published", IDs: ids, Order: "newest",
				}, 20, 0)
				return err
			}},
		{label: "发布记录整页候选", aliases: []string{"c.", "pg."},
			run: func() error {
				_, err := m.QueryPublishRecordPage(ctx, projectID, "", "", 20, 1)
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
			aliases := tc.aliases
			if len(aliases) == 0 {
				aliases = []string{""}
			}
			// 分页层和最终输出层都要保留同一组排序键，不能只核对其中一处。
			for _, alias := range aliases {
				start := strings.Index(capturedSQL, "ORDER BY "+alias)
				if start < 0 {
					t.Fatalf("没抓到生产 ORDER BY %s：%s", alias, capturedSQL)
				}
				want, got := orderClause(c.sql), strings.ReplaceAll(orderClause(capturedSQL[start:]), alias, "")
				if want == "" || got == "" {
					t.Fatalf("ORDER BY 子句没解析出来：基线 %q / 生产 %q", want, got)
				}
				if want != got {
					t.Fatalf("排序不同源（%s）：基线 ORDER BY %q / 生产 ORDER BY %q", alias, want, got)
				}
			}
		})
	}
}

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

func firstLine(sql string) string {
	if i := strings.IndexByte(sql, '\n'); i >= 0 {
		return sql[:i]
	}
	if len(sql) > 120 {
		return sql[:120] + "…"
	}
	return sql
}

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

func expandedBindings(args []any) int {
	n := 0
	for _, a := range args {
		switch v := a.(type) {
		case []string:
			n += len(v)
		case []any:
			n += len(v)
		case baselineUUIDArrayParam:

			n++
		default:
			n++
		}
	}
	return n
}

type baselineUUIDArrayParam []string

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

func baselineUUIDArray(ids []string) baselineUUIDArrayParam { return baselineUUIDArrayParam(ids) }

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

func seedBaselineCorpus(t *testing.T, db *gorm.DB, projectID string, rows int) {
	t.Helper()

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

	if err := db.Exec("INSERT INTO product_ratings (id, project_id, product_id, score, source, create_time, update_time) "+
		"SELECT gen_random_uuid(), ?::uuid, p.id, 1 + (p.sort % 5), 'manual', now(), now() "+
		"FROM products p WHERE p.project_id = ?::uuid", projectID, projectID).Error; err != nil {
		t.Fatalf("灌评分失败：%v", err)
	}
}

func baselineProductIDs(t *testing.T, db *gorm.DB, projectID string, n int) []string {
	t.Helper()
	var ids []string
	if err := db.Raw("SELECT id FROM products WHERE project_id = ? "+
		"ORDER BY sort ASC, create_time DESC, id ASC LIMIT ?", projectID, n).Scan(&ids).Error; err != nil {
		t.Fatalf("取上线 id 失败：%v", err)
	}
	return ids
}

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
