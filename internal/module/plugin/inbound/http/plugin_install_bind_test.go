package pluginhttp

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	plugindto "go_wp/internal/module/plugin/dto"
)

// multipartBody 组装一个带真实文件控件的 multipart 请求。
func multipartBody(t *testing.T, field, name string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf, w.FormDataContentType()
}

// 文件字段必须是 *multipart.FileHeader：声明成 string 时 Gin 直接返回
// ErrMultiFileHeader，安装入口永远拿不到文件。这里锁住真实绑定行为。
func TestPluginInstallBindsRealFileHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body, contentType := multipartBody(t, "file", "demo.zip", []byte("PK\x03\x04payload"))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/install", body)
	req.Header.Set("Content-Type", contentType)
	c.Request = req

	var form plugindto.InstallFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormMultipart); err != nil {
		t.Fatalf("真实文件控件必须能绑定：%v", err)
	}
	if form.File == nil {
		t.Fatal("绑定后应拿到文件头")
	}
	if form.File.Filename != "demo.zip" {
		t.Fatalf("文件名应保持原值，实际 %q", form.File.Filename)
	}
	f, err := form.File.Open()
	if err != nil {
		t.Fatalf("打开绑定的文件失败：%v", err)
	}
	defer func() { _ = f.Close() }()
	buf := &bytes.Buffer{}
	if _, err := buf.ReadFrom(f); err != nil || buf.String() != "PK\x03\x04payload" {
		t.Fatalf("文件内容应可读且一致：%q err=%v", buf.String(), err)
	}
}

// 没有文件控件时绑定应失败或文件头为空 —— 不能静默当成空 zip 继续安装。
func TestPluginInstallRejectsMissingFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	_ = w.WriteField("note", "no file here")
	_ = w.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/install", buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	c.Request = req

	var form plugindto.InstallFormReq
	err := c.ShouldBindWith(&form, ginbinding.FormMultipart)
	if err == nil && form.File != nil {
		t.Fatal("缺少文件控件时不应拿到文件头")
	}
}
