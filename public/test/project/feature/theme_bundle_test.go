package feature

// theme_bundle_test.go — 主题包（审计 VIS-014）feature 链路：
// 导出的包在另一个工程导入后令牌与块内容一致；版本不匹配被拒绝；媒体缺失给出明确清单。

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

const bundleMissingMediaURL = "/storage/theme-bundle/missing.jpg"

// newBundleEnv 建隔离库（真实生产迁移）并装配 project + block 服务，注入主题包端口。
// 页面端口传 nil：页面能力由假端口用例单独覆盖，这里验证真实块链路。
func newBundleEnv(t *testing.T) (projects *projectservice.Service, blocks *blockservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	projects = projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks = blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	projects.SetThemeBundleAssetPort(projectservice.NewThemeBundleAssetPort(blocks, nil))
	return projects, blocks
}

// setBundleMediaRoot 指定主题包媒体根目录（测试隔离：不要把字节写进仓库的 public/storage）。
func setBundleMediaRoot(t *testing.T, root string) {
	t.Helper()
	old, had := os.LookupEnv("GO_WP_THEME_MEDIA_ROOT")
	if err := os.Setenv("GO_WP_THEME_MEDIA_ROOT", root); err != nil {
		t.Fatalf("设置媒体根目录失败: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("GO_WP_THEME_MEDIA_ROOT", old)
			return
		}
		_ = os.Unsetenv("GO_WP_THEME_MEDIA_ROOT")
	})
}

// marshalDoc 构造页面/块同构文档（settings.layout.mode 必须存在）。
func marshalDoc(t *testing.T, root []any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
		"root":     root,
	})
	if err != nil {
		t.Fatalf("构造文档失败: %v", err)
	}
	return raw
}

// containerDoc 一份最小的合法块文档（容器节点）。
func containerDoc(t *testing.T) json.RawMessage {
	t.Helper()
	return marshalDoc(t, []any{
		map[string]any{
			"id": "n1", "type": "core.container",
			"props": map[string]any{
				"tag":    "div",
				"layout": map[string]any{"engine": "flex", "flex": map[string]any{}},
			},
		},
	})
}

// createBlock 建一个块并返回投影。
func createBlock(t *testing.T, blocks *blockservice.Service, projectID, name, kind string, doc json.RawMessage) *blockdto.BlockResp {
	t.Helper()
	res, err := blocks.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: projectID, Name: name, Kind: kind, Document: doc,
	})
	if err != nil {
		t.Fatalf("创建块 %s 失败: %v", name, err)
	}
	return res
}

// createTheme 建主题（settings 由调用方给出）。
func createTheme(t *testing.T, projects *projectservice.Service, projectID, name string, settings map[string]any) *projectdto.ThemeResp {
	t.Helper()
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("构造主题设置失败: %v", err)
	}
	res, err := projects.CreateTheme(context.Background(), &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: name, Settings: raw,
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	return res
}

// zipEntry 取包内某个条目的字节。
func zipEntry(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("导出结果不是合法 zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			t.Fatalf("打开包内条目失败: %v", oerr)
		}
		raw, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			t.Fatalf("读取包内条目失败: %v", rerr)
		}
		return raw
	}
	t.Fatalf("包内缺少条目：%s", name)
	return nil
}

// bundleBlockIDByName 从导入报告里按块名取新 id。
func bundleBlockIDByName(t *testing.T, report *projectdto.ThemeBundleImportResp, name string) string {
	t.Helper()
	for _, b := range report.Blocks {
		if b.Name == name {
			return b.ID
		}
	}
	t.Fatalf("导入报告里没有名为 %s 的块", name)
	return ""
}

