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
	if got, err := f.pres.PublishedInstancePath(ctx, f.projectID, instance.ID, pub.Lang); err != nil || got != pub.ActivePath {
		t.Fatalf("实例路径必须等于目标语言Publication: %s %+v %v", got, pub, err)
	}
	if got, err := f.pres.PublishedInstancePath(ctx, f.projectID, instance.ID, "missing-language"); err != nil || got != "" {
		t.Fatalf("无目标语言不能回退url_path: %s %v", got, err)
	}
	if _, err := f.pres.PublishedInstancePath(ctx, uuid.NewString(), instance.ID, pub.Lang); err == nil {
		t.Fatal("不能引用别的工程实例")
	}
	if err := os.Remove(filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(pub.ActivePath, "/"), "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pres.PublishedInstancePath(ctx, f.projectID, instance.ID, pub.Lang); err == nil {
		t.Fatal("缺文件实例不得形成死链接")
	}
}
