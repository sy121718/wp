package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
)

const blueprintDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"bp-node-1","type":"core.text","props":{"text":"FROM-BLUEPRINT"}}]}`

func TestPageCreateFromBlueprint(t *testing.T) {
	db, pageSvc, projectID := newPageService(t)
	ctx := context.Background()
	bpSvc := blueprintservice.NewService(blueprintmodel.NewModel(db))
	injectable, ok := pageSvc.(interface {
		SetBlueprints(blueprintcontract.BlueprintService)
	})
	if !ok {
		t.Fatalf("page service 应提供蓝图注入点（SetBlueprints）")
	}
	injectable.SetBlueprints(bpSvc)

	bp, err := bpSvc.Create(ctx, &blueprintdto.CreateReq{
		Name: "落地页骨架", Kind: "page", DraftDocument: json.RawMessage(blueprintDocument),
	})
	if err != nil {
		t.Fatalf("创建蓝图失败: %v", err)
	}

	created, err := pageSvc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/from-blueprint", BlueprintID: bp.ID,
	})
	if err != nil {
		t.Fatalf("从蓝图建页失败: %v", err)
	}
	detail, err := pageSvc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if err != nil {
		t.Fatalf("查询新页面失败: %v", err)
	}
	if !strings.Contains(string(detail.DraftDocument), "FROM-BLUEPRINT") {
		t.Fatalf("页面文档应来自蓝图，实际: %s", string(detail.DraftDocument)[:min(len(detail.DraftDocument), 200)])
	}

	if strings.Contains(string(detail.DraftDocument), "bp-node-1") {
		t.Fatalf("蓝图节点 ID 应被重生成，实际文档: %s", string(detail.DraftDocument))
	}
}

func TestPageCreateWithoutBlueprintRejectsEmptyDocument(t *testing.T) {
	_, pageSvc, projectID := newPageService(t)
	ctx := context.Background()

	if _, err := pageSvc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/no-doc",
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidParam) {
		t.Fatalf("既无文档也无蓝图时应返回 %s，实际: %v", pageenums.ErrInvalidParam, err)
	}
}

func TestPageCreateFromBlueprintWithoutPortFails(t *testing.T) {
	_, pageSvc, projectID := newPageService(t)
	ctx := context.Background()

	if _, err := pageSvc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/bp-missing", BlueprintID: "00000000-0000-0000-0000-000000000001",
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrBlueprintUnavailable) {
		t.Fatalf("未注入蓝图端口时应返回 %s，实际: %v", pageenums.ErrBlueprintUnavailable, err)
	}
}