// blockRefsOf 取文档里出现的块引用值（props.blockId 形态）。
func blockRefsOf(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	out := []string{}
	var walk func(node any)
	walk = func(node any) {
		switch val := node.(type) {
		case map[string]any:
			for k, child := range val {
				if k == "blockId" {
					if s, ok := child.(string); ok {
						out = append(out, s)
					}
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range val {
				walk(child)
			}
		}
	}
	walk(doc)
	return out
}

// settingsOf 解析主题 settings 为键 → 原始 JSON 片段。
func settingsOf(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析主题设置失败: %v", err)
	}
	return out
}

// stripStructure 去掉结构分区与只读元数据，只留设计令牌（用于与源主题比较）。
func stripStructure(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	obj := map[string]any{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("解析主题设置失败: %v", err)
	}
	for _, key := range []string{"headerBlockId", "footerBlockId", "slots", "themeId"} {
		delete(obj, key)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("序列化令牌失败: %v", err)
	}
	return out
}

// TestThemeBundleExportImportRoundTrip 导出 → 导入到另一个工程：令牌一致、块内容一致、id 全部重分配。
func TestThemeBundleExportImportRoundTrip(t *testing.T) {
	setBundleMediaRoot(t, t.TempDir())
	projects, blocks := newBundleEnv(t)
	ctx := context.Background()

	src, err := projects.Create(ctx, &projectdto.CreateReq{Name: "源站点"})
	if err != nil {
		t.Fatalf("建源工程失败: %v", err)
	}
	dst, err := projects.Create(ctx, &projectdto.CreateReq{Name: "目标站点"})
	if err != nil {
		t.Fatalf("建目标工程失败: %v", err)
	}

	child := createBlock(t, blocks, src.ID, "内容段", "block", containerDoc(t))
	footer := createBlock(t, blocks, src.ID, "页脚", "footer", containerDoc(t))
	headerDoc := marshalDoc(t, []any{
		map[string]any{
			"id": "h1", "type": "core.globalref",
			"props": map[string]any{"blockId": child.ID},
		},
	})
	header := createBlock(t, blocks, src.ID, "页眉", "header", headerDoc)

	theme := createTheme(t, projects, src.ID, "春季主题", map[string]any{
		"colors":        map[string]any{"primary": "#123456", "heading": "#101010"},
		"typography":    map[string]any{"body": map[string]any{"fontSize": "17px"}},
		"headerBlockId": header.ID,
		"footerBlockId": footer.ID,
		"slots":         map[string]any{"announcement": child.ID},
		// 只读元数据不该随包走：它是存储层注入的主题标识，带过去会让两个工程共用同一个标识。
		"themeId": "should-not-travel",
	})

	exported, err := projects.ExportThemeBundle(ctx, &projectdto.ThemeBundleExportReq{ThemeID: theme.ID})
	if err != nil {
		t.Fatalf("导出主题包失败: %v", err)
	}
	if exported.BlockCount != 3 {
		t.Fatalf("引用闭包应含 3 个块（页眉 + 页脚 + 页眉引用的内容段），实际 %d", exported.BlockCount)
	}
	if exported.PageCount != 0 {
		t.Fatalf("未要求带页面时不应导出页面，实际 %d", exported.PageCount)
	}
	if len(exported.Warnings) != 0 {
		t.Fatalf("干净主题不应有告警: %v", exported.Warnings)
	}
	if !strings.HasSuffix(exported.FileName, ".skintheme.zip") {
		t.Fatalf("包文件名后缀不符: %s", exported.FileName)
	}
	// 包内文档里的块引用必须是包内 key，而不是原 id（原 id 一旦进包就有被沿用的可能）。
	rawHeader := zipEntry(t, exported.Bytes, "blocks/b1.json")
	if bytes.Contains(rawHeader, []byte(header.ID)) {
		t.Fatalf("包内块文档仍含原块 id：%s", string(rawHeader))
	}
	var headerEntry struct {
		Document json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(rawHeader, &headerEntry); err != nil {
		t.Fatalf("解析包内块条目失败: %v", err)
	}
	bundleKeys := map[string]bool{"b1": true, "b2": true, "b3": true}
	keyRefs := blockRefsOf(t, headerEntry.Document)
	if len(keyRefs) != 1 || !bundleKeys[keyRefs[0]] || keyRefs[0] == child.ID {
		t.Fatalf("包内块文档里的引用应是包内 key（b1..b3），实际 %v", keyRefs)
	}

	imported, err := projects.ImportThemeBundle(ctx, &projectdto.ThemeBundleImportReq{ProjectID: dst.ID, Zip: exported.Bytes})
	if err != nil {
		t.Fatalf("导入主题包失败: %v", err)
	}
	if imported.ThemeID == theme.ID {
		t.Fatalf("导入应新建主题，不是复用源主题")
	}
	if imported.NameAdjusted {
		t.Fatalf("目标工程无同名主题，不该改名")
	}
	if len(imported.Blocks) != 3 {
		t.Fatalf("应导入 3 个块，实际 %d", len(imported.Blocks))
	}
	if len(imported.Media.Missing) != 0 {
		t.Fatalf("无媒体引用时不应有缺失清单: %v", imported.Media.Missing)
	}

	// 令牌一致（去掉结构分区后比较关键令牌）。
	gotTheme, err := projects.GetTheme(ctx, imported.ThemeID)
	if err != nil {
		t.Fatalf("回读导入主题失败: %v", err)
	}
	wantTokens, werr := builder.ParseThemeSettings(stripStructure(t, theme.Settings))
	gotTokens, gerr := builder.ParseThemeSettings(stripStructure(t, gotTheme.Settings))
	if werr != nil || gerr != nil {
		t.Fatalf("解析令牌失败: want=%v got=%v", werr, gerr)
	}
	if gotTokens.Colors.Primary != wantTokens.Colors.Primary || gotTokens.Colors.Primary != "#123456" {
		t.Fatalf("主色令牌不一致: %s vs %s", gotTokens.Colors.Primary, wantTokens.Colors.Primary)
	}
	if gotTokens.Typography.Body.FontSize != wantTokens.Typography.Body.FontSize || gotTokens.Typography.Body.FontSize != "17px" {
		t.Fatalf("正文字号令牌不一致: %s", gotTokens.Typography.Body.FontSize)
	}
	if gotTokens.ThemeID != "" {
		t.Fatalf("只读元数据 themeId 不应随包导入: %s", gotTokens.ThemeID)
	}

	// 结构绑定指向新块 id。
	gotSettings := settingsOf(t, gotTheme.Settings)
	newHeaderID := bundleBlockIDByName(t, imported, "页眉")
	newFooterID := bundleBlockIDByName(t, imported, "页脚")
	newChildID := bundleBlockIDByName(t, imported, "内容段")
	for _, old := range []string{header.ID, footer.ID, child.ID} {
		if old == newHeaderID || old == newFooterID || old == newChildID {
			t.Fatalf("块 id 必须重新分配，出现了沿用原 id 的情况: %s", old)
		}
	}
	var boundHeader string
	if err := json.Unmarshal(gotSettings["headerBlockId"], &boundHeader); err != nil || boundHeader != newHeaderID {
		t.Fatalf("headerBlockId 应指向导入后的新块 id: %s (err=%v)", boundHeader, err)
	}
	var boundFooter string
	if err := json.Unmarshal(gotSettings["footerBlockId"], &boundFooter); err != nil || boundFooter != newFooterID {
		t.Fatalf("footerBlockId 应指向导入后的新块 id: %s (err=%v)", boundFooter, err)
	}
	slots := map[string]string{}
	if err := json.Unmarshal(gotSettings["slots"], &slots); err != nil {
		t.Fatalf("解析 slots 失败: %v", err)
	}
	if slots["announcement"] != newChildID {
		t.Fatalf("槽位预设应指向导入后的新块 id: %v", slots)
	}

	// 块内容一致 + 引用已改写到新 id。
	newHeader, err := blocks.Detail(ctx, &blockdto.DetailReq{ProjectID: dst.ID, ID: newHeaderID})
	if err != nil {
		t.Fatalf("取导入块详情失败: %v", err)
	}
	refs := blockRefsOf(t, newHeader.Document)
	if len(refs) != 1 || refs[0] != newChildID {
		t.Fatalf("导入块内的引用应指向新块 id: %v（期望 %s）", refs, newChildID)
	}
	srcRefs := blockRefsOf(t, headerDoc)
	if len(srcRefs) != 1 || srcRefs[0] != child.ID {
		t.Fatalf("源块引用异常: %v", srcRefs)
	}
}

// TestThemeBundleRejectsIncompatiblePackages 版本号/格式/manifest/路径四类不兼容包一律拒绝。
func TestThemeBundleRejectsIncompatiblePackages(t *testing.T) {
	setBundleMediaRoot(t, t.TempDir())
	projects, _ := newBundleEnv(t)
	ctx := context.Background()
	dst, err := projects.Create(ctx, &projectdto.CreateReq{Name: "目标站点"})
	if err != nil {
		t.Fatalf("建工程失败: %v", err)
	}

	baseManifest := func(overrides map[string]any) map[string]any {
		m := map[string]any{
			"format":        projectservice.ThemeBundleFormat,
			"schemaVersion": projectservice.ThemeBundleSchemaVersion,
			"generator":     "test",
			"createdAt":     "2026-01-01T00:00:00Z",
			"source":        map[string]any{},
			"theme":         map[string]any{"name": "外来主题", "slots": map[string]any{}},
		}
		for k, v := range overrides {
			m[k] = v
		}
		return m
	}
	tokens := map[string]any{"colors": map[string]any{"primary": "#0a0b0c"}}

	cases := []struct {
		scenario string
		files    map[string]any
		wantErr  error
	}{
		{
			scenario: "包版本高于当前支持",
			files: map[string]any{
				"manifest.json": baseManifest(map[string]any{"schemaVersion": projectservice.ThemeBundleSchemaVersion + 98}),
				"tokens.json":   tokens,
			},
			wantErr: projectservice.ErrThemeBundleVersionTooNew,
		},
		{
			scenario: "版本号非正整数",
			files: map[string]any{
				"manifest.json": baseManifest(map[string]any{"schemaVersion": 0}),
				"tokens.json":   tokens,
			},
			wantErr: projectservice.ErrThemeBundleVersionInvalid,
		},
		{
			scenario: "不是主题包",
			files: map[string]any{
				"manifest.json": baseManifest(map[string]any{"format": "some-other-format"}),
				"tokens.json":   tokens,
			},
			wantErr: projectservice.ErrThemeBundleFormatUnknown,
		},
		{
			scenario: "缺少 manifest",
			files:    map[string]any{"tokens.json": tokens},
			wantErr:  projectservice.ErrThemeBundleMissingManifest,
		},
		{
			scenario: "路径穿越条目",
			files: map[string]any{
				"manifest.json":   baseManifest(nil),
				"tokens.json":     tokens,
				"../outside.json": map[string]any{"x": 1},
			},
			wantErr: projectservice.ErrThemeBundleUnsafeEntry,
		},
	}
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			pkg := buildBundleZip(t, tc.files)
			_, ierr := projects.ImportThemeBundle(ctx, &projectdto.ThemeBundleImportReq{ProjectID: dst.ID, Zip: pkg})
			if !errors.Is(ierr, tc.wantErr) {
				t.Fatalf("期望错误 %v，实际 %v", tc.wantErr, ierr)
			}
		})
	}

	// 反向护栏：同一条导入路径在合法包上必须成功（否则上面的拒绝可能来自别的原因）。
	ok := buildBundleZip(t, map[string]any{
		"manifest.json": baseManifest(nil),
		"tokens.json":   tokens,
	})
	res, err := projects.ImportThemeBundle(ctx, &projectdto.ThemeBundleImportReq{ProjectID: dst.ID, Zip: ok})
	if err != nil {
		t.Fatalf("合法包应导入成功: %v", err)
	}
	if res.ThemeName != "外来主题" {
		t.Fatalf("主题名不符: %s", res.ThemeName)
	}
}

