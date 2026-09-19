package unit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/internal/pipeline"
)

func TestPresentationDependencyIncludesOtherPublishedLocale(t *testing.T) {
	f := newPresFixture(t)
	ctx := context.Background()
	f.createTemplate(t)
	if err := f.db.Exec(`INSERT INTO project_locales(project_id,lang,sort_order,is_default,enabled,create_time,update_time) VALUES (?,'zh-CN',0,true,true,now(),now()),(?,'en-US',1,false,true,now(),now())`, f.projectID, f.projectID).Error; err != nil {
		t.Fatal(err)
	}
	e, err := f.content.Create(ctx, &contentdto.CreateReq{EntityType: "article", Slug: "locale-dep", Data: map[string]any{"title": "语言依赖"}})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{ProjectID: f.projectID, EntityType: "article", EntityID: e.ID, URLPath: "/locale-dep"})
	if err != nil {
		t.Fatal(err)
	}
	m := presentationmodel.NewModel(f.db)
	pubs, err := m.ListPublications(ctx, inst.ID)
	if err != nil || len(pubs) != 2 {
		t.Fatalf("语言账本: %+v %v", pubs, err)
	}
	// 镜像只指向第二种语言，第一种语言独有的依赖仍必须命中。
	if err := f.db.Exec(`UPDATE presentation_instances SET active_artifact_id=?, staged_artifact_id=? WHERE id=?`, pubs[1].ArtifactID, pubs[1].ArtifactID, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, templateOnly := range []bool{false, true} {
		dep := pipeline.BlockKey(uuid.NewString())
		if templateOnly {
			dep = pipeline.ContentTemplateKey(uuid.NewString())
		}
		if err := f.db.Exec(`INSERT INTO presentation_dependencies(presentation_id,artifact_id,dependency_kind,dependency_key,last_checked) VALUES (?,?,?,?,now())`, inst.ID, pubs[0].ArtifactID, dep.Kind, dep.Key).Error; err != nil {
			t.Fatal(err)
		}
		var ids []string
		if templateOnly {
			ids, err = m.MarkStaleTemplateModeByDependency(ctx, f.projectID, dep.Kind, dep.Key, time.Now())
		} else {
			ids, err = m.MarkStaleByDependency(ctx, f.projectID, dep.Kind, dep.Key, time.Now())
		}
		if err != nil || len(ids) != 1 || ids[0] != inst.ID {
			t.Fatalf("其他语言依赖漏标(templateOnly=%v): %v %v", templateOnly, ids, err)
		}
	}
	// 脱离正文模板不等于脱离页眉/页脚结构模板。
	if err := f.db.Exec(`UPDATE presentation_instances SET render_mode='document' WHERE id=?`, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	var boundTemplate string
	if err := f.db.Raw(`SELECT template_id FROM presentation_instances WHERE id=?`, inst.ID).Scan(&boundTemplate).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		want int
	}{{boundTemplate, 0}, {uuid.NewString(), 1}} {
		dep := pipeline.ContentTemplateKey(tc.id)
		if err := f.db.Exec(`INSERT INTO presentation_dependencies(presentation_id,artifact_id,dependency_kind,dependency_key,last_checked) VALUES (?,?,?,?,now()) ON CONFLICT DO NOTHING`, inst.ID, pubs[0].ArtifactID, dep.Kind, dep.Key).Error; err != nil {
			t.Fatal(err)
		}
		ids, err := m.MarkStaleTemplateModeByDependency(ctx, f.projectID, dep.Kind, dep.Key, time.Now())
		if err != nil || len(ids) != tc.want {
			t.Fatalf("独立文档模板依赖 %s：want=%d got=%v err=%v", tc.id, tc.want, ids, err)
		}
	}

}
