package feature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	"gorm.io/gorm"
)

func TestPricingUnpriceableVariantRejectsEveryWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cost   float64
		reason string
	}{
		{"缺少成本", -1, "未填成本价"}, {"售价越界", 9999999999, "超过 9999999999.99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPricingFixture(t)
			if f == nil {
				return
			}
			p := pfCreateProduct(t, f, "完整定价", 100, 50)
			rejected := pfAddVariant(t, f, p.ID, "blocked-sku", 120, tc.cost)
			before := pfGetProduct(t, f, p.ID)
			batches := pfCountAdjustments(t, f)
			engine := gin.New()
			engine.POST("/apply", producthttp.NewProductHandle(f.svc).ApplyPricing)
			form := url.Values{"projectId": {f.projectID}, "ruleType": {productenums.PricingRuleCostMultiple}, "multiplier": {"2"}, "scope": {productenums.PricingScopeProduct}, "targetId": {p.ID}}
			rec := postForm(engine, "/apply", form)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), rejected.ID) || !strings.Contains(rec.Body.String(), rejected.SKUCode) || !strings.Contains(rec.Body.String(), tc.reason) || !strings.Contains(rec.Body.String(), "\"items\"") {
				t.Fatalf("应整批拒绝并返回定位明细：%d %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "SQLSTATE") || strings.Contains(rec.Body.String(), "\"message\":\"ErrPricing") {
				t.Fatal("不得透出内部错误原文")
			}
			_, err := f.svc.BulkApplyPricing(context.Background(), &productdto.BulkPricingReq{IDs: []string{p.ID}, ProjectID: f.projectID, Rule: pfReq(f, productenums.PricingRuleCostMultiple, "{\"multiplier\":2}", "", productenums.PricingScopeProduct, p.ID)})
			items := bulkRejected(t, err)
			if len(items) != 1 || items[0].ID != rejected.ID {
				t.Fatalf("批量入口也应拒绝同一变体：%+v", items)
			}
			for _, variant := range before.Variants {
				if got := pfPriceOf(t, f, variant.SKUCode); got != variant.Price {
					t.Fatalf("失败后价格改变：%s %v -> %v", variant.SKUCode, variant.Price, got)
				}
			}
			if got := pfCountAdjustments(t, f); got != batches {
				t.Fatalf("失败后产生调价批次：%d -> %d", batches, got)
			}
		})
	}
}

func TestPricingAutoTagFailureRollsBackPriceAndAudit(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	_, err := f.svc.CreateTag(context.Background(), &productdto.CreateTagReq{ProjectID: f.projectID, Name: "定价联动", Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange, RuleParams: []byte("{\"minPrice\":100,\"maxPrice\":200}")})
	if err != nil {
		t.Fatal(err)
	}
	p := pfCreateProduct(t, f, "事务商品", 500, 200)
	before := pfCountAdjustments(t, f)
	const callback = "test:pricing_tag_failure"
	if err := f.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "product_tags" {
			tx.AddError(errors.New("injected tag failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Update().Remove(callback)
	_, err = f.svc.ApplyPricing(context.Background(), &productdto.PricingApplyReq{PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":150}", "", productenums.PricingScopeProduct, p.ID)})
	if err == nil {
		t.Fatal("标签归属失败必须拒绝同一写操作")
	}
	if got := pfPriceOf(t, f, p.Variants[0].SKUCode); got != p.Variants[0].Price {
		t.Fatalf("价格未回滚：%v -> %v", p.Variants[0].Price, got)
	}
	if got := pfCountAdjustments(t, f); got != before {
		t.Fatalf("台账未回滚：%d -> %d", before, got)
	}
}

func TestPricingWaitsForVariantLockAndUsesCurrentCost(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	p := pfCreateProduct(t, f, "并发成本", 100, 50)
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Model(&productmodel.VariantEntity{}).Where("id = ?", p.Variants[0].ID).Update("cost_price", 80).Error; err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	var once sync.Once
	const callback = "test:pricing_variant_lock"
	if err := f.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "product_variants" {
			if _, ok := q.Statement.Clauses["FOR"]; ok {
				once.Do(func() { close(reached) })
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(callback)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	defer func() {
		cancel()
		tx.Rollback()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("取消后并发调价仍未退出")
		}
	}()
	go func() {
		defer close(finished)
		_, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{PricingRuleReq: pfReq(f, productenums.PricingRuleCostMultiple, "{\"multiplier\":2}", "", productenums.PricingScopeProduct, p.ID)})
		done <- err
	}()
	select {
	case <-reached:
	case err := <-done:
		t.Fatalf("锁前已结束：%v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("没有执行变体行锁")
	}
	select {
	case err := <-done:
		t.Fatalf("持锁时不得完成：%v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("释放锁后调价没有完成")
	}
	if got := pfPriceOf(t, f, p.Variants[0].SKUCode); got != 160 {
		t.Fatalf("应使用锁后的成本80计算，实际售价%v", got)
	}
}

func TestPricingFilterShrinkingWhileWaitingRejectsEveryWrite(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	first := pfCreateProduct(t, f, "入选商品甲", 100, 50)
	second := pfCreateProduct(t, f, "入选商品乙", 120, 60)
	before := pfCountAdjustments(t, f)
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Model(&productmodel.ProductEntity{}).Where("id = ?", first.ID).Update("name", "已退出筛选").Error; err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	var once sync.Once
	const callback = "test:pricing_scope_lock"
	if err := f.db.Callback().Query().Before("gorm:query").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "products" {
			if _, ok := q.Statement.Clauses["FOR"]; ok {
				once.Do(func() { close(reached) })
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Query().Remove(callback)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	defer func() {
		cancel()
		tx.Rollback()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("取消后并发调价仍未退出")
		}
	}()
	go func() {
		defer close(finished)
		rule := pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":180}", "", productenums.PricingScopeFilter, "")
		rule.Keyword = "入选商品"
		_, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{PricingRuleReq: rule})
		done <- err
	}()
	select {
	case <-reached:
	case err := <-done:
		t.Fatalf("锁前已结束：%v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("没有执行商品行锁")
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		items := bulkRejected(t, err)
		if len(items) != 1 || items[0].ID != first.Variants[0].ID {
			t.Fatalf("应定位退出筛选的变体：%+v", items)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("释放锁后调价没有完成")
	}
	for _, product := range []*productdto.ProductResp{first, second} {
		if got := pfPriceOf(t, f, product.Variants[0].SKUCode); got != product.Variants[0].Price {
			t.Fatalf("范围变化后不应修改任何价格：%s %v", product.ID, got)
		}
	}
	if got := pfCountAdjustments(t, f); got != before {
		t.Fatalf("范围变化后不应生成台账：%d -> %d", before, got)
	}
}
