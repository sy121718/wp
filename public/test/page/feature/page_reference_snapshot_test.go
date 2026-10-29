package feature

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagedto "go_wp/internal/module/page/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/pipeline"
	json "go_wp/pkg/jsonx"
)

func TestPageBuildPublishKeepsFrozenBlockVersionAndDocument(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projectservice.NewService(projectmodel.NewProjectModel(db)))
	inner, err := blocks.Create(ctx, &blockdto.CreateReq{ProjectID: projectID, Name: "内部快照", Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"copy","type":"core.text","props":{"text":"OLD-SNAPSHOT-COPY"}}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := blocks.Create(ctx, &blockdto.CreateReq{ProjectID: projectID, Name: "外部快照", Document: json.RawMessage(fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nested","type":"core.globalref","props":{"blockId":%q}}]}`, inner.ID))})
	if err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"left","type":"core.globalref","props":{"blockId":%q}},{"id":"right","type":"core.globalref","props":{"blockId":%q}}]}`, outer.ID, outer.ID)
	page, err := svc.Create(ctx, &pagedto.CreateReq{ProjectID: projectID, Kind: "home", ContentTargetType: "none", DraftPath: "/frozen-reference", DraftDocument: json.RawMessage(doc)})
	if err != nil {
		t.Fatal(err)
	}
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := blocks.Update(ctx, &blockdto.UpdateReq{ID: inner.ID, Name: inner.Name, ExpectedVersion: uint64(inner.Version), Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"copy","type":"core.text","props":{"text":"NEW-LIVE-COPY"}}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID})
	if err != nil {
		t.Fatal(err)
	}
	if published.ActiveHash != built.StagedHash {
		t.Fatalf("Build→Publish不得混入新块版本: staged=%s active=%s", built.StagedHash, published.ActiveHash)
	}
	read := func(hash string) (string, pipeline.Manifest) {
		t.Helper()
		base := filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", hash)
		html, e := os.ReadFile(filepath.Join(base, "index.html"))
		if e != nil {
			t.Fatal(e)
		}
		raw, e := os.ReadFile(filepath.Join(base, "manifest.json"))
		if e != nil {
			t.Fatal(e)
		}
		var manifest pipeline.Manifest
		if e = json.Unmarshal(raw, &manifest); e != nil {
			t.Fatal(e)
		}
		return string(html), manifest
	}
	html, manifest := read(published.ActiveHash)
	if strings.Count(html, "OLD-SNAPSHOT-COPY") != 2 || strings.Contains(html, "NEW-LIVE-COPY") {
		t.Fatal("两个实例必须消费构建时同一原文")
	}
	var sourceDoc, frozenDoc any
	if err = json.Unmarshal(inner.Document, &sourceDoc); err != nil {
		t.Fatal(err)
	}
	if manifest.References != nil {
		if err = json.Unmarshal(manifest.References.Blocks[inner.ID].Document, &frozenDoc); err != nil {
			t.Fatal(err)
		}
	}
	if manifest.References == nil || len(manifest.References.Blocks) != 2 || manifest.References.Blocks[inner.ID].Version != inner.Version || !reflect.DeepEqual(sourceDoc, frozenDoc) {
		t.Fatalf("产物必须记录准确的id/version/document: %+v", manifest.References)
	}
	// 新构建属于新的发布决策，不能永久固定旧块；语言计划仍按原合同沿用。
	rebuilt, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatal(err)
	}
	html, next := read(rebuilt.StagedHash)
	if strings.Count(html, "NEW-LIVE-COPY") != 2 || next.References.Blocks[inner.ID].Version != updated.Version {
		t.Fatal("新构建必须固定新块版本")
	}
}
