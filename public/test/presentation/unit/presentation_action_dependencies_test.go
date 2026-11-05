package unit

import (
	"context"
	"fmt"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/facing"
	publish "go_wp/pkg/publish"
	"reflect"
	"testing"
)

func TestPresentationActionDependencyImpactAndDeleteProtection(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	targetTemplate, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{ProjectID: f.projectID, EntityType: "article", Name: "目标模板", DraftDocument: []byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	create := func(slug, templateID string) string {
		t.Helper()
		entity, err := f.content.Create(ctx, &contentdto.CreateReq{EntityType: "article", Slug: slug, Data: map[string]any{"title": slug}})
		if err != nil {
			t.Fatal(err)
		}
		inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, TemplateID: templateID, URLPath: "/" + slug})
		if err != nil {
			t.Fatal(err)
		}
		return inst.ID
	}
	target := create("explicit-target", targetTemplate.ID)
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"cart","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"presentation","instanceId":%q}}}]}`, target)
	consumerTemplate, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{ProjectID: f.projectID, EntityType: "article", Name: "引用模板", DraftDocument: []byte(doc)})
	if err != nil {
		t.Fatal(err)
	}
	consumer := create("explicit-consumer", consumerTemplate.ID)
	plain := create("no-reference", targetTemplate.ID)
	key := pipeline.PresentationTargetKey(target)
	ids, err := presentationmodel.NewModel(f.db).ListTargetDependencyInstances(ctx, f.projectID, key.Kind, key.Key)
	if err != nil || !reflect.DeepEqual(ids, []string{consumer}) {
		t.Fatalf("真实Compile依赖必须精确: %v %v", ids, err)
	}
	if err := f.pres.Delete(ctx, &presentationdto.DeleteReq{ProjectID: f.projectID, ID: target}); err == nil {
		t.Fatal("有引用的实例不得删除")
	} else if status, ok := facing.StatusOf(err); !ok || status != 409 {
		t.Fatalf("必须声明409: %v", err)
	}
	fanout := publish.NewFanout()
	fanout.Register(publish.SourceTypePresentation, f.pres)
	f.pres.SetActionTargetInvalidator(fanout)
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{ProjectID: f.projectID, ID: target, NewPath: "/explicit-target-moved"}); err != nil {
		t.Fatal(err)
	}
	var affected []string
	if err := f.db.Table("presentation_instances").Where("id IN ? AND stale=true", []string{consumer, plain}).Order("id").Pluck("id", &affected).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, affected) {
		t.Fatalf("改址影响集合必须等于真实依赖集合: deps=%v affected=%v", ids, affected)
	}
}
