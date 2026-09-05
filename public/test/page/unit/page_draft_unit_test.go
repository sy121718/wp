package unit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
)

// ---- 创建：正常路径 ----

// TestPageCreateSuccess 合法请求创建页面：路径规范化、版本 1、stale、
// 初始修订与 reserved 路由占用全部就位。
func TestPageCreateSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about/", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if created.DraftPath != "/about" {
		t.Errorf("路径应规范化去尾斜杠: %s", created.DraftPath)
	}
	if created.DraftVersion != 1 || !created.Stale {
		t.Errorf("初始状态错误: version=%d stale=%v", created.DraftVersion, created.Stale)
	}
	if created.ID == "" || created.ProjectID != projectID || created.Kind != "home" {
		t.Errorf("投影字段错误: %+v", created)
	}
	var revisionCount, routeCount int64
	db.Table("page_revisions").Where("page_id = ?", created.ID).Count(&revisionCount)
	db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/about").Count(&routeCount)
	if revisionCount != 1 || routeCount != 1 {
		t.Errorf("初始修订/路由占用错误: revisions=%d routes=%d", revisionCount, routeCount)
	}
	if kind := routeKind(t, db, projectID, "/about"); kind != "reserved" {
		t.Errorf("初始路由应为 reserved: %s", kind)
	}
}

// TestPageCreateBindingTarget kind=page 携带内容目标 ID 应创建成功。
func TestPageCreateBindingTarget(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	targetID := "9f0d3a4e-5b6c-4d1e-8f2a-1b2c3d4e5f60"
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "page", ContentTargetType: "page", ContentTargetID: &targetID,
		DraftPath: "/news/hello", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("绑定目标创建失败: %v", err)
	}
	if created.ContentTargetType != "page" || created.ContentTargetID == nil || *created.ContentTargetID != targetID {
		t.Errorf("内容目标未保留: %+v", created)
	}
}

// TestPageCreateKindMatrix kind/target 组合矩阵。
func TestPageCreateKindMatrix(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	targetID := "9f0d3a4e-5b6c-4d1e-8f2a-1b2c3d4e5f61"

	cases := []struct {
		kind, targetType string
		target           *string
		wantErr          bool
	}{
		{"home", "none", nil, false},
		{"archive", "none", nil, false},
		{"search", "none", nil, false},
		{"notFound", "none", nil, false},
		{"article", "article", &targetID, false},
		{"product", "product", &targetID, false},
		{"category", "category", &targetID, false},
		{"tag", "tag", &targetID, false},
		{"home", "page", &targetID, true},
		{"page", "none", nil, true},
		{"page", "article", &targetID, true},
		{"unknown", "none", nil, true},
	}
	for i, c := range cases {
		_, err := svc.Create(ctx, &pagedto.CreateReq{
			ProjectID: projectID, Kind: c.kind, ContentTargetType: c.targetType,
			ContentTargetID: c.target, DraftPath: "/case" + string(rune('a'+i)),
			DraftDocument: json.RawMessage(pageDocument),
		})
		if (err != nil) != c.wantErr {
			t.Errorf("kind=%s target=%s: 期望错误=%v 实际=%v", c.kind, c.targetType, c.wantErr, err)
		}
	}
}

// ---- 创建：输入校验与恶意输入 ----

// TestPageCreateNilRequest nil 请求应返回参数无效错误。
func TestPageCreateNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Create(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Fatalf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

// TestPageCreateInvalidDocumentMalformedJSON 畸形 JSON 应被拒绝。
func TestPageCreateInvalidDocumentMalformedJSON(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	_, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/malformed", DraftDocument: json.RawMessage(`{"settings":`),
	})
	if err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidDocument) {
		t.Fatalf("畸形 JSON 应被拒绝: %v", err)
	}
}

// TestPageCreateEmptyDocument 空对象 / null 文档（缺 layout.mode）应被拒绝。
func TestPageCreateEmptyDocument(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	for _, doc := range []string{`{}`, `null`, `{"root":[]}`} {
		_, err := svc.Create(context.Background(), &pagedto.CreateReq{
			ProjectID: projectID, Kind: "home", ContentTargetType: "none",
			DraftPath: "/empty", DraftDocument: json.RawMessage(doc),
		})
		if err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidDocument) {
			t.Errorf("文档 %s 应被拒绝: %v", doc, err)
		}
	}
}

// TestPageCreateEmptyRoot 空 root 节点列表是合法文档。
func TestPageCreateEmptyRoot(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/empty-root", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil || created == nil {
		t.Fatalf("空 root 应可创建: %v", err)
	}
}