// TestThemeBundleReportsMissingMedia 媒体引用不到时必须给出清单，而不是静默留死链。
func TestThemeBundleReportsMissingMedia(t *testing.T) {
	// 导出侧与导入侧都用空目录：既读不到字节（导出），目标也没有该文件（导入）。
	setBundleMediaRoot(t, t.TempDir())
	projects, blocks := newBundleEnv(t)
	ctx := context.Background()

	src, err := projects.Create(ctx, &projectdto.CreateReq{Name: "源站点"})
	if err != nil {
		t.Fatalf("建源工程失败: %v", err)
	}
	dst, err := projects.Create(ctx, &projectdto.CreateReq{Name: "目标站点"})
	if err != nil {
		t.Fatalf("建目标工程失败: %v", err)
	}
	doc := marshalDoc(t, []any{
		map[string]any{
			"id": "m1", "type": "core.image",
			"props": map[string]any{"src": bundleMissingMediaURL, "alt": "图"},
		},
	})
	header := createBlock(t, blocks, src.ID, "带图页眉", "header", doc)
	theme := createTheme(t, projects, src.ID, "缺媒体主题", map[string]any{
		"colors":        map[string]any{"primary": "#0a0b0c"},
		"headerBlockId": header.ID,
	})

	exported, err := projects.ExportThemeBundle(ctx, &projectdto.ThemeBundleExportReq{
		ThemeID: theme.ID, Media: projectdto.ThemeBundleMediaDeclare,
	})
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if exported.MediaEmbedded != 0 || exported.MediaDeclared != 1 {
		t.Fatalf("declare 策略下应只声明不内嵌: embedded=%d declared=%d", exported.MediaEmbedded, exported.MediaDeclared)
	}
	var manifest struct {
		Media []struct {
			URL          string   `json:"url"`
			Embedded     bool     `json:"embedded"`
			ReferencedBy []string `json:"referencedBy"`
		} `json:"media"`
	}
	if err := json.Unmarshal(zipEntry(t, exported.Bytes, "manifest.json"), &manifest); err != nil {
		t.Fatalf("解析包内 manifest 失败: %v", err)
	}
	if len(manifest.Media) != 1 || manifest.Media[0].URL != bundleMissingMediaURL || manifest.Media[0].Embedded {
		t.Fatalf("包内媒体清单不符: %+v", manifest.Media)
	}
	if len(manifest.Media[0].ReferencedBy) == 0 {
		t.Fatalf("媒体清单应记录引用来源: %+v", manifest.Media[0])
	}

	imported, err := projects.ImportThemeBundle(ctx, &projectdto.ThemeBundleImportReq{ProjectID: dst.ID, Zip: exported.Bytes})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(imported.Media.Resolved) != 0 {
		t.Fatalf("目标没有该文件，不该出现已解决条目: %+v", imported.Media.Resolved)
	}
	if len(imported.Media.Missing) != 1 {
		t.Fatalf("应给出 1 条缺失清单，实际 %+v", imported.Media.Missing)
	}
	missing := imported.Media.Missing[0]
	if missing.URL != bundleMissingMediaURL || !missing.WillDeadLink {
		t.Fatalf("缺失条目不符: %+v", missing)
	}
	if len(missing.ReferencedBy) == 0 {
		t.Fatalf("缺失条目必须指明引用来源（否则补件无从下手）: %+v", missing)
	}
}

