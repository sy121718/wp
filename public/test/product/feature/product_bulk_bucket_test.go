// product_bulk_bucket_test.go — 批量删除 / 批量改价的「整批或零」判据。
//
// 语义（见 internal/module/CLAUDE.md「列表页批量操作」）：一批 = 一个事务，任何一条不能处理
// 整批不生效，错误里列出可定位的条目（id + 名称 + 原因 key）。所以本文件钉的不是「哪几条被
// 跳过」，而是两件事：
//  1. 只要夹着一条不可处理的条目，**一行都不能动**（库里原样）；
//  2. 拒绝时逐条给 reasonKey，让操作者能定位到具体对象 —— 没有这条，错误就只是「批量失败」。
//
// 五类输入（重复 id、不存在 id、跨工程 id、被引用的对象：主分类真列 / 品牌真列 / 属性 jsonb /
// 标签跨工程、分类有子级）都在这里出现：批量引用扫描的谓词写错（例如 uuid 真列与 text 数组
// 比较）时，本文件直接变红，而不是静默全删。
package feature

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	json "go_wp/pkg/jsonx"
)

// rowCount 某张表里 id 还在不在（1 / 0）。
func rowCount(t *testing.T, db *gorm.DB, table, id string) int {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("统计 %s 失败：%v", table, err)
	}
	return int(n)
}

// jsonbList 读一行的 jsonb 字符串数组列（断言解绑之类的副作用时用）。
func jsonbList(t *testing.T, db *gorm.DB, table, id, col string) []string {
	t.Helper()
	var raw string
	if err := db.Table(table).Select("COALESCE("+col+"::text, '[]')").Where("id = ?", id).Scan(&raw).Error; err != nil {
		t.Fatalf("读 %s.%s 失败：%v", table, col, err)
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("解析 %s.%s（%q）失败：%v", table, col, raw, err)
	}
	return out
}

func setJSONB(t *testing.T, db *gorm.DB, productID, col, jsonLiteral string) {
	t.Helper()
	if err := db.Exec("UPDATE products SET "+col+" = ?::jsonb WHERE id = ?", jsonLiteral, productID).Error; err != nil {
		t.Fatalf("更新 products.%s 失败：%v", col, err)
	}
}

func setUUIDCol(t *testing.T, db *gorm.DB, productID, col, value string) {
	t.Helper()
	if err := db.Exec("UPDATE products SET "+col+" = ? WHERE id = ?", value, productID).Error; err != nil {
		t.Fatalf("更新 products.%s 失败：%v", col, err)
	}
}

// missingUUID 一个格式合法、库里不存在的 id。
const missingUUID = "11111111-1111-4111-8111-111111111111"

// bulkRejected 取整批拒绝的条目；没有错误或不是整批拒绝都直接判失败。
func bulkRejected(t *testing.T, err error) []productdto.BulkBlockedItemResp {
	t.Helper()
	if err == nil {
		t.Fatal("整批应被拒绝（夹着不能处理的条目），实际没有错误 —— 等于部分提交")
	}
	var rejected *productdto.BulkRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("错误应是 *BulkRejectedError（带可定位条目），实际 %T: %v", err, err)
	}
	return rejected.Items
}

// reasonOf 取某条 id 的原因 key；不存在则判失败。
func reasonOf(t *testing.T, items []productdto.BulkBlockedItemResp, id string) string {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it.ReasonKey
		}
	}
	t.Fatalf("拒绝条目里没有 %s（有 %+v）", id, items)
	return ""
}

