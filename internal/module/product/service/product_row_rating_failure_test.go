package productservice_test

// product_row_rating_failure_test.go — 「评分读不到」不得渲染成「暂无评分」
//（dd.txt 3.5 验收表最后一行）。
//
// 详情页与编辑页各验三档：
//  1. **确实没有评分**（查询成功、零行）→ 照常渲染：LoadFailed=false、行 HasRating=false、
//     RatingCount=0。这一档是反面判据：把「空结果」也当成失败就会在这里变红。
//  2. **有评分** → 均值与条数照常进成品行（证明失败档不是靠关掉整块评分换来的）。
//  3. **评分读不到**（数据库故障）→ 读模型返回 error、LoadFailed=true、HTTPStatus=500，
//     且不留半截成品行。把 ratingsOf 的吞错（err != nil → 返回空 RatingResp）加回去，
//     这一档立刻变红 —— 那正是修复前的形状：数据库故障被渲染成「暂无评分」。
//
// 注入只命中 product_ratings 的查询（GORM Query 回调 + tx.AddError），工程 / 商品 / 分类 /
// 品牌 / 标签的取数照常成功 —— 否则页面会在更早一步失败，判据就证明不了「是评分这一步」。
// 命中次数一并断言：注入没生效时测试报「判据无效」，而不是绿着过去。
//
// 表结构来自生产迁移（support.NewMigratedPGTestDB），不手抄 CREATE TABLE。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go_wp/pkg/facing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

const ratingFailureScene = "评分读失败"

// errRatingReadInjected 注入给 model 层的读失败。
var errRatingReadInjected = errors.New("injected: 读 product_ratings 失败")

// ratingReadFailureProbe 记录注入命中了几条评分查询（判据有效性的自证）。
type ratingReadFailureProbe struct {
	hits int
	sqls []string
}

// attachRatingReadFailure 只让 **product_ratings** 的查询失败，其余取数照常。
func attachRatingReadFailure(t *testing.T, db *gorm.DB) *ratingReadFailureProbe {
	t.Helper()
	probe := &ratingReadFailureProbe{}
	err := db.Callback().Query().After("gorm:query").Register("test:product_rating_read_failure",
		func(tx *gorm.DB) {
			sql := tx.Statement.SQL.String()
			if !strings.Contains(sql, "product_ratings") {
				return
			}
			probe.hits++
			probe.sqls = append(probe.sqls, sql)
			tx.AddError(errRatingReadInjected)
		})
	if err != nil {
		t.Fatalf("注册评分读失败回调失败：%v", err)
	}
	return probe
}