// TestPageCreateOversizeDocument 超长内容（SEO 标题 201 字符 / 描述 501 字符 /
// 节点文本 501 字符 / 节点名 101 字符）应被拒绝。
func TestPageCreateOversizeDocument(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	title := strings.Repeat("长", 201)
	desc := strings.Repeat("长", 501)
	longText := strings.Repeat("x", 501)
	longName := strings.Repeat("名", 101)

	docs := []string{
		`{"settings":{"layout":{"mode":"full"},"seo":{"title":"` + title + `"}},"root":[]}`,
		`{"settings":{"layout":{"mode":"full"},"seo":{"description":"` + desc + `"}},"root":[]}`,
		`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"` + longText + `"}}]}`,
		`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","name":"` + longName + `","type":"core.heading","props":{"text":"ok"}}]}`,
	}
	for i, doc := range docs {
		_, err := svc.Create(ctx, &pagedto.CreateReq{
			ProjectID: projectID, Kind: "home", ContentTargetType: "none",
			DraftPath: "/oversize" + string(rune('0'+i)), DraftDocument: json.RawMessage(doc),
		})
		if err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidDocument) {
			t.Errorf("超长文档 #%d 应被拒绝: %v", i, err)
		}
	}
}

// TestPageCreateUnknownProject 工程不存在返回工程不存在；空工程 ID 属于参数错误。
func TestPageCreateUnknownProject(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f", Kind: "home", ContentTargetType: "none",
		DraftPath: "/nop", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || err.Error() != pageenums.ErrProjectNotFound {
		t.Errorf("不存在的工程应返回 %q: %v", pageenums.ErrProjectNotFound, err)
	}
	if _, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: "  ", Kind: "home", ContentTargetType: "none",
		DraftPath: "/nop2", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空工程 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

// TestPageCreateXSSDocument XSS 字符串作为结构化文本 props 合法保存（构建期转义，
// service 层不得拒绝合法文档，也不得崩溃）。存储为规范化 JSON
// （Go json.Marshal 默认 HTML 转义 < → \u003c，解析后文本语义不变）。
func TestPageCreateXSSDocument(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	xssText := `<script>alert(1)</script><img src=x onerror=alert(2)>`
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"x1","type":"core.heading","props":{"text":"` + xssText + `"}}]}`
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/xss", DraftDocument: json.RawMessage(doc),
	})
	if err != nil {
		t.Fatalf("XSS 文本应作为普通字符串保存: %v", err)
	}
	var stored struct {
		Root []struct {
			Props struct {
				Text string `json:"text"`
			} `json:"props"`
		} `json:"root"`
	}
	if err := json.Unmarshal(created.DraftDocument, &stored); err != nil {
		t.Fatalf("解析保存的文档失败: %v", err)
	}
	if len(stored.Root) != 1 || stored.Root[0].Props.Text != xssText {
		t.Errorf("保存的文档应保留文本原义: %+v", stored)
	}
}

// ---- 创建：路径校验 ----

// TestPageCreateInvalidPath 保留路径、路径穿越、空格、非法字符应被拒绝。
func TestPageCreateInvalidPath(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	for _, path := range []string{"/admin", "/admin/console", "/api/users", "/assets/x", "/%2e%2e/escape", "/a/../b", "/a//b", "/a\\b", "/a b", "relative", ""} {
		_, err := svc.Create(ctx, &pagedto.CreateReq{
			ProjectID: projectID, Kind: "home", ContentTargetType: "none",
			DraftPath: path, DraftDocument: json.RawMessage(pageDocument),
		})
		if err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidPath) {
			t.Errorf("路径 %q 应被拒绝为 %q: %v", path, pageenums.ErrInvalidPath, err)
		}
	}
}

// TestPageCreateDuplicatePath 同工程重复路径应被拒绝（reserved 占用）。
func TestPageCreateDuplicatePath(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	req := &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/dup", DraftDocument: json.RawMessage(pageDocument),
	}
	if _, err := svc.Create(ctx, req); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if _, err := svc.Create(ctx, req); err == nil || !strings.Contains(err.Error(), pageenums.ErrPathOccupied) {
		t.Fatalf("重复路径应被拒绝: %v", err)
	}
}

// ---- 保存草稿 ----

// TestPageSaveDraftSuccess 保存草稿：版本递增、修订追加、stale 置位、文档更新。
func TestPageSaveDraftSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/about", pageDocument)

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/about", DraftDocument: json.RawMessage(headingDocument),
	})
	if err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	if saved.DraftVersion != 2 || !saved.Stale {
		t.Errorf("保存后状态错误: version=%d stale=%v", saved.DraftVersion, saved.Stale)
	}
	if !strings.Contains(string(saved.DraftDocument), "core.heading") {
		t.Errorf("文档未更新: %s", saved.DraftDocument)
	}
	var revisionCount int64
	db.Table("page_revisions").Where("page_id = ?", created.ID).Count(&revisionCount)
	if revisionCount != 2 {
		t.Errorf("修订应追加为 2 条: %d", revisionCount)
	}
	// 修订快照不可变：v1 保留旧文档。
	var v1Doc string
	db.Raw(`SELECT draft_document::text FROM page_revisions WHERE page_id = ? AND version = 1`, created.ID).Scan(&v1Doc)
	if strings.Contains(v1Doc, "core.heading") {
		t.Errorf("v1 修订快照不应被改写")
	}
}

// TestPageSaveDraftVersionConflict 旧版本乐观锁冲突：拒绝且不写修订。
func TestPageSaveDraftVersionConflict(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/about", pageDocument)

	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion + 5,
		DraftPath: "/about", DraftDocument: json.RawMessage(headingDocument),
	}); err == nil || err.Error() != pageenums.ErrDraftVersionConflict {
		t.Fatalf("版本冲突应被拒绝: %v", err)
	}
	var revisionCount int64
	db.Table("page_revisions").Where("page_id = ?", created.ID).Count(&revisionCount)
	if revisionCount != 1 {
		t.Errorf("冲突写入不得产生修订: %d", revisionCount)
	}
}

// TestPageSaveDraftNotFound 不存在的页面保存应返回页面不存在。
func TestPageSaveDraftNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.SaveDraft(context.Background(), &pagedto.SaveDraftReq{
		ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f", ExpectedVersion: 1,
		DraftPath: "/ghost", DraftDocument: json.RawMessage(pageDocument),
	})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageSaveDraftNilRequest nil / 空 ID 请求属于参数错误。
func TestPageSaveDraftNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.SaveDraft(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.SaveDraft(context.Background(), &pagedto.SaveDraftReq{
		ExpectedVersion: 1, DraftPath: "/x", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

// TestPageSaveDraftInvalidDocument 非法文档（缺 layout.mode）应被拒绝且不写修订。
func TestPageSaveDraftInvalidDocument(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/about", pageDocument)

	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/about", DraftDocument: json.RawMessage(`{"root":[]}`),
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrInvalidDocument) {
		t.Fatalf("非法文档应被拒绝: %v", err)
	}
	var revisionCount int64
	db.Table("page_revisions").Where("page_id = ?", created.ID).Count(&revisionCount)
	if revisionCount != 1 {
		t.Errorf("非法保存不得产生修订: %d", revisionCount)
	}
}

// TestPageSaveDraftPathChange 改草稿路径后：旧 reserved 释放、新 reserved 建立。
func TestPageSaveDraftPathChange(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/old", pageDocument)

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/new", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("改路径保存失败: %v", err)
	}
	if saved.DraftPath != "/new" {
		t.Errorf("草稿路径未迁移: %s", saved.DraftPath)
	}
	var oldCount, newCount int64
	db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/old").Count(&oldCount)
	db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/new").Count(&newCount)
	if oldCount != 0 || newCount != 1 {
		t.Errorf("路由占用应迁移: old=%d new=%d", oldCount, newCount)
	}
}

// TestPageSaveDraftPathOccupied 草稿路径改为他人占用路径应被拒绝（事务回滚）。
func TestPageSaveDraftPathOccupied(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	pageA := createPage(t, svc, projectID, "/a", pageDocument)
	createPage(t, svc, projectID, "/b", pageDocument)

	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: pageA.ID, ExpectedVersion: pageA.DraftVersion,
		DraftPath: "/b", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || !strings.Contains(err.Error(), pageenums.ErrPathOccupied) {
		t.Fatalf("改到他人路径应被拒绝: %v", err)
	}
	// 事务回滚：A 路径与版本均未变，B 占用完好。
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: pageA.ID})
	if err != nil || detail.DraftPath != "/a" || detail.DraftVersion != pageA.DraftVersion {
		t.Errorf("失败保存不应迁移路径或版本: %+v err=%v", detail, err)
	}
	if kind := routeKind(t, db, projectID, "/b"); kind != "reserved" {
		t.Errorf("他人占用不应被动摇: %s", kind)
	}
}