func TestBulkDeleteAllOrNothing(t *testing.T) {
	f := newBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	projectA := f.newProject(t, "整批或零 A")
	projectB := f.newProject(t, "整批或零 B")

	t.Run("商品：夹着不存在 / 跨工程的 id 时一条都不删", func(t *testing.T) {
		ids := seedProductRows(t, f.db, projectA, make([]string, 3))
		other := seedProductRows(t, f.db, projectB, make([]string, 1))
		// p0 出现两次：归一去重后是 4 个 id，其中 2 个不能处理。
		_, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{
			IDs:       []string{ids[0], ids[0], ids[1], missingUUID, other[0]},
			ProjectID: projectA,
		})
		items := bulkRejected(t, err)
		if len(items) != 2 {
			t.Fatalf("拒绝条目 %d 条，want 2（去重后的不存在 id + 跨工程 id，实际 %+v）", len(items), items)
		}
		if key := reasonOf(t, items, missingUUID); key != productenums.ErrNotFound {
			t.Errorf("不存在 id 的原因 = %q，want %q", key, productenums.ErrNotFound)
		}
		if key := reasonOf(t, items, other[0]); key != productenums.ErrNotFound {
			t.Errorf("跨工程 id 的原因 = %q，want %q", key, productenums.ErrNotFound)
		}
		// 关键是这两行：整批被拒 = 一个都没删。
		for _, id := range ids {
			if n := rowCount(t, f.db, "products", id); n != 1 {
				t.Fatalf("整批被拒时 %s 不该被删，实际剩下 %d 行", id, n)
			}
		}
		if n := rowCount(t, f.db, "products", other[0]); n != 1 {
			t.Fatal("别的工程的商品不能被删（工程作用域必须生效）")
		}
	})

	t.Run("商品：清单全可处理时整批删掉", func(t *testing.T) {
		ids := seedProductRows(t, f.db, projectA, make([]string, 2))
		res, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{IDs: ids, ProjectID: projectA})
		if err != nil {
			t.Fatalf("批量删除商品失败：%v", err)
		}
		if res.Done != 2 {
			t.Fatalf("Done = %d，want 2", res.Done)
		}
		for _, id := range ids {
			if n := rowCount(t, f.db, "products", id); n != 0 {
				t.Fatalf("%s 应已删除，实际还剩 %d 行", id, n)
			}
		}
	})

	t.Run("分类：有子级 / 被主分类引用 / 不存在 → 整批拒绝", func(t *testing.T) {
		free := seedTaxonomyRows(t, f.db, "product_categories", projectA, "自由分类", 1)[0]
		parent := seedTaxonomyRows(t, f.db, "product_categories", projectA, "父分类", 1)[0]
		child := seedTaxonomyRows(t, f.db, "product_categories", projectA, "子分类", 1)[0]
		if err := f.db.Exec("UPDATE product_categories SET parent_id = ? WHERE id = ?", parent, child).Error; err != nil {
			t.Fatalf("造子级失败：%v", err)
		}
		refCate := seedTaxonomyRows(t, f.db, "product_categories", projectA, "被引用分类", 1)[0]
		prod := seedProductRows(t, f.db, projectA, make([]string, 1))[0]
		// 主分类走真列 primary_category_id：这条正是批量扫描里最容易写错的一路。
		setUUIDCol(t, f.db, prod, "primary_category_id", refCate)

		_, err := f.svc.BulkDeleteProductCategories(ctx, &productdto.BulkDeleteReq{
			IDs:       []string{free, parent, refCate, missingUUID},
			ProjectID: projectA,
		})
		items := bulkRejected(t, err)
		if len(items) != 3 {
			t.Fatalf("拒绝条目 %d 条，want 3（有子级 / 被引用 / 不存在），实际 %+v", len(items), items)
		}
		if key := reasonOf(t, items, parent); key != productenums.ErrCategoryHasChildren {
			t.Errorf("有子级的分类原因 = %q，want %q", key, productenums.ErrCategoryHasChildren)
		}
		if key := reasonOf(t, items, refCate); key != productenums.ErrCategoryInUse {
			t.Errorf("被主分类引用的原因 = %q，want %q", key, productenums.ErrCategoryInUse)
		}
		if n := rowCount(t, f.db, "product_categories", free); n != 1 {
			t.Fatal("整批被拒时没人引用的分类也不能被删")
		}
		if n := rowCount(t, f.db, "product_categories", child); n != 1 {
			t.Fatal("子级分类不能被连带删掉")
		}
	})

	t.Run("分类：只挑没人引用的那一个才删得掉", func(t *testing.T) {
		free := seedTaxonomyRows(t, f.db, "product_categories", projectA, "可删分类", 1)[0]
		res, err := f.svc.BulkDeleteProductCategories(ctx, &productdto.BulkDeleteReq{IDs: []string{free}, ProjectID: projectA})
		if err != nil {
			t.Fatalf("批量删除分类失败：%v", err)
		}
		if res.Done != 1 {
			t.Fatalf("Done = %d，want 1", res.Done)
		}
		if n := rowCount(t, f.db, "product_categories", free); n != 0 {
			t.Fatal("没人引用的分类应该被删掉")
		}
	})

	t.Run("品牌：被商品引用时整批拒绝", func(t *testing.T) {
		free := seedTaxonomyRows(t, f.db, "product_brands", projectA, "自由品牌", 1)[0]
		refBrand := seedTaxonomyRows(t, f.db, "product_brands", projectA, "被引用品牌", 1)[0]
		prod := seedProductRows(t, f.db, projectA, make([]string, 1))[0]
		setUUIDCol(t, f.db, prod, "brand_id", refBrand)

		_, err := f.svc.BulkDeleteProductBrands(ctx, &productdto.BulkDeleteReq{IDs: []string{free, refBrand}, ProjectID: projectA})
		items := bulkRejected(t, err)
		if len(items) != 1 {
			t.Fatalf("拒绝条目 %d 条，want 1，实际 %+v", len(items), items)
		}
		if key := reasonOf(t, items, refBrand); key != productenums.ErrBrandInUse {
			t.Errorf("被引用品牌的原因 = %q，want %q", key, productenums.ErrBrandInUse)
		}
		if n := rowCount(t, f.db, "product_brands", free); n != 1 {
			t.Fatal("整批被拒时自由品牌也不能被删")
		}
	})

	t.Run("属性：被商品引用时整批拒绝", func(t *testing.T) {
		free := seedAttributeRows(t, f.db, projectA, 1)[0]
		refAttr := seedAttributeRows(t, f.db, projectA, 1)[0]
		prod := seedProductRows(t, f.db, projectA, make([]string, 1))[0]
		setJSONB(t, f.db, prod, "attribute_ids", "[\""+refAttr+"\"]")

		_, err := f.svc.BulkDeleteProductAttributes(ctx, &productdto.BulkDeleteReq{IDs: []string{free, refAttr}, ProjectID: projectA})
		items := bulkRejected(t, err)
		if len(items) != 1 {
			t.Fatalf("拒绝条目 %d 条，want 1，实际 %+v", len(items), items)
		}
		if key := reasonOf(t, items, refAttr); key != productenums.ErrAttrInUse {
			t.Errorf("被引用属性的原因 = %q，want %q", key, productenums.ErrAttrInUse)
		}
		if n := rowCount(t, f.db, "product_attributes", free); n != 1 {
			t.Fatal("整批被拒时自由属性组也不能被删")
		}
	})

	t.Run("标签：跨工程引用时整批拒绝，本工程引用也不动", func(t *testing.T) {
		local := seedTaxonomyRows(t, f.db, "product_tags", projectA, "本工程标签", 1)[0]
		cross := seedTaxonomyRows(t, f.db, "product_tags", projectA, "被跨工程引用标签", 1)[0]
		prodA := seedProductRows(t, f.db, projectA, []string{local})[0]
		prodB := seedProductRows(t, f.db, projectB, []string{cross})[0]

		_, err := f.svc.BulkDeleteProductTags(ctx, &productdto.BulkDeleteReq{IDs: []string{local, cross}, ProjectID: projectA})
		items := bulkRejected(t, err)
		if len(items) != 1 {
			t.Fatalf("拒绝条目 %d 条，want 1，实际 %+v", len(items), items)
		}
		if key := reasonOf(t, items, cross); key != productenums.ErrTagCrossProject {
			t.Errorf("跨工程引用的原因 = %q，want %q", key, productenums.ErrTagCrossProject)
		}
		if n := rowCount(t, f.db, "product_tags", local); n != 1 {
			t.Fatal("整批被拒时本工程标签也不能被删")
		}
		if tags := jsonbList(t, f.db, "products", prodA, "tag_ids"); len(tags) != 1 {
			t.Fatalf("整批被拒时引用不该被动到，实际 %v", tags)
		}
		if tags := jsonbList(t, f.db, "products", prodB, "tag_ids"); len(tags) != 1 {
			t.Fatalf("别的工程的引用更不该被动到，实际 %v", tags)
		}

		// 只挑本工程那一个：删得掉，且引用先解绑。
		res, err := f.svc.BulkDeleteProductTags(ctx, &productdto.BulkDeleteReq{IDs: []string{local}, ProjectID: projectA})
		if err != nil {
			t.Fatalf("只删本工程标签应成功：%v", err)
		}
		if res.Done != 1 {
			t.Fatalf("Done = %d，want 1", res.Done)
		}
		if tags := jsonbList(t, f.db, "products", prodA, "tag_ids"); len(tags) != 0 {
			t.Fatalf("删标签要连同本工程商品上的引用一起解绑，实际还剩 %v", tags)
		}
	})
}

