package mediahttp

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
	"go_wp/pkg/facing"
)

type uploadBindingMedia struct {
	mediacontract.MediaService
	called   bool
	filename string
	contents string
	category *uint64
	err      error
}

func (s *uploadBindingMedia) Upload(_ context.Context, file *multipart.FileHeader, category *uint64) (*mediadto.AttachmentResp, error) {
	s.called, s.filename, s.category = true, file.Filename, category
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	s.contents = string(body)
	return &mediadto.AttachmentResp{}, s.err
}

// TestMediaUploadMultipartBinding 只核上传绑定与错误透传，不替代存储持久化验收。
func TestMediaUploadMultipartBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, category string
		err            error
		status         int
	}{
		{"bodyCategory", "7", nil, http.StatusOK},
		{"queryCannotFillBody", "", nil, http.StatusOK},
		{"zeroCategory", "0", nil, http.StatusOK},
		{"serviceLimit", "7", facing.WithStatus(facing.NewMessage("uploadLimit", "上传超过限制"), http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			file, err := writer.CreateFormFile("file", "sample.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(file, "真实上传内容"); err != nil {
				t.Fatal(err)
			}
			if tc.category != "" {
				if err := writer.WriteField("category_id", tc.category); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			svc := &uploadBindingMedia{err: tc.err}
			router := gin.New()
			router.POST("/upload", NewHandle(svc).Upload)
			req := httptest.NewRequest(http.MethodPost, "/upload?category_id=99", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status || !svc.called || svc.filename != "sample.txt" || svc.contents != "真实上传内容" {
				t.Fatalf("上传绑定或状态失真：status=%d service=%+v body=%s", rec.Code, svc, rec.Body.String())
			}
			if tc.category == "7" {
				if svc.category == nil || *svc.category != 7 {
					t.Fatalf("分类必须来自 multipart body：%v", svc.category)
				}
			} else if svc.category != nil {
				t.Fatalf("空分类和 0 不得被 query 补齐：%v", *svc.category)
			}
		})
	}
	for _, tc := range []struct{ name, contentType, body string }{
		{"json", "application/json", `{"file":"sample","category_id":"7"}`},
		{"form", "application/x-www-form-urlencoded", "file=sample&category_id=7"},
		{"missingFile", "multipart/form-data; boundary=test", "--test--\r\n"},
		{"malformed", "multipart/form-data; boundary=test", "--test\r\nbroken"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &uploadBindingMedia{}
			router := gin.New()
			router.POST("/upload", NewHandle(svc).Upload)
			req := httptest.NewRequest(http.MethodPost, "/upload?file=sample&category_id=99", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || svc.called {
				t.Fatalf("非法上传必须在绑定层拒绝：status=%d called=%v body=%s", rec.Code, svc.called, rec.Body.String())
			}
		})
	}
}
