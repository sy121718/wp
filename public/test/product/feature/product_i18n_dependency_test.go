package feature

import (
	"context"
	"testing"

	"go_wp/internal/pipeline"
)

func TestProductArtifactRegistersI18nDependencies(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	productID := f.createProduct(t, "译文依赖商品", "/i18n-dep", "译文依赖测试描述", 10, 20)
	inst := f.publish(t, productID, "/i18n-dep")

	for _, key := range []string{pipeline.I18NDependencyKey} {
		var cnt int64
		if err := f.db.Table("presentation_dependencies").
			Where("presentation_id = ? AND dependency_kind = ? AND dependency_key = ?",
				inst.ID, pipeline.DependencyKindI18N, key).
			Count(&cnt).Error; err != nil {
			t.Fatalf("查询依赖表失败: %v", err)
		}
		if cnt == 0 {
			t.Fatalf("产物未登记 i18n 依赖 %q —— 改了词条/译文不会重建商品页（静默失效）", key)
		}
	}

	if err := f.db.Exec("UPDATE presentation_instances SET stale = false WHERE id = ?", inst.ID).Error; err != nil {
		t.Fatalf("重置 stale 失败: %v", err)
	}
	if err := f.pres.MarkStaleForI18n(ctx); err != nil {
		t.Fatalf("MarkStaleForI18n 失败: %v", err)
	}
	var stale bool
	if err := f.db.Table("presentation_instances").Select("stale").
		Where("id = ?", inst.ID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	if !stale {
		t.Fatalf("译文/词条变更后实例未被标记待重建（改译文不会重建商品页）")
	}
}