func TestBulkPricingAllOrNothing(t *testing.T) {
	f := newBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	projectA := f.newProject(t, "整批改价 A")

	ids := seedProductRows(t, f.db, projectA, make([]string, 2))
	seedVariantRows(t, f.db, ids, 100, 50)
	// 第二个变体本来就等于「成本 × 1.5」，规则跑完没有变化 → 进 UnchangedProducts。
	if err := f.db.Exec("UPDATE product_variants SET price = 75 WHERE product_id = ?", ids[1]).Error; err != nil {
		t.Fatalf("造「已在目标价」失败：%v", err)
	}

	t.Run("夹着不存在的 id 时一条都不改", func(t *testing.T) {
		_, err := f.svc.BulkApplyPricing(ctx, &productdto.BulkPricingReq{
			IDs:       []string{ids[0], ids[1], missingUUID},
			ProjectID: projectA,
			Note:      "整批或零",
			Rule:      pricingBudgetRule(),
		})
		items := bulkRejected(t, err)
		if len(items) != 1 {
			t.Fatalf("拒绝条目 %d 条，want 1（不存在的 id），实际 %+v", len(items), items)
		}
		if key := reasonOf(t, items, missingUUID); key != productenums.ErrPricingTargetNotFound {
			t.Errorf("不存在目标的原因 = %q，want %q", key, productenums.ErrPricingTargetNotFound)
		}
		var price float64
		if err := f.db.Table("product_variants").Select("price").Where("product_id = ?", ids[0]).Scan(&price).Error; err != nil {
			t.Fatalf("读价格失败：%v", err)
		}
		if price != 100 {
			t.Fatalf("整批被拒时价格不该被改，实际 %v（原 100）", price)
		}
	})

	t.Run("全可处理时一次改完，无变化的计 unchanged", func(t *testing.T) {
		res, err := f.svc.BulkApplyPricing(ctx, &productdto.BulkPricingReq{
			IDs:       []string{ids[0], ids[1]},
			ProjectID: projectA,
			Note:      "整批或零成功",
			Rule:      pricingBudgetRule(),
		})
		if err != nil {
			t.Fatalf("批量改价应成功：%v", err)
		}
		if res.Done != 2 || res.ChangedVariants != 1 || res.UnchangedProducts != 1 {
			t.Fatalf("回执不对：%+v，want done=2 changedVariants=1 unchangedProducts=1", res)
		}
	})
}
