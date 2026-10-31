package unit

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	"go_wp/pkg/upload"

	"github.com/spf13/viper"
)

func initMediaCenterUpload(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := viper.New()
	cfg.Set("upload.base_url", "https://media.example.test")
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", dir)
	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() { _ = upload.Close() })
	return dir
}

func newUploadFileHeader(t *testing.T, filename, contentType, content string) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	fhs := req.MultipartForm.File["file"]
	if len(fhs) == 0 {
		t.Fatal("multipart 解析未得到文件")
	}
	return fhs[0]
}

func TestMediaCenterStableNaming(t *testing.T) {
	db, svc := newMediaUnitService(t)
	storeDir := initMediaCenterUpload(t)
	ctx := context.Background()

	resp, err := svc.Upload(ctx, newUploadFileHeader(t, "photo.png", "image/png", "content-A"), nil)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	wantKey := fmt.Sprintf("%d.png", resp.ID)
	if resp.URL != upload.BaseURL()+"/"+wantKey {
		t.Fatalf("URL 应按附件 ID 命名: got=%q want=%q", resp.URL, upload.BaseURL()+"/"+wantKey)
	}
	var filePath string
	if err := db.Raw("SELECT file_path FROM sys_attachment WHERE id = ?", resp.ID).Scan(&filePath).Error; err != nil {
		t.Fatalf("查询 file_path 失败: %v", err)
	}
	if filePath != wantKey {
		t.Fatalf("file_path 应按附件 ID 命名: got=%q want=%q", filePath, wantKey)
	}
	if _, err := os.Stat(filepath.Join(storeDir, wantKey)); err != nil {
		t.Fatalf("物理文件未按 ID 落盘: %v", err)
	}
	if resp.Generation != 1 {
		t.Fatalf("新上传 generation 应为 1，实际 %d", resp.Generation)
	}
	if resp.MD5 == "" {
		t.Fatalf("上传应记录 md5（去重键）")
	}
}

