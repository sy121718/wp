package feature

// media_download_test.go — 媒体资源包下载接口 feature 链路：
// 造附件 + 变体记录 + 本地物理文件 → GET /api/media/download(/batch) →
// 断言 200、zip 头（PK）、zip 内四目录结构与 README 占位。
// 存储目录经 upload.local_dir 指向 t.TempDir()；PG 不可用时 Skip。

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	mediahttp "go_wp/internal/module/media/inbound/http"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"
	"go_wp/pkg/upload"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// initDownloadEnv 初始化上传组件并把 local 存储根指向临时目录（测试自清理）。
func initDownloadEnv(t *testing.T) string {
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

// newDownloadRouter 复用 newMediaService（建隔离 PG + 三表 DDL），并按
// SetupMediaRoutes 同款装配（model→service→handle→路由）挂载下载接口。
// 仅豁免 SessionAuth 中间件：完整会话链路依赖 Redis 与管理员登录（已由
// admin/auth feature 测试覆盖），此处聚焦下载的 handler→service→model→zip 链路。
func newDownloadRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db, _ := newMediaService(t)
	am := mediamodel.NewAttachmentModel(db)
	cm := mediamodel.NewFileCategoryModel(db)
	vm := mediamodel.NewMediaVariantModel(db)
	svc := mediaservice.NewService(am, cm, vm)
	handle := mediahttp.NewHandle(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	g := router.Group("/api/media")
	{
		g.GET("/download", handle.Download)
		g.GET("/download/batch", handle.DownloadBatch)
		g.POST("/variants/generate", handle.GenerateVariants)
	}
	return router, db
}

// makeStoragePNG 在存储目录写一张真实 png（zip original/ 内容字节断言用）。
func makeStoragePNG(t *testing.T, dir string, key string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成测试 PNG 失败: %v", err)
	}
	_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(key))), 0o755)
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(key)), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("写入测试 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

// makeVariantFile 写一个假 webp 变体文件（RIFF 头字节，zip 只校验条目存在与可读）。
func makeVariantFile(t *testing.T, dir string, key string) {
	t.Helper()
	payload := append([]byte("RIFF"), bytes.Repeat([]byte{0}, 64)...)
	_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(key))), 0o755)
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(key)), payload, 0o644); err != nil {
		t.Fatalf("写入变体文件失败: %v", err)
	}
}

// seedDownloadAttachment 种子一张图片附件（物理文件需另行写入存储目录），返回 ID。
func seedDownloadAttachment(t *testing.T, db *gorm.DB, name string) uint64 {
	t.Helper()
	mime := "image/png"
	storage := "test/" + name
	e := &mediamodel.AttachmentEntity{
		FileName:    name,
		FilePath:    storage,
		FileSize:    1024,
		FileType:    "image",
		MimeType:    &mime,
		StorageType: "local",
		StoragePath: &storage,
		Status:      1,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("插入附件失败: %v", err)
	}
	return e.ID
}

// seedReadyVariant 种子一条 ready 变体记录（物理文件需另行写入）。
func seedReadyVariant(t *testing.T, db *gorm.DB, attachmentID uint64, variantType string, key string) {
	t.Helper()
	w, h := 100, 80
	size := int64(2048)
	mime := "image/webp"
	e := &mediamodel.MediaVariantEntity{
		AttachmentID: attachmentID,
		VariantType:  variantType,
		FilePath:     key,
		Width:        &w,
		Height:       &h,
		FileSize:     size,
		MimeType:     &mime,
		Status:       mediamodel.VariantStatusReady,
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("插入变体记录失败: %v", err)
	}
}

// readZipEntries 解析响应字节为 zip 条目名 → 内容映射。
func readZipEntries(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("响应不是合法 zip: %v", err)
	}
	out := make(map[string][]byte)
	for _, f := range zr.File {
		rc, oerr := f.Open()
		if oerr != nil {
			t.Fatalf("打开 zip 条目失败: %v", oerr)
		}
		data, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			t.Fatalf("读取 zip 条目失败: %v", rerr)
		}
		out[f.Name] = data
	}
	return out
}

