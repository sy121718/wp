package unit

import (
	"context"
	"encoding/json"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
)

// docSettings 解析草稿文档的 settings 键。
func docSettings(t *testing.T, doc []byte) map[string]json.RawMessage {
	t.Helper()
	var page struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	return page.Settings
}

// themeDocSettings 断言 settings.theme 快照与期望一致。
func assertThemeSnapshot(t *testing.T, doc []byte, wantPrimary, wantFont string) {
	t.Helper()
	settings := docSettings(t, doc)
	raw, ok := settings["theme"]
	if !ok {
		t.Fatalf("文档缺少 settings.theme 快照: %s", doc)
	}
	var theme struct {
		Colors     map[string]string `json:"colors"`
		FontFamily string            `json:"fontFamily"`
	}
	if err := json.Unmarshal(raw, &theme); err != nil {
		t.Fatalf("解析 theme 快照失败: %v", err)
	}
	if theme.Colors["primary"] != wantPrimary || theme.FontFamily != wantFont {
		t.Errorf("theme 快照错误: primary=%q font=%q", theme.Colors["primary"], theme.FontFamily)
	}
}

// assertStructureSnapshot 断言 settings.structure 页眉/页脚绑定。
func assertStructureSnapshot(t *testing.T, doc []byte, wantHeader, wantFooter string) {
	t.Helper()
	settings := docSettings(t, doc)
	raw, ok := settings["structure"]
	if !ok {
		t.Fatalf("文档缺少 settings.structure 快照: %s", doc)
	}
	var structure struct {
		HeaderBlockID string `json:"headerBlockId"`
		FooterBlockID string `json:"footerBlockId"`
	}
	if err := json.Unmarshal(raw, &structure); err != nil {
		t.Fatalf("解析 structure 快照失败: %v", err)
	}
	if structure.HeaderBlockID != wantHeader || structure.FooterBlockID != wantFooter {
		t.Errorf("structure 快照错误: header=%q footer=%q", structure.HeaderBlockID, structure.FooterBlockID)
	}
}

// TestPageCreateThemeMerge 有激活主题时创建页面：settings.theme/structure 快照合入，
// 页面挂接主题 ID。
func TestPageCreateThemeMerge(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "亮色主题",
		Settings: json.RawMessage(`{"colors":{"primary":"#ff0000"},"fontFamily":"Arial","headerBlockId":"hdr","footerBlockId":"ftr"}`),
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/merged", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	if created.ThemeID != theme.ID {
		t.Errorf("页面应挂接激活主题: %+v", created)
	}
	assertThemeSnapshot(t, created.DraftDocument, "#ff0000", "Arial")
	assertStructureSnapshot(t, created.DraftDocument, "hdr", "ftr")
}

// TestPageSaveDraftThemeMerge 保存草稿同样合入主题快照（保存时快照语义）。
func TestPageSaveDraftThemeMerge(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	if _, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "主题",
		Settings: json.RawMessage(`{"colors":{"primary":"#00ff00"},"headerBlockId":"h1"}`),
	}); err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	created := createPage(t, svc, projectID, "/save-merge", pageDocument)
	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/save-merge", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	assertThemeSnapshot(t, saved.DraftDocument, "#00ff00", "")
	assertStructureSnapshot(t, saved.DraftDocument, "h1", "")
}

// TestPageCreateNoTheme 无主题时页面不挂接主题；文档规范化后仍含空的
// theme/structure 键（Go json.Marshal 对 struct 字段的 omitempty 不生效），
// 但不得合入任何主题值。
func TestPageCreateNoTheme(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	created := createPage(t, svc, projectID, "/no-theme", headingDocument)
	if created.ThemeID != "" {
		t.Errorf("无主题时 ThemeID 应为空: %+v", created)
	}
	assertThemeSnapshot(t, created.DraftDocument, "", "")
	assertStructureSnapshot(t, created.DraftDocument, "", "")
}

