package productmodel_test

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

func insertProductRating(t *testing.T, db *gorm.DB, projectID, productID, score string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO product_ratings (id, project_id, product_id, score, source, create_time, update_time)
			VALUES (?, ?, ?, ?::numeric, 'manual', now(), now())`,
		uuid.NewString(), projectID, productID, score).Error; err != nil {
		t.Fatalf("插入评分失败：%v", err)
	}
}

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
			v := rng.Intn(501)
			scores = append(scores, fmt.Sprintf("%d.%02d", v/100, v%100))
		}
		sets[fmt.Sprintf("随机#%03d", i)] = scores
	}
	return sets
}

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

	noRating := uuid.NewString()
	ids = append(ids, noRating)

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
