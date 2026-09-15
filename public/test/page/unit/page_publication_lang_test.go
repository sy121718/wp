package unit

// page_publication_lang_test.go — 按语言删除发布指针（审计 I18N-017）。
//
// 这里钉住的是**误伤**：禁用一种语言时，另一种语言的发布指针必须原样留着。
// 原有的 DeletePublications(pageID) 没有语言维度，会把仍在服务的语言一起清掉 ——
// 那个语言的页面会立刻失去「已发布」状态，而访问面上产物还在服务，
// 后台与访问面从此各说各话，且没有任何界面能看出是哪一步错的。

import (
	"context"
	"testing"
	"time"

	pagemodel "go_wp/internal/module/page/model"

	"go_wp/public/test/support"
)

func TestDeletePublicationsByLangKeepsOtherLanguages(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return
	}
	// pages 也在迁移范围内：发布表的 page_id 带外键，缺它时插入会报
	// `relation "pages" does not exist`（而被误读成「模型写错了」）。
	if err := db.AutoMigrate(&pagemodel.PageEntity{}, &pagemodel.PublicationEntity{}, &pagemodel.StagingEntity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	m := pagemodel.NewPageModel(db)
	ctx := context.Background()
	now := time.Now().UTC()
	pageID := "11111111-1111-1111-1111-111111111111"
	for _, lang := range []string{"zh-CN", "en-US"} {
		if err := db.Exec(`INSERT INTO page_publications
			(page_id, lang, active_path, artifact_hash, published_at, update_time)
			VALUES (?, ?, ?, ?, ?, ?)`,
			pageID, lang, "/"+lang, "hash-"+lang, now, now).Error; err != nil {
			t.Fatalf("插入 %s 发布指针失败: %v", lang, err)
		}
	}

	if err := m.DeletePublicationsByLang(ctx, pageID, "en-US"); err != nil {
		t.Fatalf("按语言删除失败: %v", err)
	}

	var remaining []string
	if err := db.Table("page_publications").Where("page_id = ?", pageID).Pluck("lang", &remaining).Error; err != nil {
		t.Fatalf("查询剩余指针失败: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "zh-CN" {
		t.Fatalf("只应删 en-US，实际剩余 %v（误删了仍在服务的语言）", remaining)
	}
}
