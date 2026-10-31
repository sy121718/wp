package feature

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	mediadto "go_wp/internal/module/media/dto"
	mediahttp "go_wp/internal/module/media/inbound/http"
	mediamodel "go_wp/internal/module/media/model"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/upload"
)

func TestMediaAbsoluteURLLifecycle(t *testing.T) {
	db, svc := newMediaService(t)
	dir := t.TempDir()
	cfg := viper.New()
	cfg.Set("upload.default_provider", "local")
	cfg.Set("upload.local_dir", dir)
	cfg.Set("upload.base_url", "https://media-a.example.test/storage")
	if err := upload.Init(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upload.Close() })
	imageBytes := makeStoragePNG(t, dir, "fixture.png", 1600, 900)
	handle := mediahttp.NewHandle(svc)
	router := gin.New()
	router.POST("/api/media/upload", handle.Upload)
	router.GET("/api/media/list", handle.List)
	router.GET("/api/media/detail", handle.Detail)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(imageBytes)
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/media/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	var uploaded struct {
		Code int                     `json:"code"`
		Data mediadto.AttachmentResp `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &uploaded); err != nil || recorder.Code != 200 || uploaded.Data.ID == 0 {
		t.Fatalf("上传失败: status=%d body=%s err=%v", recorder.Code, recorder.Body.String(), err)
	}
	key := upload.StorageKey(uploaded.Data.URL)
	if key == "" || uploaded.Data.URL != "https://media-a.example.test/storage/"+key {
		t.Fatalf("上传必须使用唯一完整前缀: %q", uploaded.Data.URL)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/media/list", nil))
	var listed struct {
		Data mediadto.ListResp `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil || len(listed.Data.List) != 1 || listed.Data.List[0].URL != uploaded.Data.URL {
		t.Fatalf("列表与上传不一致: %s / %s", recorder.Body.String(), uploaded.Data.URL)
	}

	oldURL := "/storage/" + key
	if err := db.Model(&mediamodel.AttachmentEntity{}).Where("id = ?", uploaded.Data.ID).Update("url", oldURL).Error; err != nil {
		t.Fatal(err)
	}
	_ = upload.Close()
	cfg.Set("upload.base_url", "https://media-b.example.test/shop")
	if err := upload.Init(cfg); err != nil {
		t.Fatal(err)
	}
	want := "https://media-b.example.test/shop/storage/" + key
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/media/detail?projectId=media-url-feature&id="+strconv.FormatUint(uploaded.Data.ID, 10), nil))
	var detail struct {
		Data mediadto.AttachmentResp `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil || detail.Data.URL != want {
		t.Fatalf("历史URL未跟随域名: got=%s want=%s body=%s", detail.Data.URL, want, recorder.Body.String())
	}
	ctx := context.Background()
	variants, err := svc.GenerateVariants(ctx, uploaded.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range variants {
		if variant.Status != "ready" {
			t.Fatalf("变体未生成: %+v", variant)
		}
		if !strings.HasPrefix(variant.URL, upload.BaseURL()+"/") {
			t.Fatalf("变体非完整地址: %+v", variant)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(upload.StorageKey(variant.URL)))); err != nil {
			t.Fatal(err)
		}
	}
	if got := svc.ProbeImageVariants(ctx, want+"?v=2#photo"); len(got) != 3 {
		t.Fatalf("完整URL探测未命中变体: %+v", got)
	}
	if got := svc.ProbeImageVariants(ctx, "https://external.example.test/storage/"+key); len(got) != 0 {
		t.Fatalf("外链误认本地资产: %+v", got)
	}
	html := `<img src="` + want + `" srcset="` + variants[0].URL + ` 320w"><img src="https://external.example.test/storage/` + key + `">`
	n, err := svc.SyncReferencesFromHTML(ctx, "page", "absolute-media-ref", "媒体测试", html)
	if err != nil || n != 1 {
		t.Fatalf("完整原图与变体引用未归并: n=%d err=%v", n, err)
	}
	refs, err := svc.References(ctx, uploaded.Data.ID)
	if err != nil || len(refs) != 1 || refs[0].ID != "absolute-media-ref" {
		t.Fatalf("引用未落库: %+v err=%v", refs, err)
	}
	n, err = svc.SyncReferencesFromHTML(ctx, "page", "external-only-ref", "外链测试", `<img src="https://external.example.test/storage/`+key+`">`)
	if err != nil || n != 0 {
		t.Fatalf("外链不得占用本地附件引用: n=%d err=%v", n, err)
	}
}
