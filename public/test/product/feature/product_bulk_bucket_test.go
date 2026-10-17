// product_bulk_bucket_test.go — 六个批量用例的分桶与守卫（task-156，dd.txt §5.4）。
//
// SQL 次数由 product_bulk_sql_budget_test.go 钉；本文件钉另一半：**每一项落到哪个桶**。
// dd.txt 点名要处理的五类输入都在这里：重复 id、不存在 id、跨工程 id、被引用的对象（分类的
// 主分类真列 / 品牌真列 / 属性 jsonb / 标签跨工程引用）、分类有子级。
//
// 这些断言同时是「批量守卫真的在跑」的证据：把批量引用扫描的谓词写错（例如 uuid 真列与 text
// 数组比较，PG 报 operator does not exist: uuid = text），本文件会直接变红，而不是静默全删。
package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
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

func TestBulkDeleteBuckets(t *testing.T) {
	f := newBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	projectA := f.newProject(t, "桶判定 A")
	projectB := f.newProject(t, "桶判定 B")

	t.Run("商品：重复 id / 不存在 id / 跨工程 id", func(t *testing.T) {
		ids := seedProductRows(t, f.db, projectA, make([]string, 3))
		other := seedProductRows(t, f.db, projectB, make([]string, 1))
		res, err := f.svc.BulkDeleteProducts(ctx, &productdto.BulkDeleteReq{
			// p0 出现两次（校验去重只算一项）、p1 正常、再来一个不存在的和别的工程的。
			IDs:       []string{ids[0], ids[0], ids[1], missingUUID, other[0]},
			ProjectID: projectA,
		})
		if err != nil {
			t.Fatalf("批量删除商品失败：%v", err)
		}
		if res.Requested != 4 || res.Succeeded != 2 || res.Skipped != 2 || res.Failed != 0 {
			t.Fatalf("分桶不对：%+v，want requested=4 succeeded=2 skipped=2 failed=0", res)
		}
		if rowCount(t, f.db, "products", ids[0]) != 0 || rowCount(t, f.db, "products", ids[1]) != 0 {
			t.Fatal("清单里的两项应该已被删除")
		}
		if rowCount(t, f.db, "products", ids[2]) != 1 {
			t.Fatal("不在清单里的商品不能被删（批量入口按请求 id 清单操作）")
		}
		if rowCount(t, f.db, "products", other[0]) != 1 {
			t.Fatal("别的工程的商品不能被删（工程作用域必须生效）")
		}
	})

	t.Run("分类：有子级 / 被主分类引用 / 可删", func(t *testing.T) {
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

		res, err := f.svc.BulkDeleteProductCategories(ctx, &productdto.BulkDeleteReq{
			IDs:       []string{free, parent, refCate, missingUUID},
			ProjectID: projectA,
		})
		if err != nil {
			t.Fatalf("批量删除分类失败：%v", err)
		}
		if res.Requested != 4 || res.Succeeded != 1 || res.Skipped != 3 || res.Failed != 0 {
			t.Fatalf("分桶不对：%+v，want requested=4 succeeded=1 skipped=3 failed=0", res)
		}
		if rowCount(t, f.db, "product_categories", free) != 0 {
			t.Fatal("没人引用的分类应该被删掉")
		}
		if rowCount(t, f.db, "product_categories", parent) != 1 || rowCount(t, f.db, "product_categories", child) != 1 {
			t.Fatal("有子级的分类不能被删（连同它的子树一起留着）")
		}
		if rowCount(t, f.db, "product_categories", refCate) != 1 {
			t.Fatal("被商品主分类引用的分类不能被删")
		}
	})

	t.Run("品牌：被引用 / 可删", func(t *testing.T) {
		free := seedTaxonomyRows(t, f.db, "product_brands", projectA, "自由品牌", 1)[0]
		refBrand := seedTaxonomyRows(t, f.db, "product_brands", projectA, "被引用品牌", 1)[0]
		prod := seedProductRows(t, f.db, projectA, make([]string, 1))[0]
		setUUIDCol(t, f.db, prod, "brand_id", refBrand)

		res, err := f.svc.BulkDeleteProductBrands(ctx, &productdto.BulkDeleteReq{
			IDs:       []string{free, refBrand},
			ProjectID: projectA,
		})
		if err != nil {
			t.Fatalf("批量删除品牌失败：%v", err)
		}
		if res.Succeeded != 1 || res.Skipped != 1 {
			t.Fatalf("分桶不对：%+v，want succeeded=1 skipped=1", res)
		}
		if rowCount(t, f.db, "product_brands", refBrand) != 1 {
			t.Fatal("被商品引用的品牌不能被删")
		}
	})

	t.Run("属性：被引用 / 可删", func(t *testing.T) {
		free := seedAttributeRows(t, f.db, projectA, 1)[0]
		refAttr := seedAttributeRows(t, f.db, projectA, 1)[0]
		prod := seedProductRows(t, f.db, projectA, make([]string, 1))[0]
		setJSONB(t, f.db, prod, "attribute_ids", "[\""+refAttr+"\"]")

		res, err := f.svc.BulkDeleteProductAttributes(ctx, &productdto.BulkDeleteReq{
			IDs:       []string{free, refAttr},
			ProjectID: projectA,
		})
		if err != nil {
			t.Fatalf("批量删除属性失败：%v", err)
		}
		if res.Succeeded != 1 || res.Skipped != 1 {
			t.Fatalf("分桶不对：%+v，want succeeded=1 skipped=1", res)
		}
		if rowCount(t, f.db, "product_attributes", refAttr) != 1 {
			t.Fatal("被商品引用的属性不能被删")
		}
	})

	t.Run("标签：本工程引用可解绑 / 跨工程引用必须拒绝", func(t *testing.T) {
		local := seedTaxonomyRows(t, f.db, "product_tags", projectA, "本工程标签", 1)[0]
		cross := seedTaxonomyRows(t, f.db, "product_tags", projectA, "被跨工程引用标签", 1)[0]
		prodA := seedProductRows(t, f.db, projectA, []string{local})[0]
		prodB := seedProductRows(t, f.db, projectB, []string{cross})[0]

		res, err := f.svc.BulkDeleteProductTags(ctx, &productdto.BulkDeleteReq{
			IDs:       []string{local, cross, missingUUID},
			ProjectID: projectA,
		})
		if err != nil {
			t.Fatalf("批量删除标签失败：%v", err)
		}
		if res.Requested != 3 || res.Succeeded != 1 || res.Skipped != 2 {
			t.Fatalf("分桶不对：%+v，want requested=3 succeeded=1 skipped=2", res)
		}
		if rowCount(t, f.db, "product_tags", local) != 0 {
			t.Fatal("本工程内被引用的标签应当能删（引用先解绑）")
		}
		if tags := jsonbList(t, f.db, "products", prodA, "tag_ids"); len(tags) != 0 {
			t.Fatalf("删标签要连同本工程商品上的引用一起解绑，实际还剩 %v", tags)
		}
		if rowCount(t, f.db, "product_tags", cross) != 1 {
			t.Fatal("被别的工程引用的标签不能被删")
		}
		if tags := jsonbList(t, f.db, "products", prodB, "tag_ids"); len(tags) != 1 {
			t.Fatalf("别的工程的引用不该被动到，实际 %v", tags)
		}
	})

	t.Run("定价：不存在 id / 已在目标价 / 正常改价", func(t *testing.T) {
		ids := seedProductRows(t, f.db, projectA, make([]string, 3))
		// 前两个商品各一个变体（成本 50），第三个商品没有变体（无从改价 → 跳过）。
		seedVariantRows(t, f.db, ids[:2], 100, 50)
		// 第二个变体本来就等于「成本 × 1.5」，规则跑完没有变化 → 不改（进 UnchangedProducts）。
		if err := f.db.Exec("UPDATE product_variants SET price = 75 WHERE product_id = ?", ids[1]).Error; err != nil {
			t.Fatalf("造「已在目标价」失败：%v", err)
		}
		res, err := f.svc.BulkApplyPricing(ctx, &productdto.BulkPricingReq{
			IDs:       []string{ids[0], ids[1], ids[2], missingUUID},
			ProjectID: projectA,
			Note:      "分桶回归",
			Rule:      pricingBudgetRule(),
		})
		if err != nil {
			t.Fatalf("批量改价失败：%v", err)
		}
		if res.ChangedVariants != 1 || res.UnchangedProducts != 1 || res.Skipped != 2 || res.Failed != 0 {
			t.Fatalf("分桶不对：%+v，want changedVariants=1 unchangedProducts=1 skipped=2 failed=0", res)
		}
	})
}
