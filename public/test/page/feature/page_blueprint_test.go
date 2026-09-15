package feature

// page_blueprint_test.go — 从蓝图新建页面（审计 VIS-010）。
//
// 蓝图（Blueprint）是创建 Page Document 的**版本化初始化输入**，用完即弃：
// InitPageDocument 复制完整 AST 并递归生成新节点 ID，得到不再依赖蓝图的独立文档。
//
// 这条能力此前只有后端实现，没有任何调用方 —— routes.go 里长期挂着一行
// `_ = blueprintSvc // 未来 page CreatePage 消费 InitPageDocument`，
// 新建页面流程永远传的是手写空文档。本用例钉住的是「接线真的通了」。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
)

// blueprints / blueprint_versions 由生产迁移建（库由 newPageService 内的
// support.NewMigratedPGTestDB 跑 migrations.Run 建出），不再手抄建表。

// blueprintDocument 蓝图文档：一个带固定节点 ID 的文本节点。
const blueprintDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"bp-node-1","type":"core.text","props":{"text":"FROM-BLUEPRINT"}}]}`

// TestPageCreateFromBlueprint 从蓝图建页：内容来自蓝图，节点 ID 已重生成。
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
	// 节点 ID 必须重生成：与蓝图文档原样相同意味着两个页面会共用同一批 sky-c-<id> CSS 类，
	// 样式互相污染（这正是 InitPageDocument 递归重生成 ID 的原因）。
	if strings.Contains(string(detail.DraftDocument), "bp-node-1") {
		t.Fatalf("蓝图节点 ID 应被重生成，实际文档: %s", string(detail.DraftDocument))
	}
}

// TestPageCreateWithoutBlueprintRejectsEmptyDocument 没文档也没蓝图：明确拒绝。
//
// 静默建空页最难被发现 —— 后台显示新建成功，编辑者打开画布才发现是白的。
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

// TestPageCreateFromBlueprintWithoutPortFails 蓝图端口未注入：报错而不是降级建空页。
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
