package feature

import (
	"context"
	"fmt"
	"go_wp/internal/builder/source"
	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/pipeline"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
	"reflect"
	"strings"
	"testing"
)

func TestPublishedActionDependencyImpactAndDeleteProtection(t *testing.T) {
	db, api, projectID := newPageService(t)
	svc := api.(*pageservice.Service)
	ctx := context.Background()
	withDefaultPlainFeature(t)
	if err := db.Exec("INSERT INTO project_locales (project_id,lang,sort_order,is_default,enabled,create_time,update_time) VALUES (?, 'zh-CN',0,true,true,now(),now()), (?, 'en-US',1,false,true,now(),now())", projectID, projectID).Error; err != nil {
		t.Fatal(err)
	}
	create := func(path, doc string) string {
		t.Helper()
		row, err := api.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: path, DraftDocument: json.RawMessage(doc)})
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	publish := func(id string) {
		t.Helper()
		if _, err := api.Build(ctx, &pagedto.BuildReq{ID: id, Lang: "en-US"}); err != nil {
			t.Fatal(err)
		}
		if _, err := api.Publish(ctx, &pagedto.PublishReq{ID: id, Lang: "en-US"}); err != nil {
			t.Fatal(err)
		}
	}
	target := create("/controlled-target", slotPlainDocument)
	publish(target)
	sourceDoc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"cart","type":"core.cartIcon","props":{"mode":"inline"},"actions":{"checkout":{"kind":"page","pageId":%q}}}]}`, target)
	consumer := create("/actual-consumer", sourceDoc)
	publish(consumer)
	plain := create("/unrelated", slotPlainDocument)
	publish(plain)
	key := pipeline.PageTargetKey(target)
	refs, err := svc.FindPagesByDependency(ctx, projectID, key.Kind, key.Key)
	if err != nil || len(refs) != 1 || refs[0].ID != consumer {
		t.Fatalf("真实Compile依赖只含消费页: %+v %v", refs, err)
	}
	if err := api.Delete(ctx, &pagedto.DeleteReq{ID: target}); err == nil {
		t.Fatal("有源码或产物引用的目标不能删除")
	} else {
		status, ok := facing.StatusOf(err)
		fact, factOK := facing.FactOf(err)
		if !ok || status != 409 || !factOK || !strings.Contains(fact.Fallback, "引用") {
			t.Fatalf("引用冲突必须是对外409事实: %+v %d %v", fact, status, err)
		}
	}
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePage, svc)
	svc.SetActionTargetInvalidator(fanout)
	if _, err := api.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: target, NewPath: "/target-moved", Lang: "en-US"}); err != nil {
		t.Fatal(err)
	}

	detail, err := api.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: target})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, pub := range detail.Publications {
		if pub.Lang != "en-US" {
			continue
		}
		found = true
		if pub.ArtifactID == nil || *pub.ArtifactID == "" || pub.ArtifactHash == "" {
			t.Fatalf("改址账本必须包含新产物: %+v", pub)
		}
		var rowHash string
		if err := db.Table("page_artifacts").Select("artifact_hash").Where("id = ?", *pub.ArtifactID).Scan(&rowHash).Error; err != nil {
			t.Fatal(err)
		}
		path, err := svc.PublishedPagePath(ctx, projectID, target, "en-US")
		if err != nil || path != pub.ActivePath || rowHash != pub.ArtifactHash {
			t.Fatalf("路径、产物行、hash、激活文件必须一致: path=%s pub=%+v row=%s err=%v", path, pub, rowHash, err)
		}
	}
	if !found {
		t.Fatal("缺改址语言Publication")
	}
	var affected []string
	if err := db.Table("pages").Where("id IN ? AND stale = true", []string{consumer, plain}).Order("id").Pluck("id", &affected).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(affected, []string{consumer}) {
		t.Fatalf("改址影响集合须与Compile依赖相等: refs=%+v affected=%v", refs, affected)
	}
	publish(consumer)
	if _, err := svc.ExcludePageLang(ctx, target, "en-US"); err != nil {
		t.Fatal(err)
	}
	affected = nil
	if err := db.Table("pages").Where("id IN ? AND stale = true", []string{consumer, plain}).Order("id").Pluck("id", &affected).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(affected, []string{consumer}) {
		t.Fatalf("下线同样只标记真实依赖: %v", affected)
	}
	if _, err := api.Build(ctx, &pagedto.BuildReq{ID: consumer, Lang: "en-US"}); err == nil {
		t.Fatal("目标该语言下线后不能再发布死链接")
	}
	// 尚未构建的文档同样进入删除保护，而不靠虚构产物依赖。
	draftTarget := create("/draft-target", slotPlainDocument)
	draftRef := create("/draft-ref", strings.ReplaceAll(sourceDoc, target, draftTarget))
	current, err := svc.ListActionTargetReferences(ctx, projectID, "page", draftTarget)
	if err != nil || !reflect.DeepEqual(current, []source.ActionTargetReference{{SourceKind: "page", SourceID: draftRef}}) {
		t.Fatalf("未构建草稿引用必须可定位: %+v %v", current, err)
	}
	if err := api.Delete(ctx, &pagedto.DeleteReq{ID: draftTarget}); err == nil {
		t.Fatal("未构建草稿引用也不能被删成悬空")
	}
}
