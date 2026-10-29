package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/internal/pipeline"
)

func TestPublishedInstanceActionTargetIsScopedExactAndReadable(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	f.createTemplate(t)
	ctx := context.Background()
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{EntityType: "article", Slug: "action-target", Data: map[string]any{"title": "精确详情目标"}})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/selected-detail"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := presentationmodel.NewModel(f.db).ListPublications(ctx, instance.ID)
	if err != nil || len(rows) == 0 {
		t.Fatalf("真实发布应登记语言: %+v %v", rows, err)
	}
	pub := rows[0]
	listed, err := f.pres.List(ctx, &presentationdto.ListReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	var selected *presentationdto.InstanceResp
	for _, row := range listed {
		if row.ID == instance.ID {
			selected = row
			break
		}
	}
	if selected == nil || len(selected.Publications) != len(rows) {
		t.Fatal("作者目标列表未暴露真实逐语言Publication")
	}
	for i, published := range selected.Publications {
		if published.Lang != rows[i].Lang || published.ActivePath != rows[i].ActivePath || published.ArtifactHash != rows[i].ArtifactHash || published.ArtifactID == nil || rows[i].ArtifactID == nil || *published.ArtifactID != *rows[i].ArtifactID {
			t.Fatalf("目标列表语言事实与Publication不同：%+v %+v", published, rows[i])
		}
	}

	if got, err := f.pres.PublishedInstancePath(ctx, f.projectID, instance.ID, pub.Lang); err != nil || got != pub.ActivePath {
		t.Fatalf("实例路径必须等于目标语言Publication: %s %+v %v", got, pub, err)
	}
	if got, err := f.pres.PublishedInstancePath(ctx, f.projectID, instance.ID, "missing-language"); err != nil || got != "" {
		t.Fatalf("无目标语言不能回退url_path: %s %v", got, err)
	}
	if _, err := f.pres.PublishedInstancePath(ctx, uuid.NewString(), instance.ID, pub.Lang); err == nil {
		t.Fatal("不能引用别的工程实例")
	}
	choices, err := f.pres.List(ctx, &presentationdto.ListReq{ProjectID: f.projectID, Lang: pub.Lang, ActionTargetsOnly: true})
	if err != nil || len(choices) != 1 || choices[0].ID != instance.ID {
		t.Fatalf("真实激活实例可选: %+v %v", choices, err)
	}
	missing, err := f.pres.List(ctx, &presentationdto.ListReq{ProjectID: f.projectID, Lang: "missing-language", ActionTargetsOnly: true})
	if err != nil || len(missing) != 0 {
		t.Fatalf("候选不得回退其它语言: %+v %v", missing, err)
	}
	if _, err := f.pres.List(ctx, &presentationdto.ListReq{ProjectID: f.projectID, ActionTargetsOnly: true}); err == nil {
		t.Fatal("动作候选必须指定精确语言")
	}
	if err := os.Remove(filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(pub.ActivePath, "/"), "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pres.PublishedInstancePath(ctx, f.projectID, instance.ID, pub.Lang); err == nil {
		t.Fatal("缺文件实例不得形成死链接")
	}
	choices, err = f.pres.List(ctx, &presentationdto.ListReq{ProjectID: f.projectID, Lang: pub.Lang, ActionTargetsOnly: true})
	if err != nil || len(choices) != 0 {
		t.Fatalf("损坏实例不应列为动作候选: %+v %v", choices, err)
	}
}
