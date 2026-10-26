package productservice_test

// product_source_nquery_test.go — 集合源关联翻译的查询次数不随商品数增长。
//
// 审查 P1：localizeRelatedFrom 对每个商品都遍历全页的 categories / brands / tags 索引，
// 为其中每个实体取一次译文。N 个商品 × C 个关联实体 = N×C 次 contentStore 查询，
// 而 pkg/i18n 的查询侧没有缓存。
//
// 判据用**查询次数**而不是耗时：次数是确定的，耗时会被机器噪声干扰。

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"go_wp/internal/builder/core"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"
)

// countingStore 记录 LoadTargets 被调了几次。
type countingStore struct {
	mu    sync.Mutex
	calls int
}

func (c *countingStore) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return map[string]string{}, nil
}

var _ i18n.ContentStore = (*countingStore)(nil)

// TestCollectionRelatedTranslationQueryCountDoesNotGrowWithProducts
// 商品数从 2 增到 8 时，关联翻译的查询次数不该跟着翻四倍。
func TestCollectionRelatedTranslationQueryCountDoesNotGrowWithProducts(t *testing.T) {
	counts := map[int]int{}
	for _, n := range []int{2, 8} {
		db := support.NewMigratedPGTestDB(t)
		projectID := uuid.NewString()
		support.SeedProjectRow(t, db, projectID, "N 查询")

		// 一个品牌、一个分类、一个标签，被 n 个商品共享 —— 这正是「C 固定、N 增长」的场景。
		brandID, catID, tagID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		if err := db.Exec(`INSERT INTO product_brands (id, project_id, name, slug, sort, create_time, update_time)
			VALUES (?, ?, '品牌甲', 'brand-a', 1, now(), now())`, brandID, projectID).Error; err != nil {
			t.Fatalf("造品牌失败：%v", err)
		}
		if err := db.Exec(`INSERT INTO product_categories (id, project_id, name, slug, sort, create_time, update_time)
			VALUES (?, ?, '分类甲', 'cat-a', 1, now(), now())`, catID, projectID).Error; err != nil {
			t.Fatalf("造分类失败：%v", err)
		}
		if err := db.Exec(`INSERT INTO product_tags (id, project_id, name, slug, kind, rule_type, sort, create_time, update_time)
			VALUES (?, ?, '标签甲', 'tag-a', 'manual', '', 1, now(), now())`, tagID, projectID).Error; err != nil {
			t.Fatalf("造标签失败：%v", err)
		}
		for i := 0; i < n; i++ {
			if err := db.Exec(`INSERT INTO products
				(id, project_id, name, slug, sku_code, status, sort,
				 category_ids, primary_category_id, brand_id, tag_ids, create_time, update_time)
				VALUES (?, ?, ?, ?, ?, 'published', ?, ?::jsonb, ?, ?, ?::jsonb, now(), now())`,
				uuid.NewString(), projectID, "商品"+uuid.NewString()[:4], "p-"+uuid.NewString()[:8],
				"SKU-"+uuid.NewString()[:8], i+1,
				`["`+catID+`"]`, catID, brandID, `["`+tagID+`"]`).Error; err != nil {
				t.Fatalf("造商品失败：%v", err)
			}
		}

		store := &countingStore{}
		svc := productservice.NewService(productmodel.NewModel(db), nqueryProjectStub{projectID: projectID})
		svc.SetContentStore(store)

		ctx := core.WithBuildLang(core.WithBuildProjectID(context.Background(), projectID), "en-US")
		if _, err := svc.ResolveCollection(ctx, "content:product", nil); err != nil {
			t.Fatalf("取数失败：%v", err)
		}
		counts[n] = store.calls
	}

	t.Logf("查询次数：2 个商品 %d 次，8 个商品 %d 次", counts[2], counts[8])
	// 商品数翻 4 倍（2→8），查询次数不该跟着翻 4 倍。
	// 修复前：每个商品各遍历一遍实体（每次 LoadTargets 取一批），次数与 N 成正比。
	if counts[8] > counts[2]*2 {
		t.Fatalf("商品 2 个时查询 %d 次、8 个时 %d 次 —— 关联翻译仍随商品数线性增长（N×C）。\n"+
			"应当整页只算一遍实体译文，商品数增加不该带来新的实体查询",
			counts[2], counts[8])
	}
}

type nqueryProjectStub struct {
	projectcontract.ProjectService
	projectID string
}

func (s nqueryProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return []projectdto.ProjectResp{{ID: s.projectID}}, nil
}