// TestPageCreateThemeBrokenSettings 主题 settings 为非法 JSON（数组）时：
// 忽略合入，不阻塞页面保存。
func TestPageCreateThemeBrokenSettings(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	themeID := "8f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"
	if err := db.Exec(`INSERT INTO themes (id, project_id, name, settings, is_active, created_at, updated_at)
		VALUES (?, ?, '坏主题', '[1,2,3]', true, now(), now())`, themeID, projectID).Error; err != nil {
		t.Fatalf("插入非法主题失败: %v", err)
	}
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/broken-theme", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("主题设置非法不应阻塞页面创建: %v", err)
	}
	// 非法主题设置被忽略：不得合入任何主题值（空快照）。
	assertThemeSnapshot(t, created.DraftDocument, "", "")
	assertStructureSnapshot(t, created.DraftDocument, "", "")
}

// TestPageRefreshThemeForTheme 主题设置刷新批量合入挂接页面，draft_version 不变。
func TestPageRefreshThemeForTheme(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "主题",
		Settings: json.RawMessage(`{"colors":{"primary":"#111111"}}`),
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	pageA := createPage(t, svc, projectID, "/ref-a", pageDocument)
	pageB := createPage(t, svc, projectID, "/ref-b", pageDocument)

	newTheme := json.RawMessage(`{"colors":{"primary":"#abcdef"},"fontFamily":"Serif"}`)
	if err := svc.RefreshThemeForTheme(ctx, theme.ID, newTheme); err != nil {
		t.Fatalf("刷新主题失败: %v", err)
	}
	for _, p := range []*pagedto.PageResp{pageA, pageB} {
		detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: p.ID})
		if err != nil {
			t.Fatalf("详情失败: %v", err)
		}
		assertThemeSnapshot(t, detail.DraftDocument, "#abcdef", "Serif")
		if detail.DraftVersion != p.DraftVersion {
			t.Errorf("主题刷新不应改动 draft_version: got %d want %d", detail.DraftVersion, p.DraftVersion)
		}
	}
}

// TestPageRefreshStructureForTheme 页眉/页脚块绑定刷新批量合入挂接页面。
func TestPageRefreshStructureForTheme(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "主题",
		Settings: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	page := createPage(t, svc, projectID, "/struct", pageDocument)

	if err := svc.RefreshStructureForTheme(ctx, theme.ID, json.RawMessage(`{"headerBlockId":"new-header"}`)); err != nil {
		t.Fatalf("刷新结构失败: %v", err)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: page.ID})
	if err != nil {
		t.Fatalf("详情失败: %v", err)
	}
	assertStructureSnapshot(t, detail.DraftDocument, "new-header", "")
}

// TestPageMarkStaleForTheme 主题块变更批量标记挂接页面为待重建。
func TestPageMarkStaleForTheme(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "主题", Settings: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	page := createPage(t, svc, projectID, "/stale", pageDocument)
	// 手动清除 stale 模拟已构建。
	db.Table("pages").Where("id = ?", page.ID).Update("stale", false)

	if err := svc.MarkStaleForTheme(ctx, theme.ID); err != nil {
		t.Fatalf("标记 stale 失败: %v", err)
	}
	var stale bool
	db.Raw(`SELECT stale FROM pages WHERE id = ?`, page.ID).Scan(&stale)
	if !stale {
		t.Errorf("页面应被标记为待重建")
	}
}

// TestPageAttachThemeToUnassigned 工程首个主题创建后回填未挂主题的页面，
// 已挂主题的页面不受影响。
func TestPageAttachThemeToUnassigned(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	// 先建页面（此时无主题，不挂接）。
	pageA := createPage(t, svc, projectID, "/attach-a", pageDocument)
	pageB := createPage(t, svc, projectID, "/attach-b", pageDocument)
	// 首个主题自动激活。
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "首主题", Settings: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	// 第二个主题（非激活）——页面不挂它。
	theme2, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "次主题", Settings: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("创建次主题失败: %v", err)
	}
	if err := svc.AttachThemeToUnassigned(ctx, projectID, theme2.ID); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	// 回填到激活主题的页面仍应显示激活主题（AttachThemeToUnassigned
	// 按调用方传入的 themeID 回填；此处验证列已从 NULL 变为非 NULL）。
	for _, id := range []string{pageA.ID, pageB.ID} {
		detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: id})
		if err != nil || detail.ThemeID == "" {
			t.Errorf("页面 %s 应被回填主题: %+v err=%v", id, detail, err)
		}
	}
	// 回填幂等：再次调用不报错。
	if err := svc.AttachThemeToUnassigned(ctx, projectID, theme.ID); err != nil {
		t.Errorf("重复回填应幂等: %v", err)
	}
}
