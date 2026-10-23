package productservice_test

// product_translation_candidates_test.go — 整工程翻译候选必须覆盖 subtitle / description / imageAlts。
//
// 审查 P1：ProjectTranslationCandidates 原先用 s.m.List 取行，而那个方法**只 Select
// 列表需要的列**（metadata / description / subtitle / image_alts 都不在）。
// 于是候选里这三类文本恒为零值 —— 表现为「整工程翻了一遍，商品副标题与描述没进待翻清单」，
// 而没有任何报错。单商品入口 ProductTranslationCandidates 用 s.m.Get（全列）所以是对的，
// 两个入口对同一份数据给出不同结果，正是这类缺陷最难发现的地方。

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
		(id, project_id, name, subtitle, description, images_alt, slug, sku_code, status, type, sort, create_time, update_time)
		VALUES (?, ?, '主标题', '副标题文本', ?::jsonb, ?::jsonb, ?, ?, 'published', 'variant', 1, now(), now())`,
		productID, projectID,
		`{"html":"<p>描述正文</p>"}`,
		`["alt 文本甲"]`,
		"p-"+uuid.NewString()[:8], "SKU-"+uuid.NewString()[:8]).Error; err != nil {
		t.Fatalf("造数据失败：%v", err)
	}

	svc := productservice.NewService(productmodel.NewModel(db), candidateProjectStub{projectID: projectID})
	list, err := svc.ProjectTranslationCandidates(context.Background(), projectID)
	if err != nil {
		t.Fatalf("取候选失败：%v", err)
	}

	// 收集 (entityType, field) 与文本，按字段判。
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

// candidateProjectStub 工程契约桩：内嵌接口，只覆盖 resolveProjectID 用到的那一个方法。
// 内嵌让其余方法在调用时 panic（显式失败，不静默返回零值）。
type candidateProjectStub struct {
	projectcontract.ProjectService
	projectID string
}

func (s candidateProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return []projectdto.ProjectResp{{ID: s.projectID}}, nil
}
