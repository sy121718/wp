package unit

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/upload"

	"github.com/spf13/viper"
	"gorm.io/gorm"
)

func initVariantUploadForTest(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfg := viper.New()
	cfg.Set("upload.base_url", "https://media.example.test")
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", tmp)
	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() { _ = upload.Close() })
	return tmp
}

func makeTestPNG(t *testing.T, dir string, key string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成测试 PNG 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(key)), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("写入测试 PNG 失败: %v", err)
	}
}

func makeOversizePNG(t *testing.T, dir string, key string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成底图失败: %v", err)
	}
	raw := buf.Bytes()
	binary.BigEndian.PutUint32(raw[16:20], 7000)
	binary.BigEndian.PutUint32(raw[20:24], 10)
	binary.BigEndian.PutUint32(raw[30:34], crc32.ChecksumIEEE(raw[12:29]))
	if err := os.WriteFile(filepath.Join(dir, key), raw, 0o644); err != nil {
		t.Fatalf("写入超大头 PNG 失败: %v", err)
	}
}

func seedVariantAttachment(t *testing.T, db *gorm.DB, fileName string, storageKey string) uint64 {
	t.Helper()
	mime := "image/png"
	e := &mediamodel.AttachmentEntity{
		FileName:    fileName,
		FilePath:    storageKey,
		FileSize:    1024,
		FileType:    "image",
		MimeType:    &mime,
		StorageType: "local",
		StoragePath: &storageKey,
		Status:      mediamodel.AttachmentStatusEnabled,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("种子附件失败: %v", err)
	}
	return e.ID
}

func listVariants(t *testing.T, db *gorm.DB, attachmentID uint64) []mediamodel.MediaVariantEntity {
	t.Helper()
	var list []mediamodel.MediaVariantEntity
	if err := db.Where("attachment_id = ?", attachmentID).Order("id ASC").Find(&list).Error; err != nil {
		t.Fatalf("查询变体记录失败: %v", err)
	}
	return list
}

func variantMap(list []mediamodel.MediaVariantEntity) map[string]mediamodel.MediaVariantEntity {
	out := make(map[string]mediamodel.MediaVariantEntity, len(list))
	for _, v := range list {
		out[v.VariantType] = v
	}
	return out
}

func TestMediaVariantGenerateSuccess(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "src.png", 640, 480)
	id := seedVariantAttachment(t, db, "src.png", "src.png")

	variants, err := svc.GenerateVariants(t.Context(), id)
	if err != nil {
		t.Fatalf("生成变体失败: %v", err)
	}

	want := len(mediamodel.VariantTypes())
	if len(variants) != want {
		t.Fatalf("应生成 %d 个变体: got=%d", want, len(variants))
	}
	records := variantMap(listVariants(t, db, id))
	if len(records) != want {
		t.Fatalf("应有 %d 条变体记录: got=%d", want, len(records))
	}
	for _, vt := range mediamodel.VariantTypes() {
		rec, ok := records[vt]
		if !ok {
			t.Fatalf("缺少 %s 记录: %+v", vt, records)
		}
		if rec.Status != mediamodel.VariantStatusReady {
			t.Fatalf("%s 应为 ready: got=%s", vt, rec.Status)
		}
		abs := filepath.Join(tmp, filepath.FromSlash(rec.FilePath))
		data, rerr := os.ReadFile(abs)
		if rerr != nil {
			t.Fatalf("%s 文件应存在: %v", vt, rerr)
		}

		if len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 {
			t.Fatalf("%s 应为 JPEG 文件（SOI 头 FFD8）", vt)
		}
		if rec.MimeType == nil || *rec.MimeType != "image/jpeg" {
			t.Fatalf("%s MIME 应为 image/jpeg", vt)
		}
	}

	thumb := records[mediamodel.VariantTypeThumb]
	if thumb.Width == nil || *thumb.Width <= 0 || *thumb.Width > 320 || *thumb.Height > 320 {
		t.Fatalf("thumb 尺寸应 Fit 320x320: %dx%d", derefInt(thumb.Width), derefInt(thumb.Height))
	}
	medium := records[mediamodel.VariantTypeMedium]
	if medium.Width == nil || *medium.Width <= 0 || *medium.Width > 1280 || *medium.Height > 1280 {
		t.Fatalf("medium 尺寸应 Fit 1280x1280: %dx%d", derefInt(medium.Width), derefInt(medium.Height))
	}

	full := records[mediamodel.VariantTypeFull]
	if full.Width == nil || *full.Width != 640 || *full.Height != 480 {
		t.Fatalf("full 变体应保持原图尺寸 640x480: %dx%d", derefInt(full.Width), derefInt(full.Height))
	}
}

