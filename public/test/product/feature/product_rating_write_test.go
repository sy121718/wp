package feature

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/facing"
)

// TestProductRatingDeleteEnqueuesInvalidation 验证硬删除评分后仍为同一商品新增失效事件。
func TestProductRatingDeleteEnqueuesInvalidation(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		t.Fatal("评分专项必须使用真实 PostgreSQL")
	}
	ctx := t.Context()
	product, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "删除评分", Slug: "rating-delete-event"})
	if err != nil {
		t.Fatal(err)
	}
	score := 4.5
	ratings, err := f.svc.AddRating(ctx, &productdto.AddRatingReq{ProjectID: f.projectID, ProductID: product.ID, Score: &score})
	if err != nil || len(ratings.Items) != 1 {
		t.Fatalf("评分前置失败：%v %+v", err, ratings)
	}
	model := productmodel.NewModel(f.db)
	before, err := model.ListOutboxEvents(ctx, "product", product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteRating(ctx, &productdto.DeleteRatingReq{ProjectID: f.projectID, ID: ratings.Items[0].ID}); err != nil {
		t.Fatal(err)
	}
	after, err := model.ListOutboxEvents(ctx, "product", product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+2 {
		t.Fatalf("删除必须与商品直接依赖、集合依赖两条事件同时落库：删除前 %d，删除后 %d", len(before), len(after))
	}
	ratings, err = f.svc.ListRatings(ctx, &productdto.ListRatingsReq{ProjectID: f.projectID, ProductID: product.ID})
	if err != nil || len(ratings.Items) != 0 || ratings.HasRating {
		t.Fatalf("删除后评分事实不符：%v %+v", err, ratings)
	}
}

// TestProductRatingWriteRollsBack 验证事件或结果读取失败均不能留下部分评分写入。
func TestProductRatingWriteRollsBack(t *testing.T) {
	for _, operation := range []string{"add", "delete"} {
		for _, failure := range []string{"outbox", "result"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				f := newAttrFixture(t)
				if f == nil {
					t.Fatal("评分专项必须使用真实 PostgreSQL")
				}
				ctx := t.Context()
				p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "评分回滚", Slug: "rating-rollback"})
				if err != nil {
					t.Fatal(err)
				}
				score := 4.5
				initial, err := f.svc.AddRating(ctx, &productdto.AddRatingReq{ProjectID: f.projectID, ProductID: p.ID, Score: &score})
				if err != nil {
					t.Fatal(err)
				}
				m := productmodel.NewModel(f.db)
				before, err := m.ListOutboxEvents(ctx, "product", p.ID)
				if err != nil {
					t.Fatal(err)
				}
				injected := errors.New("评分写入故障注入")
				name := "test:rating:" + operation + ":" + failure
				if failure == "outbox" {
					if err := f.db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
						if tx.Statement.Table == "product_outbox_events" {
							tx.AddError(injected)
						}
					}); err != nil {
						t.Fatal(err)
					}
					defer f.db.Callback().Create().Remove(name)
				} else {
					if err := f.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
						// GetRating 的单行查询仍可执行，只让写后的明细读取失败。
						if tx.Statement.Table == "product_ratings" {
							if _, ok := tx.Statement.Dest.(*[]*productmodel.ProductRatingEntity); ok {
								tx.AddError(injected)
							}
						}
					}); err != nil {
						t.Fatal(err)
					}
					defer f.db.Callback().Query().Remove(name)
				}
				var result *productdto.RatingResp
				if operation == "add" {
					result, err = f.svc.AddRating(ctx, &productdto.AddRatingReq{ProjectID: f.projectID, ProductID: p.ID, Score: &score})
				} else {
					result, err = f.svc.DeleteRating(ctx, &productdto.DeleteRatingReq{ProjectID: f.projectID, ID: initial.Items[0].ID})
				}
				if !errors.Is(err, injected) || result != nil {
					t.Fatalf("故障必须拒绝且不能交付成功事实：%v %+v", err, result)
				}
				// 移除注入后检查真实持久化终态，而不是只看返回错误。
				if failure == "outbox" {
					f.db.Callback().Create().Remove(name)
				} else {
					f.db.Callback().Query().Remove(name)
				}
				after, err := m.ListOutboxEvents(ctx, "product", p.ID)
				if err != nil || len(after) != len(before) {
					t.Fatalf("评分失败不得留下事件：%v before=%d after=%d", err, len(before), len(after))
				}
				got, err := f.svc.ListRatings(ctx, &productdto.ListRatingsReq{ProjectID: f.projectID, ProductID: p.ID})
				if err != nil || len(got.Items) != 1 || got.Items[0].ID != initial.Items[0].ID || got.Rating != initial.Rating {
					t.Fatalf("评分与事件必须一起回滚：%v %+v", err, got)
				}
			})
		}
	}
}

// TestProductRatingConcurrentDelete 验证同一评分并发删除仅成功一次且只产生一对事件。
func TestProductRatingConcurrentDelete(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		t.Fatal("评分专项必须使用真实 PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "并发评分", Slug: "rating-concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	score := 0.0
	rating, err := f.svc.AddRating(ctx, &productdto.AddRatingReq{ProjectID: f.projectID, ProductID: p.ID, Score: &score})
	if err != nil {
		t.Fatal(err)
	}
	m := productmodel.NewModel(f.db)
	before, err := m.ListOutboxEvents(ctx, "product", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			_, err := f.svc.DeleteRating(ctx, &productdto.DeleteRatingReq{ProjectID: f.projectID, ID: rating.Items[0].ID})
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if status, ok := facing.StatusOf(err); !ok || status != http.StatusNotFound {
			t.Fatalf("并发删除的另一请求应明确不存在，实际 %v", err)
		}
	}
	after, err := m.ListOutboxEvents(ctx, "product", p.ID)
	if err != nil || successes != 1 || len(after) != len(before)+2 {
		t.Fatalf("并发删除必须恰好成功一次并新增两事件：%v successes=%d before=%d after=%d", err, successes, len(before), len(after))
	}
}
