package productmodel_test

// product_rating_summary_batch_test.go — 批量评分投影（RatingSummariesByProducts）的口径判据。
//
// 要钉住的是「同一商品的评分均值在列表页与详情页是同一个数」：批量路径**不做 SQL 侧
// AVG**，而是把评分明细按商品分组后交回 RatingSummary（详情页用的同一个函数），
// 所以判据写成「批量结果 == 逐商品 ListRatings + RatingSummary」，**逐位浮点相等**。
//
// 反过来说：谁把聚合搬进 SQL（GROUP BY / AVG(score)）或改掉组内顺序，本用例当场变红
// —— 310 组 score numeric(3,2) 样本实测，SQL 侧 AVG 有 8 例两位小数展示与逐条累加不同
// （例 scores=[4.49, 1.90]：逐条 3.19500000000000028422 → 展示 3.20，
// SQL 3.19499999999999984013 → 展示 3.19）。
//
// 表结构一律来自生产迁移（support.NewMigratedPGTestDB 复制模板库），不手抄 CREATE TABLE。

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

// insertProductRating 插一条评分（只补 NOT NULL 列，其余走表默认值）。
func insertProductRating(t *testing.T, db *gorm.DB, projectID, productID, score string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO product_ratings (id, project_id, product_id, score, source, create_time, update_time)
			VALUES (?, ?, ?, ?::numeric, 'manual', now(), now())`,
		uuid.NewString(), projectID, productID, score).Error; err != nil {
		t.Fatalf("插入评分失败：%v", err)
	}
}

// ratingScoreSets 评分集合矩阵：手工边界集 + 固定种子的随机集。
//
// 随机集不是装饰：逐条累加的浮点误差随条数与取值变化，边界集只覆盖到少数几个加法顺序，
// 而「组内顺序变了」这类缺陷正是靠大量随机组合才暴露出来。
func ratingScoreSets() map[string][]string {
	sets := map[string][]string{
		"单条":     {"4.15"},
		"两条边界":   {"4.00", "4.01"},
		"三条边界":   {"4.12", "4.13", "4.14"},
		"两位小数临界": {"4.49", "1.90"},
		"0.01三连": {"0.01", "0.02", "0.03"},
		"三条整数":   {"3.00", "3.00", "3.00"},
		"零分":     {"0.00"},
		"零分混合":   {"0.00", "0.00", "5.00"},
		"五条":     {"1.11", "2.22", "3.33", "4.44", "5.00"},
		"七条相同":   {"4.99", "4.99", "4.99", "4.99", "4.99", "4.99", "4.99"},
		"上界":     {"5.00"},
	}
	rng := rand.New(rand.NewSource(20261015))
	for i := 0; i < 300; i++ {
		n := 1 + rng.Intn(6)
		scores := make([]string, 0, n)
		for j := 0; j < n; j++ {
			v := rng.Intn(501) // 0..500 → 0.00~5.00，落在 CHECK (score <= 5) 内
			scores = append(scores, fmt.Sprintf("%d.%02d", v/100, v%100))
		}
		sets[fmt.Sprintf("随机#%03d", i)] = scores
	}
	return sets
}

// TestRatingSummariesByProductsMatchesDetailPath 批量投影与详情路径逐位一致，
// 且无评分 / 跨工程的商品不出现在结果里。
func TestRatingSummariesByProductsMatchesDetailPath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "批量评分口径")
	m := productmodel.NewModel(db)
	ctx := context.Background()

	type summary struct {
		avg   float64
		count int
	}
	expect := map[string]summary{}
	ids := make([]string, 0, 320)
	for name, scores := range ratingScoreSets() {
		id := uuid.NewString()
		ids = append(ids, id)
		for _, s := range scores {
			insertProductRating(t, db, projectID, id, s)
		}
		// 期望值就地从**详情路径**取回来（不是手算常数）：批量路径要对齐的正是它。
		list, err := m.ListRatings(ctx, id, projectID)
		if err != nil {
			t.Fatalf("逐商品读评分失败（%s）：%v", name, err)
		}
		items := make([]productmodel.ProductRatingEntity, 0, len(list))
		for _, r := range list {
			if r != nil {
				items = append(items, *r)
			}
		}
		avg, count, ok := productmodel.RatingSummary(items)
		if !ok {
			t.Fatalf("商品 %s 插了 %d 条评分却读不到明细：%v", name, len(scores), list)
		}
		expect[id] = summary{avg: avg, count: count}
	}

	// 无评分的商品：零值表示 = **不在 map 里**（行装配据此给 HasRating=false / RatingCount=0）。
	noRating := uuid.NewString()
	ids = append(ids, noRating)

	// 有评分但属于别的工程的商品：本连接是超级用户，RLS 不生效，
	// 「读不到」只能靠 SQL 里显式的 project_id 谓词 —— 这条就是跨项目读取的判据。
	otherProject := uuid.NewString()
	support.SeedProjectRow(t, db, otherProject, "别的工程")
	crossProject := uuid.NewString()
	insertProductRating(t, db, otherProject, crossProject, "5.00")
	ids = append(ids, crossProject)

	got, err := m.RatingSummariesByProducts(ctx, projectID, ids)
	if err != nil {
		t.Fatalf("批量读评分失败：%v", err)
	}
	if len(got) != len(expect) {
		t.Fatalf("返回 %d 个商品，期望恰好 %d 个（无评分与跨工程的都不该出现）", len(got), len(expect))
	}
	for id, want := range expect {
		gotOne, ok := got[id]
		if !ok {
			t.Fatalf("商品 %s 有评分却没出现在批量结果里", id)
		}
		if gotOne.Count != want.count {
			t.Fatalf("商品 %s 的评分条数：批量 %d、详情 %d", id, gotOne.Count, want.count)
		}
		if gotOne.Avg != want.avg {
			t.Fatalf("商品 %s 的均值与详情路径不一致：批量 %.20f、详情 %.20f（逐字一致是判据）",
				id, gotOne.Avg, want.avg)
		}
	}
	for _, id := range []string{noRating, crossProject} {
		if _, ok := got[id]; ok {
			t.Fatalf("商品 %s 不该出现在批量结果里（%v）", id, got[id])
		}
	}
}

// TestRatingSummariesByProductsEmptyInputIssuesNoQuery 空 / 全空串的 id 列表直接短路，
// 一条 SQL 都不发（不会拼出 IN ()）。
func TestRatingSummariesByProductsEmptyInputIssuesNoQuery(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "空 id 短路")

	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:rating_batch_count",
		func(*gorm.DB) { queries++ }); err != nil {
		t.Fatalf("注册查询计数回调失败：%v", err)
	}

	m := productmodel.NewModel(db)
	for _, ids := range [][]string{nil, {}, {"", "   ", ""}} {
		got, err := m.RatingSummariesByProducts(context.Background(), projectID, ids)
		if err != nil {
			t.Fatalf("空 id 列表不该报错：%v", err)
		}
		if len(got) != 0 {
			t.Fatalf("空 id 列表应返回空 map，实际 %v", got)
		}
	}
	if queries != 0 {
		t.Fatalf("空 id 列表不该发查询（IN () 是语法错），实际发了 %d 条", queries)
	}
}

// TestRatingSummariesByProductsReadFailureReturnsError 读库失败必须返回 error 并且不返回 map。
//
// 与页面语义的连线：列表页把这个 error 判成 500 失败页 —— 若这里改成「返回空 map 冒充成功」，
// 页面会把数据库故障渲染成「暂无评分」（dd.txt 3.5 验收表最后一行点名的那条）。
func TestRatingSummariesByProductsReadFailureReturnsError(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "读失败")
	productID := uuid.NewString()
	insertProductRating(t, db, projectID, productID, "4.50")

	m := productmodel.NewModel(db)
	sqlDB, derr := db.DB()
	if derr != nil {
		t.Fatalf("取底层连接失败：%v", derr)
	}
	if cerr := sqlDB.Close(); cerr != nil {
		t.Fatalf("关闭底层连接失败：%v", cerr)
	}

	got, err := m.RatingSummariesByProducts(context.Background(), projectID, []string{productID})
	if err == nil {
		t.Fatalf("库读不到时必须返回 error（不能返回 %v 冒充成功）", got)
	}
	if got != nil {
		t.Fatalf("失败时不该返回 map，实际 %v", got)
	}
}