func TestMediaVariantDecodeFailureFailed(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	if err := os.WriteFile(filepath.Join(tmp, "bad.png"), []byte("this is not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := seedVariantAttachment(t, db, "bad.png", "bad.png")

	variants, err := svc.GenerateVariants(t.Context(), id)
	if err != nil {
		t.Fatalf("解码失败应降级而非报错: %v", err)
	}
	for _, v := range variants {
		if v.Status != mediamodel.VariantStatusFailed {
			t.Fatalf("解码失败时 %s 应为 failed: got=%s", v.VariantType, v.Status)
		}
	}
}

func TestMediaVariantOversizeSkipped(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeOversizePNG(t, tmp, "huge.png")
	id := seedVariantAttachment(t, db, "huge.png", "huge.png")

	variants, err := svc.GenerateVariants(t.Context(), id)
	if err != nil {
		t.Fatalf("超大图应降级而非报错: %v", err)
	}
	if want := len(mediamodel.VariantTypes()); len(variants) != want {
		t.Fatalf("应返回 %d 条 failed 记录: got=%d", want, len(variants))
	}
	for _, v := range variants {
		if v.Status != mediamodel.VariantStatusFailed {
			t.Fatalf("超大图 %s 应为 failed: got=%s", v.VariantType, v.Status)
		}
	}

	if _, serr := os.Stat(filepath.Join(tmp, "huge_thumb.jpg")); !os.IsNotExist(serr) {
		t.Fatalf("超大图不应产出变体文件")
	}
}

func TestMediaVariantRegenerateIdempotent(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "re.png", 200, 100)
	id := seedVariantAttachment(t, db, "re.png", "re.png")

	if _, err := svc.GenerateVariants(t.Context(), id); err != nil {
		t.Fatalf("第一次生成失败: %v", err)
	}
	if _, err := svc.GenerateVariants(t.Context(), id); err != nil {
		t.Fatalf("重新生成失败: %v", err)
	}
	records := listVariants(t, db, id)
	if want := len(mediamodel.VariantTypes()); len(records) != want {
		t.Fatalf("重新生成后应仍为 %d 条（先清旧）: got=%d", want, len(records))
	}
	for _, rec := range records {
		if rec.Status != mediamodel.VariantStatusReady {
			t.Fatalf("重新生成后 %s 应为 ready: got=%s", rec.VariantType, rec.Status)
		}
	}
}

func TestMediaEnsureVariantRecords(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "up.png", 800, 600)

	idPNG := seedVariantAttachmentNamed(t, db, "up.png", "up.png", "image", "image/png")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idPNG))
	if got, want := len(listVariants(t, db, idPNG)), len(mediamodel.VariantTypes()); got != want {
		t.Fatalf("正常图片应登记 %d 条变体记录: got=%d", want, got)
	}
	for _, rec := range listVariants(t, db, idPNG) {
		if rec.Status != mediamodel.VariantStatusPending {
			t.Fatalf("探测通过应为 pending: %s=%s", rec.VariantType, rec.Status)
		}
	}

	idSVG := seedVariantAttachmentNamed(t, db, "icon.svg", "icon.svg", "image", "image/svg+xml")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idSVG))
	if got := len(listVariants(t, db, idSVG)); got != 0 {
		t.Fatalf("svg 不应登记变体记录: got=%d", got)
	}

	idGIF := seedVariantAttachmentNamed(t, db, "anim.gif", "anim.gif", "image", "image/gif")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idGIF))
	if got := len(listVariants(t, db, idGIF)); got != 0 {
		t.Fatalf("gif 不应登记变体记录: got=%d", got)
	}

	if err := os.WriteFile(filepath.Join(tmp, "broken.png"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	idBad := seedVariantAttachmentNamed(t, db, "broken.png", "broken.png", "image", "image/png")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idBad))
	bad := listVariants(t, db, idBad)
	if want := len(mediamodel.VariantTypes()); len(bad) != want {
		t.Fatalf("探测失败应登记 %d 条: got=%d", want, len(bad))
	}
	for _, rec := range bad {
		if rec.Status != mediamodel.VariantStatusFailed {
			t.Fatalf("探测失败应为 failed: %s=%s", rec.VariantType, rec.Status)
		}
	}
}

func TestMediaVariantNonImageRejected(t *testing.T) {
	db, svc := newMediaUnitService(t)
	initVariantUploadForTest(t)

	if _, err := svc.GenerateVariants(t.Context(), 99999); err == nil {
		t.Fatalf("不存在的附件应报错")
	}

	pdfKey := "doc.pdf"
	e := &mediamodel.AttachmentEntity{
		FileName: "doc.pdf", FilePath: pdfKey, FileType: "document", StorageType: "local", StoragePath: &pdfKey,
		Status: mediamodel.AttachmentStatusEnabled,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GenerateVariants(t.Context(), e.ID); err == nil {
		t.Fatalf("非图片附件应拒绝生成变体")
	}
}

func derefInt(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func seedVariantAttachmentNamed(t *testing.T, db *gorm.DB, fileName string, storageKey string, fileType string, mime string) uint64 {
	t.Helper()
	m := mime
	e := &mediamodel.AttachmentEntity{
		FileName: fileName, FilePath: storageKey, FileSize: 128, FileType: fileType, MimeType: &m,
		StorageType: "local", StoragePath: &storageKey,
		Status: mediamodel.AttachmentStatusEnabled,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("种子附件失败: %v", err)
	}
	return e.ID
}

func mustAttachment(t *testing.T, db *gorm.DB, id uint64) *mediamodel.AttachmentEntity {
	t.Helper()
	var e mediamodel.AttachmentEntity
	if err := db.First(&e, id).Error; err != nil {
		t.Fatalf("读取附件失败: %v", err)
	}
	return &e
}
