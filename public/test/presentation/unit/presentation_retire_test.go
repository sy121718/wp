package unit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/internal/pipeline"
)

func TestRetireInstanceKeepsHistoryAndReplay(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tmpl, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{ProjectID: f.projectID, EntityType: "article", Name: "保留历史", DraftDocument: []byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{EntityType: "article", Slug: "retire-history", Data: map[string]any{"title": "保留历史"}})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, TemplateID: tmpl.ID, URLPath: "/retire-history"})
	if err != nil {
		t.Fatal(err)
	}
	model := presentationmodel.NewModel(f.db)
	pubs, err := model.ListPublications(ctx, instance.ID)
	if err != nil || len(pubs) == 0 {
		t.Fatalf("需要真实已发布状态: %v %v", pubs, err)
	}
	files := map[string][]byte{}
	for _, pub := range pubs {
		path := filepath.Join(pipeline.ActiveRoot(), pub.ActivePath, "index.html")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[pub.ArtifactHash] = raw
	}
	history := func() []int64 {
		t.Helper()
		counts := make([]int64, 3)
		for i, table := range []string{"document_snapshots", "presentation_artifacts", "presentation_publish_attempts"} {
			col := "presentation_instance_id"
			if i == 2 {
				col = "instance_id"
			}
			if err := f.db.Table(table).Where(col+" = ?", instance.ID).Count(&counts[i]).Error; err != nil {
				t.Fatal(err)
			}
		}
		return counts
	}
	before := history()
	for range 2 {
		if err := f.pres.RetireInstance(ctx, f.projectID, instance.ID); err != nil {
			t.Fatal(err)
		}
		if got := history(); !reflect.DeepEqual(got, before) {
			t.Fatalf("退役/重放必须保留全部历史等式: before=%v after=%v", before, got)
		}
		row, err := model.GetInstance(ctx, f.projectID, instance.ID)
		if err != nil || row.DeletedAt == nil || row.ActiveArtifactID != nil {
			t.Fatalf("实例软删且无活跃指针: %+v %v", row, err)
		}
		got, err := model.ListPublications(ctx, instance.ID)
		if err != nil || len(got) != 0 {
			t.Fatalf("语言发布状态必须清空: %v %v", got, err)
		}
		var routes int64
		if err := f.db.Table("page_routes").Where("presentation_id = ?", instance.ID).Count(&routes).Error; err != nil || routes != 0 {
			t.Fatalf("路由需在同事务释放: %d %v", routes, err)
		}
		for _, pub := range pubs {
			if _, err := os.Lstat(filepath.Join(pipeline.ActiveRoot(), pub.ActivePath)); !os.IsNotExist(err) {
				t.Fatalf("激活路径未撤销: %s %v", pub.ActivePath, err)
			}
		}
		for hash, expected := range files {
			raw, err := os.ReadFile(filepath.Join(pipeline.DefaultArtifactRoot(), "artifacts", hash, "index.html"))
			if err != nil || !reflect.DeepEqual(raw, expected) {
				t.Fatalf("历史产物字节不可改变: %s %v", hash, err)
			}
		}
	}
}
