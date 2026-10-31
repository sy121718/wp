package unit

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"testing"

	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"

	"go_wp/public/test/support"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func seedPageRow(t *testing.T, db *gorm.DB, projectID, path string) string {
	t.Helper()
	id := uuid.NewString()
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pages (id, project_id, kind, content_target_type, draft_path,
			draft_document, draft_version, stale, create_time, update_time)
			VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, 1, false, NOW(), NOW())`,
			id, projectID, path).Error
	})
	if err != nil {
		t.Fatalf("写入页面失败: %v", err)
	}
	return id
}

func pageIsStale(t *testing.T, db *gorm.DB, pageID string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM pages WHERE id = ?", pageID).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 pages.stale 失败: %v", err)
	}
	return stale
}

func pngMultipartHeader(t *testing.T, field, filename string, w, h int) *multipart.FileHeader {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8((x * 7) % 256), G: uint8((y * 11) % 256), B: 64, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("编码测试 PNG 失败: %v", err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("构造 multipart 字段失败: %v", err)
	}
	if _, err := fw.Write(pngBuf.Bytes()); err != nil {
		t.Fatalf("写入 multipart 内容失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}
	form, err := multipart.NewReader(&body, mw.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("解析 multipart 失败: %v", err)
	}
	return form.File[field][0]
}

func TestReplaceMarksReferencingPageStale(t *testing.T) {
	storageDir := initVariantUploadForTest(t)
	db := support.NewMigratedPGTestDB(t)

	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "换图失效测试站点")
	referencingPage := seedPageRow(t, db, projectID, "/p/referencing")
	otherPage := seedPageRow(t, db, projectID, "/p/other")

	key := "replace_target.png"
	makeTestPNG(t, storageDir, key, 40, 30)

	am := mediamodel.NewAttachmentModel(db)
	mediaSvc := mediaservice.NewService(am, mediamodel.NewFileCategoryModel(db), mediamodel.NewMediaVariantModel(db))

	att := &mediamodel.AttachmentEntity{
		FileName:    "replace_target.png",
		FilePath:    key,
		FileSize:    100,
		FileType:    "image",
		StorageType: "local",
		Status:      mediamodel.AttachmentStatusEnabled,
	}
	if err := db.Create(att).Error; err != nil {
		t.Fatalf("种子附件插入失败: %v", err)
	}

	ctx := context.Background()

	if _, err := am.AddRef(ctx, att.ID, mediamodel.AttachmentRef{
		Kind: mediacontract.RefKindPage, ID: referencingPage, Title: "引用页",
	}); err != nil {
		t.Fatalf("写入媒体引用失败: %v", err)
	}

	pageSvc := pageservice.NewService(pagemodel.NewPageModel(db),
		nil, nil, projectservice.NewService(projectmodel.NewProjectModel(db)), nil, nil, nil, nil, nil)
	mediaSvc.SetStaleMarkers(pageSvc, stubStaleMarker{kind: mediacontract.RefKindPresentation})

	if pageIsStale(t, db, referencingPage) {
		t.Fatal("前提不成立：引用页在换图前就已是 stale")
	}

	if _, err := mediaSvc.Replace(ctx, att.ID, pngMultipartHeader(t, "file", "replace_target.png", 32, 24)); err != nil {
		t.Fatalf("换图失败: %v", err)
	}

	if !pageIsStale(t, db, referencingPage) {
		t.Fatalf("换图后引用该图的页面未被标记 stale（页面 id=%s）", referencingPage)
	}
	if pageIsStale(t, db, otherPage) {
		t.Fatalf("未引用该图的页面被误标 stale（页面 id=%s）：标记必须是精确集合", otherPage)
	}

	var paths []string
	if err := db.Raw("SELECT file_path FROM sys_media_variant WHERE attachment_id = ?", att.ID).
		Scan(&paths).Error; err != nil {
		t.Fatalf("读取变体记录失败: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("换图后没有任何变体记录")
	}
	t.Logf("换图后变体新名：%v", paths)
}

func TestSetStaleMarkersPanicsWhenKindMissing(t *testing.T) {
	svc := mediaservice.NewService(mediamodel.NewAttachmentModel(nil), nil, nil)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺一类引用方实现时应当 panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic 值应为字符串，实际 %T", r)
		}
		if !bytes.Contains([]byte(msg), []byte(mediacontract.RefKindPresentation)) {
			t.Fatalf("panic 文案里应点名缺失的引用方类型：%s", msg)
		}
		if !bytes.Contains([]byte(msg), []byte("换图后该类已发布页面永不更新")) {
			t.Fatalf("panic 文案里应写清未接入的后果：%s", msg)
		}
		t.Logf("装配期失败文案：%s", msg)
	}()
	svc.SetStaleMarkers(stubStaleMarker{kind: mediacontract.RefKindPage})
}

type stubStaleMarker struct{ kind string }

func (m stubStaleMarker) RefKinds() []string { return []string{m.kind} }

func (m stubStaleMarker) MarkStaleByMediaRefs(context.Context, []mediacontract.MediaRef) ([]string, error) {
	return nil, nil
}
