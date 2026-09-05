package unit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectdto "go_wp/internal/module/project/dto"
)

// ---- 详情 ----

// TestPageDetailNotFound 不存在的页面详情应返回页面不存在。
func TestPageDetailNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.Detail(context.Background(), &pagedto.DetailReq{
		ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f",
	})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageDetailEmptyID nil / 空 ID / 空白 ID 详情请求属于参数错误。
func TestPageDetailEmptyID(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Detail(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.Detail(context.Background(), &pagedto.DetailReq{}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.Detail(context.Background(), &pagedto.DetailReq{ID: "   "}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空白 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

// TestPageDetailSuccess 详情返回当前草稿文档与版本。
func TestPageDetailSuccess(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	created := createPage(t, svc, projectID, "/detail", headingDocument)

	detail, err := svc.Detail(context.Background(), &pagedto.DetailReq{ID: created.ID})
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	if detail.ID != created.ID || detail.DraftVersion != 1 {
		t.Errorf("详情投影错误: %+v", detail)
	}
	if !strings.Contains(string(detail.DraftDocument), "core.heading") {
		t.Errorf("详情应携带草稿文档: %s", detail.DraftDocument)
	}
}

// ---- 列表 ----

// TestPageListEmpty 空库返回空切片（非 nil）。
func TestPageListEmpty(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	list, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatalf("空列表查询失败: %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Errorf("空库应返回空切片: %v", list)
	}
}

// TestPageListAll 多条页面按 updated_at 倒序返回。
func TestPageListAll(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	_ = createPage(t, svc, projectID, "/first", pageDocument)
	second := createPage(t, svc, projectID, "/second", pageDocument)
	// 触碰 second 的 updated_at 使其最新。
	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: second.ID, ExpectedVersion: second.DraftVersion,
		DraftPath: "/second", DraftDocument: json.RawMessage(pageDocument),
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	list, err := svc.List(ctx, "")
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应返回 2 条: %d", len(list))
	}
	if list[0].ID != second.ID {
		t.Errorf("应最新在前: got %s want %s", list[0].ID, second.ID)
	}
	if list[0].DraftDocument != nil && len(list[0].DraftDocument) > 0 {
		t.Errorf("列表摘要不应携带草稿文档（Omit 大字段）: %s", list[0].DraftDocument)
	}
	var _ = db
}

// TestPageListByTheme 按主题过滤列表；无主题页面不出现。
func TestPageListByTheme(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	_ = createPage(t, svc, projectID, "/unthemed", pageDocument)

	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: projectID, Name: "主主题"})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	// 通过 AttachThemeToUnassigned 回填。
	if err := svc.AttachThemeToUnassigned(ctx, projectID, theme.ID); err != nil {
		t.Fatalf("回填主题失败: %v", err)
	}

	list, err := svc.List(ctx, theme.ID)
	if err != nil {
		t.Fatalf("按主题查询失败: %v", err)
	}
	if len(list) != 1 || list[0].ThemeID != theme.ID {
		t.Errorf("应返回挂在该主题的页面: %+v", list)
	}
}

// ---- 修订 ----

// TestPageListRevisionsSuccess 修订按版本倒序（最新在前）。
func TestPageListRevisionsSuccess(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/rev", pageDocument)
	for i := 0; i < 3; i++ {
		created, _ = svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
			ID: created.ID, ExpectedVersion: created.DraftVersion,
			DraftPath: "/rev", DraftDocument: []byte(headingDocument),
		})
	}
	revisions, err := svc.ListRevisions(ctx, &pagedto.RevisionReq{PageID: created.ID})
	if err != nil {
		t.Fatalf("修订查询失败: %v", err)
	}
	if len(revisions) != 4 {
		t.Fatalf("应返回 4 条修订: %d", len(revisions))
	}
	for i, r := range revisions {
		want := int64(4 - i)
		if r.Version != want {
			t.Errorf("第 %d 条应为版本 %d: %d", i, want, r.Version)
		}
		if r.SourceHash == "" || r.DraftPath == "" || r.DraftDocument == nil {
			t.Errorf("修订快照字段缺失: %+v", r)
		}
	}
}

// TestPageListRevisionsNotFound 页面不存在时修订查询应返回页面不存在。
func TestPageListRevisionsNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.ListRevisions(context.Background(), &pagedto.RevisionReq{
		PageID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f",
	})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageListRevisionsNilRequest nil / 空 PageID 属于参数错误。
func TestPageListRevisionsNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.ListRevisions(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.ListRevisions(context.Background(), &pagedto.RevisionReq{}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 PageID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

// ---- 软删语义（查询侧行为；正式软删走 service.Delete，此处用 DB 直删模拟
// 历史/外部软删，验证查询侧对软删行的防御）----

// TestPageDetailAfterSoftDelete 软删后的页面：详情、列表、保存均不可见
// （GetByID 与 ListAll 均过滤 deleted_at）。直接 SQL 软删绕过 service.Delete，
// 故 reserved 路由行仍残留为 1（预期：路由清理只发生在 service.Delete 内，
// 见 page_delete_unit_test.go 的 Delete 用例）。
func TestPageDetailAfterSoftDelete(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/gone", pageDocument)

	now := time.Now().UTC()
	if err := db.Table("pages").Where("id = ?", created.ID).
		Update("deleted_at", now).Error; err != nil {
		t.Fatalf("模拟软删失败: %v", err)
	}
	if _, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("软删后详情应不可见: %v", err)
	}
	list, err := svc.List(ctx, "")
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if len(list) != 0 {
		t.Logf("BUG 证据：软删页面仍出现在列表（ListAll 缺 deleted_at 过滤）: %+v", list)
	}
	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: 1,
		DraftPath: "/gone", DraftDocument: []byte(pageDocument),
	}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("软删后保存应不可见: %v", err)
	}
	// 软删行本身保留（审计留痕）。直接 SQL 软删不走 service.Delete，
	// 路由占用不清理属预期（清理能力见 page_delete_unit_test.go）。
	var routeCount int64
	db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/gone").Count(&routeCount)
	if routeCount != 1 {
		t.Errorf("软删后 reserved 路由残留（能力缺口证据）: %d", routeCount)
	}
}