// TestThemeBundleEmbedsMediaAndRestoresIt 内嵌媒体在干净环境导入后落盘可用。
func TestThemeBundleEmbedsMediaAndRestoresIt(t *testing.T) {
	srcRoot := t.TempDir()
	setBundleMediaRoot(t, srcRoot)
	projects, blocks := newBundleEnv(t)
	ctx := context.Background()

	const presentURL = "/storage/theme-bundle/present.jpg"
	target := filepath.Join(srcRoot, "theme-bundle", "present.jpg")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("建媒体目录失败: %v", err)
	}
	if err := os.WriteFile(target, []byte("fake-image-bytes"), 0o644); err != nil {
		t.Fatalf("写媒体文件失败: %v", err)
	}

	src, err := projects.Create(ctx, &projectdto.CreateReq{Name: "源站点"})
	if err != nil {
		t.Fatalf("建源工程失败: %v", err)
	}
	dst, err := projects.Create(ctx, &projectdto.CreateReq{Name: "目标站点"})
	if err != nil {
		t.Fatalf("建目标工程失败: %v", err)
	}
	doc := marshalDoc(t, []any{
		map[string]any{
			"id": "m1", "type": "core.image",
			"props": map[string]any{"src": presentURL, "alt": "图"},
		},
	})
	header := createBlock(t, blocks, src.ID, "带图页眉", "header", doc)
	theme := createTheme(t, projects, src.ID, "带媒体主题", map[string]any{
		"colors":        map[string]any{"primary": "#0a0b0c"},
		"headerBlockId": header.ID,
	})

	exported, err := projects.ExportThemeBundle(ctx, &projectdto.ThemeBundleExportReq{ThemeID: theme.ID})
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if exported.MediaEmbedded != 1 || exported.MediaDeclared != 0 {
		t.Fatalf("能读到的媒体应内嵌: embedded=%d declared=%d", exported.MediaEmbedded, exported.MediaDeclared)
	}
	if len(exported.Warnings) != 0 {
		t.Fatalf("不应有告警: %v", exported.Warnings)
	}

	// 干净环境：换一个空的媒体根目录再导入。
	dstRoot := t.TempDir()
	setBundleMediaRoot(t, dstRoot)
	if _, serr := os.Stat(filepath.Join(dstRoot, "theme-bundle", "present.jpg")); serr == nil {
		t.Fatalf("目标媒体根应为空")
	}

	imported, err := projects.ImportThemeBundle(ctx, &projectdto.ThemeBundleImportReq{ProjectID: dst.ID, Zip: exported.Bytes})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(imported.Media.Missing) != 0 {
		t.Fatalf("内嵌媒体不应进缺失清单: %+v", imported.Media.Missing)
	}
	if len(imported.Media.Resolved) != 1 || imported.Media.Resolved[0].URL != presentURL {
		t.Fatalf("内嵌媒体应落盘可用: %+v", imported.Media.Resolved)
	}
	restored := filepath.Join(dstRoot, "theme-bundle", "present.jpg")
	got, rerr := os.ReadFile(restored)
	if rerr != nil {
		t.Fatalf("导入后媒体文件应存在: %v", rerr)
	}
	if string(got) != "fake-image-bytes" {
		t.Fatalf("导入后媒体内容不符: %s", string(got))
	}
}

