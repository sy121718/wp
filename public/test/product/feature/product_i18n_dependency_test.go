package feature

// product_i18n_dependency_test.go — 商品轨的多语言依赖登记（翻译底座）。
//
// 背景（docs/06-D §9 关键约束）：组件固定文案（sys_i18n）与作者文案（sys_translation）
// 都在构建期取词注入 HTML 字节 —— 缺依赖登记时，改了词条/译文后 revision 未变，
// 产物不重建，页面长期停在旧内容（甚至回退原文），且日志里什么都没有。
// page 轨早已登记（page_lang.go §buildDependencies）；本用例钉住商品轨的对等行为。

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

	// 词条依赖必须登记（无条件：组件固定文案由构建期取词注入字节）。
	// 内容译文依赖（i18n:content）是**有条件**的 —— 只在本次构建确有可翻译候选时登记：
	// 默认商品详情模板是数据驱动的（字段 binding + 词条，无作者字面量），
	// 因此这里不登记它是正确行为，不是漏登记；文档里出现作者文案（多语言工程下）时才需要它。
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

	// 词条/译文变更的失效入口：整站标记必须覆盖自动发布实例（此前只有 page 侧有）。
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
