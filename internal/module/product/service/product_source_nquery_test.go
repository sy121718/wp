package productservice_test

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

func TestCollectionRelatedTranslationQueryCountDoesNotGrowWithProducts(t *testing.T) {
	counts := map[int]int{}
	for _, n := range []int{2, 8} {
		db := support.NewMigratedPGTestDB(t)
		projectID := uuid.NewString()
		support.SeedProjectRow(t, db, projectID, "N 查询")

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
