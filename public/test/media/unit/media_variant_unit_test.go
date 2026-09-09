package unit

// media_variant_unit_test.go — 图片变体生成 service 层单元测试：
// 测试图片全部由测试内置代码现场生成（image/png 编码小图写入临时存储目录），
// 不依赖真实上传链路；存储目录经 upload.local_dir 指向 t.TempDir()，测试自清理。

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

// initVariantUploadForTest 初始化上传组件并把 local 存储根指向临时目录
// （upload.LocalDir() 由此返回临时目录，变体产物全部落盘在 tmp 下自清理）。
func initVariantUploadForTest(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", tmp)
	if err := upload.Init(cfg); err != nil {
		t.Fatalf("初始化上传组件失败: %v", err)
	}
	t.Cleanup(func() { _ = upload.Close() })
	return tmp
}

// makeTestPNG 在存储目录生成 w×h 渐变小图（png 编码），key 为存储相对路径。
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

// makeOversizePNG 构造 header 声明 7000x10 的 PNG（改写 IHDR 并重算 CRC），
// 用于触发「边长 >6000px 跳过变体」的防御分支，无需真实分配大图内存。
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

// seedVariantAttachment 种子一张图片附件，返回附件 ID。
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

// listVariants 读取指定附件的全部变体记录（按 id 升序）。
func listVariants(t *testing.T, db *gorm.DB, attachmentID uint64) []mediamodel.MediaVariantEntity {
	t.Helper()
	var list []mediamodel.MediaVariantEntity
	if err := db.Where("attachment_id = ?", attachmentID).Order("id ASC").Find(&list).Error; err != nil {
		t.Fatalf("查询变体记录失败: %v", err)
	}
	return list
}

// variantMap 变体列表按类型建索引。
func variantMap(list []mediamodel.MediaVariantEntity) map[string]mediamodel.MediaVariantEntity {
	out := make(map[string]mediamodel.MediaVariantEntity, len(list))
	for _, v := range list {
		out[v.VariantType] = v
	}
	return out
}

// TestMediaVariantGenerateSuccess 生成成功：thumb/medium/webp 全部 ready，
// 落盘文件存在（RIFF 头），尺寸回填正确（thumb ≤320、medium ≤1280、webp=原图）。
func TestMediaVariantGenerateSuccess(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "src.png", 640, 480)
	id := seedVariantAttachment(t, db, "src.png", "src.png")

	variants, err := svc.GenerateVariants(t.Context(), id)
	if err != nil {
		t.Fatalf("生成变体失败: %v", err)
	}
	if len(variants) != 3 {
		t.Fatalf("应生成 3 个变体: got=%d", len(variants))
	}
	records := variantMap(listVariants(t, db, id))
	if len(records) != 3 {
		t.Fatalf("应有 3 条变体记录: got=%d", len(records))
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
		// 变体统一有损 JPEG（体积比无损 webp 小 3~4 倍，是页面加载速度的关键）。
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
	webp := records[mediamodel.VariantTypeWebp]
	if webp.Width == nil || *webp.Width != 640 || *webp.Height != 480 {
		t.Fatalf("webp 变体应保持原图尺寸 640x480: %dx%d", derefInt(webp.Width), derefInt(webp.Height))
	}
}

// TestMediaVariantDecodeFailureFailed 解码失败（伪 png 字节）：三条全部 failed，不返回 error（降级语义）。
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

// TestMediaVariantOversizeSkipped 边长 >6000px（伪造 header）：三条 failed，跳过生成。
func TestMediaVariantOversizeSkipped(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeOversizePNG(t, tmp, "huge.png")
	id := seedVariantAttachment(t, db, "huge.png", "huge.png")

	variants, err := svc.GenerateVariants(t.Context(), id)
	if err != nil {
		t.Fatalf("超大图应降级而非报错: %v", err)
	}
	if len(variants) != 3 {
		t.Fatalf("应返回 3 条 failed 记录: got=%d", len(variants))
	}
	for _, v := range variants {
		if v.Status != mediamodel.VariantStatusFailed {
			t.Fatalf("超大图 %s 应为 failed: got=%s", v.VariantType, v.Status)
		}
	}
	// 不应产生任何变体文件。
	if _, serr := os.Stat(filepath.Join(tmp, "huge_thumb.jpg")); !os.IsNotExist(serr) {
		t.Fatalf("超大图不应产出变体文件")
	}
}

// TestMediaVariantRegenerateIdempotent 重复生成幂等：先清旧记录，重建后仍为 3 条 ready。
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
	if len(records) != 3 {
		t.Fatalf("重新生成后应仍为 3 条（先清旧）: got=%d", len(records))
	}
	for _, rec := range records {
		if rec.Status != mediamodel.VariantStatusReady {
			t.Fatalf("重新生成后 %s 应为 ready: got=%s", rec.VariantType, rec.Status)
		}
	}
}

// TestMediaEnsureVariantRecords 上传切入点登记：正常图片 → 3 条 pending；
// svg/gif → 不登记；探测失败 → 3 条 failed。
func TestMediaEnsureVariantRecords(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "up.png", 800, 600)

	// 正常图片：3 条 pending。
	idPNG := seedVariantAttachmentNamed(t, db, "up.png", "up.png", "image", "image/png")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idPNG))
	if got := len(listVariants(t, db, idPNG)); got != 3 {
		t.Fatalf("正常图片应登记 3 条变体记录: got=%d", got)
	}
	for _, rec := range listVariants(t, db, idPNG) {
		if rec.Status != mediamodel.VariantStatusPending {
			t.Fatalf("探测通过应为 pending: %s=%s", rec.VariantType, rec.Status)
		}
	}

	// svg：不登记。
	idSVG := seedVariantAttachmentNamed(t, db, "icon.svg", "icon.svg", "image", "image/svg+xml")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idSVG))
	if got := len(listVariants(t, db, idSVG)); got != 0 {
		t.Fatalf("svg 不应登记变体记录: got=%d", got)
	}

	// gif：不登记。
	idGIF := seedVariantAttachmentNamed(t, db, "anim.gif", "anim.gif", "image", "image/gif")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idGIF))
	if got := len(listVariants(t, db, idGIF)); got != 0 {
		t.Fatalf("gif 不应登记变体记录: got=%d", got)
	}

	// 探测失败（坏字节）：3 条 failed。
	if err := os.WriteFile(filepath.Join(tmp, "broken.png"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	idBad := seedVariantAttachmentNamed(t, db, "broken.png", "broken.png", "image", "image/png")
	svc.EnsureVariantRecords(t.Context(), mustAttachment(t, db, idBad))
	bad := listVariants(t, db, idBad)
	if len(bad) != 3 {
		t.Fatalf("探测失败应登记 3 条: got=%d", len(bad))
	}
	for _, rec := range bad {
		if rec.Status != mediamodel.VariantStatusFailed {
			t.Fatalf("探测失败应为 failed: %s=%s", rec.VariantType, rec.Status)
		}
	}
}

// TestMediaVariantNonImageRejected 非图片附件与缺失附件拒绝。
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

// --- 小辅助 ---

func derefInt(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

// seedVariantAttachmentNamed 带类型/MIME 的附件种子（登记用例区分 svg/gif）。
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

// mustAttachment 读取附件实体。
func mustAttachment(t *testing.T, db *gorm.DB, id uint64) *mediamodel.AttachmentEntity {
	t.Helper()
	var e mediamodel.AttachmentEntity
	if err := db.First(&e, id).Error; err != nil {
		t.Fatalf("读取附件失败: %v", err)
	}
	return &e
}
