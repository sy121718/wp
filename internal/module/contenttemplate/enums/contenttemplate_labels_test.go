package contenttemplateenums

import "testing"

// 展示标签（枚举 → 展示名）的取值覆盖：key 与中文兜底都必须逐字稳定。
//
// 判据是「两样都给」：只给中文 → 英文界面恒中文；只给 key → 词条缺失时页面显示裸 key
// （admin.content.templates.entityProduct）。两者任一漂了，页面上都不会报错，
// 只会静默显示成另一种东西 —— 所以这里逐字钉住。
//
// header / footer 两条尤其重要：它们的 key 是**复用**筛选下拉的既有词条
// （admin.content.templates.entityHeader / entityFooter），改这里的 key 等于让筛选器
// 与表格徽章各查一条词条 —— 库里只有一条，另一个必然回落中文兜底。
func TestEntityTypeLabelPairs(t *testing.T) {
	cases := []struct {
		entityType, key, fallback string
	}{
		{"header", "admin.content.templates.entityHeader", "页眉（结构模板）"},
		{"footer", "admin.content.templates.entityFooter", "页脚（结构模板）"},
		{"product", "admin.content.templates.entityProduct", "商品详情"},
		{"article", "admin.content.templates.entityArticle", "文章详情"},
		{"category", "admin.content.templates.entityCategory", "分类归档"},
		{"tag", "admin.content.templates.entityTag", "标签归档"},
		{"brand", "admin.content.templates.entityBrand", "品牌归档"},
		// 认不出的取值：key 留空、兜底为原值（显示生值好过空白）。
		{"carousel", "", "carousel"},
	}
	for _, tc := range cases {
		got := EntityTypeLabel(tc.entityType)
		if got.Key != tc.key || got.Fallback != tc.fallback {
			t.Errorf("EntityTypeLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.entityType, got.Key, got.Fallback, tc.key, tc.fallback)
		}
	}
}

func TestTemplateRoleLabelPairs(t *testing.T) {
	cases := []struct {
		role, key, fallback string
	}{
		{"archive", "admin.content.templates.roleArchive", "归档页"},
		{"detail", "admin.content.templates.roleDetail", "详情页"},
		// 空值（历史数据没有这一列）按详情页：与列表页此前显示的口径一致。
		{"", "admin.content.templates.roleDetail", "详情页"},
		{"zzbogus", "", "zzbogus"},
	}
	for _, tc := range cases {
		got := TemplateRoleLabel(tc.role)
		if got.Key != tc.key || got.Fallback != tc.fallback {
			t.Errorf("TemplateRoleLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.role, got.Key, got.Fallback, tc.key, tc.fallback)
		}
	}
}

func TestSlotLabelPairs(t *testing.T) {
	for _, tc := range []struct {
		got           LabelPair
		key, fallback string
	}{
		{LabelSlotHeader, "admin.content.templates.slotHeader", "页眉"},
		{LabelSlotFooter, "admin.content.templates.slotFooter", "页脚"},
	} {
		if tc.got.Key != tc.key || tc.got.Fallback != tc.fallback {
			t.Errorf("槽位标签 = (%q, %q)，期望 (%q, %q)",
				tc.got.Key, tc.got.Fallback, tc.key, tc.fallback)
		}
	}
}
