package contenttemplateenums

import "testing"

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
