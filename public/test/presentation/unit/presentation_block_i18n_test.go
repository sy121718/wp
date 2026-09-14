// presentation_block_i18n_test.go — 内容模板 globalref 块内文案参与内容翻译（P5b + I18N-013）。
package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/i18n"
)

type presContentStore struct {
	rows map[string]string
}

func (s *presContentStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range hashes {
		for k, v := range s.rows {
			if strings.HasPrefix(k, lang+"|"+h+"|") {
				out[i18n.ContentIndexKey(h, strings.TrimPrefix(k, lang+"|"+h+"|"))] = v
			}
		}
	}
	return out, nil
}

func presBlockContentStore(rows ...[4]string) *presContentStore {
	s := &presContentStore{rows: map[string]string{}}
	for _, r := range rows {
		s.rows[r[0]+"|"+i18n.ContentHash(r[2])+"|"+r[1]] = r[3]
	}
	return s
}

func (f *presFixture) wireBlocks(t *testing.T) {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(f.db), projects)
	registry := core.NewEntitySourceRegistry()
	if err := f.content.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册实体类型失败: %v", err)
	}
	f.pres = presentationservice.NewService(
		presentationmodel.NewModel(f.db), f.templates, registry, projects, blocks, f.routes)
}

// TestPresentationBlockContentTranslation globalref 块内文本随构建语言切换。
func TestPresentationBlockContentTranslation(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })

	if err := f.db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, created_at, updated_at) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		f.projectID, "zh-CN", f.projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
	f.wireBlocks(t)
	f.pres.SetContentTranslationStore(presBlockContentStore(
		[4]string{"en-US", "core.text.text", "块内促销文案", "Promo in block"},
	))

	blockSvc := blockservice.NewService(blockmodel.NewBlockModel(f.db), projectservice.NewService(projectmodel.NewProjectModel(f.db)))
	promo, err := blockSvc.Create(ctx, &blockdto.CreateReq{
		ProjectID: f.projectID, Name: "促销条", Kind: "block",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pb1","type":"core.text","props":{"text":"块内促销文案"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建块失败: %v", err)
	}

	tplDoc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}},{"id":"ref1","type":"core.globalref","props":{"blockId":"%s"}}]}`, promo.ID)
	if _, err = f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "块引用模板", DraftDocument: []byte(tplDoc),
	}); err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "block-i18n", Data: map[string]any{"title": "块翻译测试"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	if _, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/articles/block-i18n",
	}); err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}

	zhHTML := activeHTML(t, "/articles/block-i18n")
	enHTML := activeHTML(t, "/en/articles/block-i18n")
	if !strings.Contains(zhHTML, "块内促销文案") {
		t.Fatalf("zh 产物应含块内原文")
	}
	if !strings.Contains(enHTML, "Promo in block") {
		t.Fatalf("en 产物应含块内译文")
	}
	if strings.Contains(enHTML, "块内促销文案") {
		t.Fatal("en 产物不应保留块内中文原文")
	}
}