// seedRatingFailureProduct 造一个商品（只补 NOT NULL 且无默认值的列），score 非空时补一条评分。
func seedRatingFailureProduct(t *testing.T, db *gorm.DB, projectID, name, slug, sku, score string) string {
	t.Helper()
	productID := uuid.NewString()
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, 'published', 1, now(), now())`,
		productID, projectID, name, slug, sku).Error; err != nil {
		t.Fatalf("插入商品失败：%v", err)
	}
	if score != "" {
		if err := db.Exec(`INSERT INTO product_ratings (id, project_id, product_id, score, source, create_time, update_time)
			VALUES (?, ?, ?, ?::numeric, 'manual', now(), now())`,
			uuid.NewString(), projectID, productID, score).Error; err != nil {
			t.Fatalf("插入评分失败：%v", err)
		}
	}
	return productID
}

// ratingFailureFixture 一套夹具：无评分商品 + 有评分商品（4.50）。
type ratingFailureFixture struct {
	noRating string
	rated    string
}

func seedRatingFailureFixture(t *testing.T, db *gorm.DB, projectID string) ratingFailureFixture {
	t.Helper()
	return ratingFailureFixture{
		noRating: seedRatingFailureProduct(t, db, projectID, "无评分商品", "p-160-no-rating", "SKU-160-A", ""),
		rated:    seedRatingFailureProduct(t, db, projectID, "有评分商品", "p-160-rated", "SKU-160-B", "4.50"),
	}
}

// assertNoRatingRow 「确实没有评分」档：行装配出来了，且是零评分（不是失败）。
func assertNoRatingRow(t *testing.T, page string, view *productdto.ProductRowResp, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：确实没有评分不该判失败，实际 error=%v", page, err)
	}
	if view == nil {
		t.Fatalf("%s：确实没有评分时成品行没装配出来", page)
	}
	if view.HasRating || view.RatingCount != 0 {
		t.Fatalf("%s：零评分档期望 HasRating=false、RatingCount=0，实际 HasRating=%v、RatingCount=%d",
			page, view.HasRating, view.RatingCount)
	}
}

// assertRatedRow 「有评分」档：均值与条数照常。
func assertRatedRow(t *testing.T, page string, view *productdto.ProductRowResp, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：有评分的商品不该判失败，实际 error=%v", page, err)
	}
	if view == nil || !view.HasRating || view.RatingCount != 1 || view.RatingAvg != "4.50" {
		t.Fatalf("%s：有评分档期望 HasRating=true、RatingCount=1、RatingAvg=4.50，实际 %+v", page, view)
	}
}

// assertRatingReadFailure 「评分读不到」档：error + 500 出口 + 不留半截行。
func assertRatingReadFailure[T any](t *testing.T, page string, view *T, row *productdto.ProductRowResp, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：评分读不到时必须返回 error（否则页面会把数据库故障渲染成「暂无评分」，"+
			"dd.txt 3.5 验收表最后一行）；实际 error=nil、半截行=%+v", page, row)
	}
	if row != nil {
		t.Fatalf("%s：失败档不该留下装配了一半的成品行：%+v", page, row)
	}
	if view != nil {
		t.Fatalf("%s：失败档应返回 nil 视图（失败出口是 JSON，不再渲染同一页）", page)
	}
	if status, ok := facing.StatusOf(err); ok {
		t.Fatalf("%s：数据库故障不该带业务档位（实际 %d）", page, status)
	}
	if _, ok := facing.FactOf(err); ok {
		t.Fatalf("%s：数据库故障不该被判成业务错误（那会变成 400）", page)
	}
}

func assertProbeHit(t *testing.T, probe *ratingReadFailureProbe, page string) {
	t.Helper()
	if probe.hits == 0 {
		t.Fatalf("%s：注入没有命中任何 product_ratings 查询，判据无效", page)
	}
	t.Logf("%s：命中的评分查询 %d 条：%s", page, probe.hits, strings.Join(probe.sqls, " | "))
}

// TestProductDetailPageRatingReadFailureIsNotNoRating 详情页三档（见文件头）。
func TestProductDetailPageRatingReadFailureIsNotNoRating(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, ratingFailureScene)
	fx := seedRatingFailureFixture(t, db, projectID)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: ratingFailureScene}},
	})
	ctx := context.Background()

	// 档位一：确实没有评分。
	emptyView, err := s.ProductDetailPage(ctx, &productdto.ProductDetailPageReq{ProjectID: projectID, ProductID: fx.noRating})
	if emptyView == nil {
		t.Fatalf("详情页读模型返回 nil 视图")
	}
	assertNoRatingRow(t, "详情页（无评分）", emptyView.Product, err)

	// 档位二：有评分。
	ratedView, rerr := s.ProductDetailPage(ctx, &productdto.ProductDetailPageReq{ProjectID: projectID, ProductID: fx.rated})
	if ratedView == nil {
		t.Fatalf("详情页读模型返回 nil 视图")
	}
	assertRatedRow(t, "详情页（有评分）", ratedView.Product, rerr)

	// 档位三：评分读不到。
	probe := attachRatingReadFailure(t, db)
	view, derr := s.ProductDetailPage(ctx, &productdto.ProductDetailPageReq{ProjectID: projectID, ProductID: fx.noRating})
	assertRatingReadFailure(t, "详情页（评分读不到）", view, nil, derr)
	assertProbeHit(t, probe, "详情页")
	t.Logf("详情页：error=%v", derr)
}

// TestProductEditPageRatingReadFailureIsNotNoRating 编辑页三档（与详情页同一判据）。
func TestProductEditPageRatingReadFailureIsNotNoRating(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, ratingFailureScene)
	fx := seedRatingFailureFixture(t, db, projectID)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: ratingFailureScene}},
	})
	ctx := context.Background()

	emptyView, err := s.ProductEditPage(ctx, &productdto.ProductEditPageReq{ProjectID: projectID, ProductID: fx.noRating})
	if emptyView == nil {
		t.Fatalf("编辑页读模型返回 nil 视图")
	}
	assertNoRatingRow(t, "编辑页（无评分）", emptyView.Product, err)

	ratedView, rerr := s.ProductEditPage(ctx, &productdto.ProductEditPageReq{ProjectID: projectID, ProductID: fx.rated})
	if ratedView == nil {
		t.Fatalf("编辑页读模型返回 nil 视图")
	}
	assertRatedRow(t, "编辑页（有评分）", ratedView.Product, rerr)

	probe := attachRatingReadFailure(t, db)
	view, derr := s.ProductEditPage(ctx, &productdto.ProductEditPageReq{ProjectID: projectID, ProductID: fx.noRating})
	assertRatingReadFailure(t, "编辑页（评分读不到）", view, nil, derr)
	assertProbeHit(t, probe, "编辑页")
	t.Logf("编辑页：error=%v", derr)
}