func TestMediaCenterUploadDedupReusesExisting(t *testing.T) {
	db, svc := newMediaUnitService(t)
	initMediaCenterUpload(t)
	ctx := context.Background()

	first, err := svc.Upload(ctx, newUploadFileHeader(t, "a.png", "image/png", "same-bytes"), nil)
	if err != nil {
		t.Fatalf("首次上传失败: %v", err)
	}
	if first.Duplicate {
		t.Fatalf("首次上传不应标记为复用")
	}

	second, err := svc.Upload(ctx, newUploadFileHeader(t, "renamed.png", "image/png", "same-bytes"), nil)
	if err != nil {
		t.Fatalf("重复上传失败: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("去重命中应复用已有记录: first=%d second=%d", first.ID, second.ID)
	}
	if !second.Duplicate {
		t.Fatalf("去重命中应标记 Duplicate=true")
	}
	if second.URL != first.URL {
		t.Fatalf("去重命中应返回原 URL: first=%q second=%q", first.URL, second.URL)
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_attachment WHERE md5 = ? AND status = 1", first.MD5).Scan(&count).Error; err != nil {
		t.Fatalf("统计同 md5 记录失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("同一内容应只存一份，实际 %d 条记录", count)
	}

	third, err := svc.Upload(ctx, newUploadFileHeader(t, "c.png", "image/png", "different-bytes"), nil)
	if err != nil {
		t.Fatalf("不同内容上传失败: %v", err)
	}
	if third.ID == first.ID || third.Duplicate {
		t.Fatalf("不同内容不应复用: %+v", third)
	}
}

func TestMediaCenterReplaceKeepsURLBumpsGeneration(t *testing.T) {
	_, svc := newMediaUnitService(t)
	storeDir := initMediaCenterUpload(t)
	ctx := context.Background()

	first, err := svc.Upload(ctx, newUploadFileHeader(t, "photo.png", "image/png", "v1-bytes"), nil)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	urlBefore := first.URL
	key := upload.StorageKey(urlBefore)

	replaced, err := svc.Replace(ctx, first.ID, newUploadFileHeader(t, "photo-v2.png", "image/png", "v2-bytes-longer"))
	if err != nil {
		t.Fatalf("换图失败: %v", err)
	}
	if replaced.URL != urlBefore {
		t.Fatalf("换图后 URL 必须不变: before=%q after=%q", urlBefore, replaced.URL)
	}
	if replaced.Generation != 2 {
		t.Fatalf("换图后 generation 应为 2，实际 %d", replaced.Generation)
	}
	if replaced.MD5 == first.MD5 {
		t.Fatalf("换图后 md5 应更新")
	}
	content, err := os.ReadFile(filepath.Join(storeDir, key))
	if err != nil {
		t.Fatalf("读取替换后文件失败: %v", err)
	}
	if string(content) != "v2-bytes-longer" {
		t.Fatalf("物理文件内容未替换: %q", string(content))
	}
	if replaced.FileSize != int64(len("v2-bytes-longer")) {
		t.Fatalf("换图后文件大小未更新: %d", replaced.FileSize)
	}

	third, err := svc.Replace(ctx, first.ID, newUploadFileHeader(t, "photo-v3.png", "image/png", "v3-bytes"))
	if err != nil {
		t.Fatalf("二次换图失败: %v", err)
	}
	if third.Generation != 3 {
		t.Fatalf("二次换图 generation 应为 3，实际 %d", third.Generation)
	}

	same, err := svc.Replace(ctx, first.ID, newUploadFileHeader(t, "photo-v3-again.png", "image/png", "v3-bytes"))
	if err != nil {
		t.Fatalf("同内容换图失败: %v", err)
	}
	if same.Generation != 3 {
		t.Fatalf("同内容换图不应递增 generation，实际 %d", same.Generation)
	}
	if same.URL != urlBefore {
		t.Fatalf("同内容换图后 URL 必须不变")
	}
}

func TestMediaCenterReplaceExtMismatchRejected(t *testing.T) {
	_, svc := newMediaUnitService(t)
	initMediaCenterUpload(t)
	ctx := context.Background()

	first, err := svc.Upload(ctx, newUploadFileHeader(t, "photo.png", "image/png", "png-bytes"), nil)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	_, err = svc.Replace(ctx, first.ID, newUploadFileHeader(t, "photo.jpg", "image/jpeg", "jpg-bytes"))
	if err == nil || !strings.Contains(err.Error(), mediaenums.ErrReplaceExtMismatch) {
		t.Fatalf("扩展名不一致应被拒绝: %v", err)
	}
}

func TestMediaCenterDeleteBlockedByReferences(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	id := seedAttachment(t, db, nil, "used.png", "image", "")
	if _, err := svc.SyncReferences(ctx, &mediacontract.SyncRefsInput{
		RefKind: "page", RefID: "p-1", RefTitle: "首页",
		URLs: []string{"/storage/seed/used.png"},
	}); err != nil {
		t.Fatalf("写入引用失败: %v", err)
	}

	refs, err := svc.References(ctx, id)
	if err != nil {
		t.Fatalf("查询引用失败: %v", err)
	}
	if len(refs) != 1 || refs[0].Kind != "page" || refs[0].ID != "p-1" {
		t.Fatalf("引用记录不符: %+v", refs)
	}

	err = svc.Delete(ctx, &mediadto.DeleteReq{ID: id})
	if err == nil {
		t.Fatalf("被引用的附件应拒绝删除")
	}
	if !strings.Contains(err.Error(), mediaenums.ErrAttachmentReferenced) ||
		!strings.Contains(err.Error(), "被 1 个页面引用") {
		t.Fatalf("删除拦截提示应含引用数量: %v", err)
	}

	if _, err := svc.SyncReferences(ctx, &mediacontract.SyncRefsInput{RefKind: "page", RefID: "p-1"}); err != nil {
		t.Fatalf("清空引用失败: %v", err)
	}
	refs, err = svc.References(ctx, id)
	if err != nil {
		t.Fatalf("二次查询引用失败: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("引用应已解除: %+v", refs)
	}
	if err := svc.Delete(ctx, &mediadto.DeleteReq{ID: id}); err != nil {
		t.Fatalf("无引用附件应可删除: %v", err)
	}
}

func TestMediaCenterSyncReferencesDiffAndJSONBQuery(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	idA := seedAttachment(t, db, nil, "a.png", "image", "")
	idB := seedAttachment(t, db, nil, "b.png", "image", "")
	idC := seedAttachment(t, db, nil, "c.png", "image", "")

	n, err := svc.SyncReferences(ctx, &mediacontract.SyncRefsInput{
		RefKind: "page", RefID: "p-9", RefTitle: "关于我们",
		URLs: []string{"/storage/seed/a.png", "/storage/seed/b.png", "/storage/not-exist.png"},
	})
	if err != nil {
		t.Fatalf("同步引用失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("应命中 2 个附件（不存在的 URL 跳过），实际 %d", n)
	}

	n, err = svc.SyncReferences(ctx, &mediacontract.SyncRefsInput{
		RefKind: "page", RefID: "p-9", RefTitle: "关于我们",
		URLs: []string{"/storage/seed/b.png", "/storage/seed/c.png"},
	})
	if err != nil {
		t.Fatalf("二次同步引用失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("应命中 2 个附件，实际 %d", n)
	}

	refsA, err := svc.References(ctx, idA)
	if err != nil {
		t.Fatalf("查询 A 引用失败: %v", err)
	}
	if len(refsA) != 0 {
		t.Fatalf("A 的引用应被移除: %+v", refsA)
	}
	refsB, _ := svc.References(ctx, idB)
	refsC, _ := svc.References(ctx, idC)
	if len(refsB) != 1 || len(refsC) != 1 {
		t.Fatalf("B/C 应各有一条引用: B=%+v C=%+v", refsB, refsC)
	}

	var hit int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_attachment WHERE extra_info @> '{"refs":[{"kind":"page","id":"p-9"}]}'::jsonb`).
		Scan(&hit).Error; err != nil {
		t.Fatalf("@> 查询失败: %v", err)
	}
	if hit != 2 {
		t.Fatalf("@> 查询应命中 2 行，实际 %d", hit)
	}

	title := "B 图"
	if err := svc.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{ID: idB, Alt: strPtr("B alt"), Title: &title}); err != nil {
		t.Fatalf("更新元数据失败: %v", err)
	}
	refsB, err = svc.References(ctx, idB)
	if err != nil {
		t.Fatalf("更新后查询引用失败: %v", err)
	}
	if len(refsB) != 1 {
		t.Fatalf("更新 alt/title 后 refs 应保留: %+v", refsB)
	}
	var alt, storedTitle string
	if err := db.Raw("SELECT extra_info ->> 'alt', extra_info ->> 'title' FROM sys_attachment WHERE id = ?", idB).
		Row().Scan(&alt, &storedTitle); err != nil {
		t.Fatalf("读取元数据失败: %v", err)
	}
	if alt != "B alt" || storedTitle != "B 图" {
		t.Fatalf("元数据未正确写入: alt=%q title=%q", alt, storedTitle)
	}
}

func TestMediaCenterSyncFromHTMLCollectsStorageURLs(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	idA := seedAttachment(t, db, nil, "hero.png", "image", "")
	if err := db.Exec(`INSERT INTO sys_media_variant (attachment_id, variant_type, file_path, status, file_size)
        VALUES (?, 'thumb', 'seed/hero_thumb.jpg', 'ready', 100)`, idA).Error; err != nil {
		t.Fatalf("插入变体记录失败: %v", err)
	}

	html := `<img src="/storage/seed/hero.png" srcset="/storage/seed/hero_thumb.jpg 320w, /storage/seed/hero.png 1280w">
        <a href="/other/page">无关链接</a><img src="/storage/seed/hero.png">`
	n, err := svc.SyncReferencesFromHTML(ctx, "page", "p-html", "/首页", html)
	if err != nil {
		t.Fatalf("构建期同步失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("原图与变体地址应归并到同一附件，实际命中 %d", n)
	}
	refs, err := svc.References(ctx, idA)
	if err != nil {
		t.Fatalf("查询引用失败: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "p-html" {
		t.Fatalf("引用未写入: %+v", refs)
	}
}

func TestMediaCenterAddRefToleratesNonObjectExtraInfo(t *testing.T) {
	db, svc := newMediaUnitService(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		raw   string
		refID string
	}{
		{"jsonb-null", "null", "p-null"},
		{"array", "[1,2,3]", "p-array"},
		{"scalar", "\"text\"", "p-scalar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := seedAttachment(t, db, nil, tc.name+".png", "image", tc.raw)
			if _, err := svc.SyncReferences(ctx, &mediacontract.SyncRefsInput{
				RefKind: "page", RefID: tc.refID, RefTitle: "首页",
				URLs: []string{"/storage/seed/" + tc.name + ".png"},
			}); err != nil {
				t.Fatalf("非对象 extra_info 写入 refs 失败: %v", err)
			}
			refs, err := svc.References(ctx, id)
			if err != nil {
				t.Fatalf("查询引用失败: %v", err)
			}
			if len(refs) != 1 || refs[0].ID != tc.refID {
				t.Fatalf("引用未正确写入: %+v", refs)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