// fakeBundlePort 假端口：只记录写入请求，用于验证页面文档里的块引用被改写到新 id。
type fakeBundlePort struct {
	blockID string
	pageID  string
	blocks  []*projectcontract.ThemeBundleBlockCreate
	pages   []*projectcontract.ThemeBundlePageCreate
}

func (f *fakeBundlePort) GetBlock(context.Context, string, string) (*projectcontract.ThemeBundleBlock, error) {
	return nil, projectservice.ErrThemeBundleAssetMissing
}

func (f *fakeBundlePort) CreateBlock(_ context.Context, req *projectcontract.ThemeBundleBlockCreate) (string, error) {
	f.blocks = append(f.blocks, req)
	return f.blockID, nil
}

func (f *fakeBundlePort) ListPages(context.Context, string, string) ([]projectcontract.ThemeBundlePage, error) {
	return nil, nil
}

func (f *fakeBundlePort) PagePathTaken(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeBundlePort) CreatePage(_ context.Context, req *projectcontract.ThemeBundlePageCreate) (string, error) {
	f.pages = append(f.pages, req)
	return f.pageID, nil
}

// TestThemeBundleImportRemapsBlockRefsInPages 导入页面时文档里的块引用一并改写为新 id。
func TestThemeBundleImportRemapsBlockRefsInPages(t *testing.T) {
	setBundleMediaRoot(t, t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	ctx := context.Background()
	dst, err := projects.Create(ctx, &projectdto.CreateReq{Name: "目标站点"})
	if err != nil {
		t.Fatalf("建工程失败: %v", err)
	}

	const newBlockID = "11111111-1111-4111-8111-111111111111"
	const newPageID = "22222222-2222-4222-8222-222222222222"
	port := &fakeBundlePort{blockID: newBlockID, pageID: newPageID}
	projects.SetThemeBundleAssetPort(port)

	blockDoc := map[string]any{
		"key": "b1", "name": "页眉", "kind": "header", "reuseMode": "global",
		"document": map[string]any{
			"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
			"root": []any{
				map[string]any{
					"id": "n1", "type": "core.container",
					"props": map[string]any{"tag": "div", "layout": map[string]any{"engine": "flex", "flex": map[string]any{}}},
				},
			},
		},
	}
	pageDoc := map[string]any{
		"key": "p1", "kind": "page", "path": "/landing",
		"document": map[string]any{
			"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
			"root": []any{
				map[string]any{
					"id": "g1", "type": "core.globalref",
					"props": map[string]any{"blockId": "b1"},
				},
			},
		},
	}
	pkg := buildBundleZip(t, map[string]any{
		"manifest.json": map[string]any{
			"format":        projectservice.ThemeBundleFormat,
			"schemaVersion": projectservice.ThemeBundleSchemaVersion,
			"generator":     "test",
			"createdAt":     "2026-01-01T00:00:00Z",
			"source":        map[string]any{},
			"theme":         map[string]any{"name": "带页面主题", "slots": map[string]any{"header": "b1"}},
			"blocks":        []any{map[string]any{"key": "b1", "name": "页眉", "kind": "header", "reuseMode": "global"}},
			"pages":         []any{map[string]any{"key": "p1", "kind": "page", "path": "/landing"}},
		},
		"tokens.json":    map[string]any{"colors": map[string]any{"primary": "#0a0b0c"}},
		"blocks/b1.json": blockDoc,
		"pages/p1.json":  pageDoc,
	})

	res, err := projects.ImportThemeBundle(ctx, &projectdto.ThemeBundleImportReq{
		ProjectID: dst.ID, Zip: pkg, CreatePages: true,
	})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if len(res.Blocks) != 1 || res.Blocks[0].ID != newBlockID {
		t.Fatalf("块应取端口返回的新 id: %+v", res.Blocks)
	}
	if len(res.Pages) != 1 || res.Pages[0].ID != newPageID {
		t.Fatalf("页面应取端口返回的新 id: %+v", res.Pages)
	}
	if len(port.pages) != 1 {
		t.Fatalf("端口应收到 1 次建页请求: %d", len(port.pages))
	}
	refs := blockRefsOf(t, port.pages[0].Document)
	if len(refs) != 1 || refs[0] != newBlockID {
		t.Fatalf("页面文档里的块引用应改写成新 id: %v（期望 %s）", refs, newBlockID)
	}
	if port.pages[0].DraftPath != "/landing" {
		t.Fatalf("页面路径不符: %s", port.pages[0].DraftPath)
	}
	gotTheme, err := projects.GetTheme(ctx, res.ThemeID)
	if err != nil {
		t.Fatalf("回读主题失败: %v", err)
	}
	gotSettings := settingsOf(t, gotTheme.Settings)
	var boundHeader string
	if err := json.Unmarshal(gotSettings["headerBlockId"], &boundHeader); err != nil || boundHeader != newBlockID {
		t.Fatalf("槽位预设应落回新块 id: %s (err=%v)", boundHeader, err)
	}
}

// buildBundleZip 按文件名 → 内容（对象会序列化为 JSON）打包，用于构造测试包。
func buildBundleZip(t *testing.T, files map[string]any) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for name, payload := range files {
		var raw []byte
		switch v := payload.(type) {
		case []byte:
			raw = v
		case string:
			raw = []byte(v)
		default:
			encoded, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("序列化包内条目 %s 失败: %v", name, err)
			}
			raw = encoded
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("写入包内条目 %s 失败: %v", name, err)
		}
		if _, err := w.Write(raw); err != nil {
			t.Fatalf("写入包内条目 %s 失败: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

// TestThemeBundlePortOnlyRequiredWhenBlocksReferenced 资产端口只在真的要用时才要求：
// 纯令牌主题可以独立导出，引用了块的主题在端口缺失时明确拒绝（而不是导出一个缺页眉的包）。
func TestThemeBundlePortOnlyRequiredWhenBlocksReferenced(t *testing.T) {
	setBundleMediaRoot(t, t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	ctx := context.Background()
	src, err := projects.Create(ctx, &projectdto.CreateReq{Name: "源站点"})
	if err != nil {
		t.Fatalf("建工程失败: %v", err)
	}

	plain := createTheme(t, projects, src.ID, "纯令牌主题", map[string]any{
		"colors": map[string]any{"primary": "#0a0b0c"},
	})
	res, err := projects.ExportThemeBundle(ctx, &projectdto.ThemeBundleExportReq{ThemeID: plain.ID})
	if err != nil {
		t.Fatalf("纯令牌主题不该依赖资产端口: %v", err)
	}
	if res.BlockCount != 0 || res.PageCount != 0 {
		t.Fatalf("纯令牌主题不应包含块或页面: %+v", res)
	}

	withSlot := createTheme(t, projects, src.ID, "带槽位主题", map[string]any{
		"colors":        map[string]any{"primary": "#0a0b0c"},
		"headerBlockId": "33333333-3333-4333-8333-333333333333",
	})
	_, err = projects.ExportThemeBundle(ctx, &projectdto.ThemeBundleExportReq{ThemeID: withSlot.ID})
	if !errors.Is(err, projectservice.ErrThemeBundlePortUnavailable) {
		t.Fatalf("引用了块但没有资产端口时应明确拒绝，实际 %v", err)
	}
}
