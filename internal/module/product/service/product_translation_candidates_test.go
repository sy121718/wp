package productservice_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

func TestProjectTranslationCandidatesCoverListOmittedColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "翻译候选")

	productID := uuid.NewString()
	if err := db.Exec(`INSERT INTO products
		(id, project_id, name, subtitle, description, images_alt, slug, sku_code, status, sort, create_time, update_time)
		VALUES (?, ?, '主标题', '副标题文本', ?::jsonb, ?::jsonb, ?, ?, 'published', 1, now(), now())`,
		productID, projectID,
		`{"html":"<p>描述正文</p>"}`,
		`["alt 文本甲"]`,
		"p-"+uuid.NewString()[:8], "SKU-"+uuid.NewString()[:8]).Error; err != nil {
		t.Fatalf("造数据失败：%v", err)
	}

	svc := productservice.NewService(productmodel.NewModel(db), candidateProjectStub{projectID: projectID}, stubMasterDataChanges{})
	list, err := svc.ProjectTranslationCandidates(context.Background(), projectID)
	if err != nil {
		t.Fatalf("取候选失败：%v", err)
	}

	got := map[string]string{}
	for _, c := range list {
		if c.EntityType == productcontract.EntityTypeProduct {
			got[c.Field] = c.SourceText
		}
	}
	for field, want := range map[string]string{
		"name":        "主标题",
		"subtitle":    "副标题文本",
		"description": "<p>描述正文</p>",
		"imageAlts":   "alt 文本甲",
	} {
		if _, ok := got[field]; !ok {
			t.Errorf("候选缺少字段 %q —— 用 s.m.List 取行时这一列不在 Select 里，恒为零值", field)
			continue
		}
		if got[field] != want {
			t.Errorf("字段 %q 的候选文本 = %q，期望 %q", field, got[field], want)
		}
	}
}

type candidateProjectStub struct {
	projectcontract.ProjectService
	projectID string
}

func (s candidateProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return []projectdto.ProjectResp{{ID: s.projectID}}, nil
}
