package feature

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

func TestTagWritesKeepInterleavedDefinition(t *testing.T) {
	for _, operation := range []string{"重算", "编辑"} {
		t.Run(operation, func(t *testing.T) {
			f := newPricingFixture(t)
			if f == nil {
				return
			}
			ctx := context.Background()
			tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "原规则", Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange, RuleParams: []byte(`{"minPrice":100,"maxPrice":200}`)})
			if err != nil {
				t.Fatal(err)
			}
			fired := afterProductSnapshot(t, f.db, "product_tags", func() error {
				name, kind := "并发改名", productenums.TagKindManual
				_, err := f.svc.UpdateTag(ctx, &productdto.UpdateTagReq{ProjectID: f.projectID, ID: tag.ID, Name: &name, Kind: &kind})
				return err
			})
			if operation == "重算" {
				_, err = f.svc.RecalcTags(ctx, &productdto.RecalcTagsReq{ProjectID: f.projectID, TagID: tag.ID})
			} else {
				sort := 7
				_, err = f.svc.UpdateTag(ctx, &productdto.UpdateTagReq{ProjectID: f.projectID, ID: tag.ID, Sort: &sort})
			}
			if err != nil {
				t.Fatal(err)
			}
			var actual productmodel.ProductTagEntity
			if err := f.db.First(&actual, "id = ?", tag.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !*fired || actual.Name != "并发改名" || actual.Kind != productenums.TagKindManual || actual.RuleType != "" {
				t.Fatalf("标签写入覆盖并发定义: fired=%v name=%s kind=%s rule=%s", *fired, actual.Name, actual.Kind, actual.RuleType)
			}
		})
	}
}

func TestRecalcAllTagsRollsBackTogether(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	for _, name := range []string{"规则一", "规则二"} {
		_, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: name, Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange, RuleParams: []byte("{\"minPrice\":100,\"maxPrice\":200}")})
		if err != nil {
			t.Fatal(err)
		}
	}
	var before, after []productmodel.ProductTagEntity
	if err := f.db.Where("project_id = ?", f.projectID).Order("id ASC").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected second tag failure")
	calls := 0
	const callback = "test:second_tag_failure"
	if err := f.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "product_tags" {
			calls++
			if calls == 2 {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Update().Remove(callback)
	_, err := f.svc.RecalcTags(ctx, &productdto.RecalcTagsReq{ProjectID: f.projectID})
	if !errors.Is(err, injected) {
		t.Fatalf("未返回第二条标签的故障: %v", err)
	}
	if err := f.db.Where("project_id = ?", f.projectID).Order("id ASC").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("批量重算失败后仍提交了部分标签")
	}
}

func TestConcurrentProductWritesAndRuleCreationKeepFinalMembership(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	a := pfCreateProduct(t, f, "并发甲", 500, 200)
	b := pfCreateProduct(t, f, "并发乙", 600, 200)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, 4)
	writes := []func() error{
		func() error {
			price := 150.0
			_, err := f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ProjectID: f.projectID, ID: a.Variants[0].ID, Price: &price})
			return err
		},
		func() error {
			price := 175.0
			_, err := f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ProjectID: f.projectID, ID: b.Variants[0].ID, Price: &price})
			return err
		},
		func() error {
			_, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":160}", "", productenums.PricingScopeProduct, a.ID)})
			return err
		},
		func() error {
			_, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "并发新规则", Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange, RuleParams: []byte("{\"minPrice\":100,\"maxPrice\":200}")})
			return err
		},
	}
	for _, write := range writes {
		go func(write func() error) { <-start; done <- write() }(write)
	}
	close(start)
	for range writes {
		if err := <-done; err != nil {
			t.Errorf("并发写入失败: %v", err)
		}
	}
	if t.Failed() {
		return
	}
	tags, err := f.svc.ListTags(ctx, &productdto.ListTagReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Fatalf("规则数错误: %d", len(tags))
	}
	rows, err := f.svc.ListTagProducts(ctx, &productdto.ListTagProductsReq{ProjectID: f.projectID, TagID: tags[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(rows.Items))
	for _, item := range rows.Items {
		got = append(got, item.ID)
	}
	want := []string{a.ID, b.ID}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("标签归属与最终价格不一致: got=%v want=%v", got, want)
	}
}

func TestProductWriteLockCanonicalizesProjectID(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	var keys []string
	const callback = "test:canonical_product_write_lock"
	if err := f.db.Callback().Raw().Before("gorm:raw").Register(callback, func(tx *gorm.DB) {
		for _, v := range tx.Statement.Vars {
			if key, ok := v.(string); ok && strings.HasPrefix(key, "product:write:") {
				keys = append(keys, key)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer f.db.Callback().Raw().Remove(callback)
	_, err := f.svc.CreateTag(context.Background(), &productdto.CreateTagReq{ProjectID: strings.ToUpper(f.projectID), Name: "规范锁键", Kind: productenums.TagKindRule, RuleType: productenums.TagRulePriceRange, RuleParams: []byte(`{"minPrice":100,"maxPrice":200}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("未捕获工程协调锁")
	}
	for _, key := range keys {
		if key != "product:write:"+strings.ToLower(f.projectID) {
			t.Fatalf("同工程使用不同锁键: %q", keys)
		}
	}
}
