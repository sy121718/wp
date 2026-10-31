package unit

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