// TestMediaDownloadSingleZip 单图下载：original/ 恒有；ready 变体打文件；
// 未 ready 的目录留 README.txt；Content-Disposition attachment。
func TestMediaDownloadSingleZip(t *testing.T) {
	tmp := initDownloadEnv(t)
	router, db := newDownloadRouter(t)

	origBytes := makeStoragePNG(t, tmp, "test/dl.png", 320, 240)
	id := seedDownloadAttachment(t, db, "dl.png")
	makeVariantFile(t, tmp, "test/dl_thumb.webp")
	seedReadyVariant(t, db, id, mediamodel.VariantTypeThumb, "test/dl_thumb.webp")
	// medium 记录 pending（无文件）、webp 无记录 → 两个目录都应出现 README.txt。
	var pendingRow = &mediamodel.MediaVariantEntity{
		AttachmentID: id,
		VariantType:  mediamodel.VariantTypeMedium,
		FilePath:     "test/dl_medium.webp",
		Status:       mediamodel.VariantStatusPending,
	}
	if err := db.Create(pendingRow).Error; err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/media/download?id="+uint64ToString(id), nil)
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("下载应 200: got=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type 应为 application/zip: got=%s", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("应为 attachment 下载: got=%s", cd)
	}
	body := w.Body.Bytes()
	if len(body) < 2 || body[0] != 'P' || body[1] != 'K' {
		t.Fatalf("响应应以 zip 头 PK 开头")
	}

	entries := readZipEntries(t, body)
	if got, ok := entries["original/dl.png"]; !ok || !bytes.Equal(got, origBytes) {
		t.Fatalf("original/dl.png 应存在且字节一致: ok=%t len=%d", ok, len(got))
	}
	if _, ok := entries["thumb/dl_thumb.webp"]; !ok {
		t.Fatalf("ready 的 thumb 变体应打包: %v", keysOf(entries))
	}
	if _, ok := entries["medium/README.txt"]; !ok {
		t.Fatalf("未 ready 的 medium 目录应有 README.txt: %v", keysOf(entries))
	}
	if _, ok := entries["webp/README.txt"]; !ok {
		t.Fatalf("无记录的 webp 目录应有 README.txt: %v", keysOf(entries))
	}
}

// TestMediaDownloadBatchZip 批量下载：每图一个 <stem>_<id>/ 子文件夹，内同四目录。
func TestMediaDownloadBatchZip(t *testing.T) {
	tmp := initDownloadEnv(t)
	router, db := newDownloadRouter(t)

	makeStoragePNG(t, tmp, "test/a1.png", 64, 64)
	makeStoragePNG(t, tmp, "test/b2.png", 64, 64)
	id1 := seedDownloadAttachment(t, db, "a1.png")
	id2 := seedDownloadAttachment(t, db, "b2.png")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/media/download/batch?ids="+uint64ToString(id1)+","+uint64ToString(id2), nil)
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("批量下载应 200: got=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if len(body) < 2 || body[0] != 'P' || body[1] != 'K' {
		t.Fatalf("响应应以 zip 头 PK 开头")
	}
	entries := readZipEntries(t, body)
	for _, want := range []string{
		"a1_" + uint64ToString(id1) + "/original/a1.png",
		"b2_" + uint64ToString(id2) + "/original/b2.png",
		"a1_" + uint64ToString(id1) + "/thumb/README.txt",
		"b2_" + uint64ToString(id2) + "/webp/README.txt",
	} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("批量包缺少条目 %s: %v", want, keysOf(entries))
		}
	}
}

// TestMediaDownloadErrors 不存在附件 400；ids 为空 400。
func TestMediaDownloadErrors(t *testing.T) {
	initDownloadEnv(t)
	router, _ := newDownloadRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/media/download?id=99999", nil)
	router.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("不存在的附件应 400: got=%d", w.Code)
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/media/download/batch?ids=", nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != 400 {
		t.Fatalf("空 ids 应 400: got=%d", w2.Code)
	}
}

// --- 小辅助 ---

func uint64ToString(v uint64) string {
	return strings.TrimSpace(strconv.FormatUint(v, 10))
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
