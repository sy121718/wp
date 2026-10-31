package blockhttp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	json "go_wp/pkg/jsonx"

	"github.com/gin-gonic/gin"
)

type presetCreateStub struct {
	blockcontract.BlockService
	request *blockdto.CreateReq
	err     error
}

func (s *presetCreateStub) Create(_ context.Context, req *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	s.request = req
	return &blockdto.BlockResp{ID: "created"}, s.err
}

func createPresetResponse(t *testing.T, mode string, fields map[string]any, svc *presetCreateStub) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	contentType := "application/json"
	switch mode {
	case "json":
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(raw)
	case "form":
		values := url.Values{}
		for key, value := range fields {
			values.Set(key, value.(string))
		}
		body.WriteString(values.Encode())
		contentType = "application/x-www-form-urlencoded"
	case "multipart":
		writer := multipart.NewWriter(&body)
		for key, value := range fields {
			if err := writer.WriteField(key, value.(string)); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		contentType = writer.FormDataContentType()
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/block/create", &body)
	c.Request.Header.Set("Content-Type", contentType)
	(&Handle{svc: svc}).Create(c)
	return w
}

func TestPresetCreateJSONAndFormDataShareProtocol(t *testing.T) {
	for _, mode := range []string{"json", "form", "multipart"} {
		t.Run(mode, func(t *testing.T) {
			version := any("2")
			if mode == "json" {
				version = 2
			}
			svc := &presetCreateStub{}
			w := createPresetResponse(t, mode, map[string]any{"projectId": "project", "name": "账号中心", "presetKey": "user.account", "presetVersion": version}, svc)
			if w.Code != http.StatusOK || svc.request == nil {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if svc.request.PresetKey != "user.account" || svc.request.PresetVersion != 2 || len(svc.request.Document) != 0 {
				t.Fatalf("绑定来源漂移：%+v", svc.request)
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || w.Header().Get("Location") != "" {
				t.Fatal("创建必须只回JSON")
			}
		})
	}
}

func TestPresetCreateRejectsMixedDocumentAndInvalidVersion(t *testing.T) {
	for _, mode := range []string{"json", "form", "multipart"} {
		for _, invalid := range []string{"document", "missing-version", "zero-version", "negative-version", "orphan-version"} {
			t.Run(mode+"/"+invalid, func(t *testing.T) {
				fields := map[string]any{"projectId": "project", "name": "账号中心", "presetKey": "user.account", "presetVersion": "1"}
				if mode == "json" {
					fields["presetVersion"] = 1
				}
				switch invalid {
				case "document":
					fields["document"] = ""
					if mode == "json" {
						fields["document"] = map[string]any{"root": []any{}}
					}
				case "missing-version":
					delete(fields, "presetVersion")
				case "zero-version":
					fields["presetVersion"] = "0"
					if mode == "json" {
						fields["presetVersion"] = 0
					}
				case "negative-version":
					fields["presetVersion"] = "-1"
					if mode == "json" {
						fields["presetVersion"] = -1
					}
				case "orphan-version":
					delete(fields, "presetKey")
				}
				svc := &presetCreateStub{}
				w := createPresetResponse(t, mode, fields, svc)
				if w.Code != http.StatusBadRequest || svc.request != nil {
					t.Fatalf("非法组合进入service：%d %s", w.Code, w.Body.String())
				}
				var payload struct {
					Code    int
					Message string
				}
				if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || payload.Code != w.Code || payload.Message == "" || strings.Contains(payload.Message, "CreateReq") {
					t.Fatalf("错误协议不受控：%s", w.Body.String())
				}
			})
		}
	}
}

func TestOrdinaryBlockFormKeepsDocumentAndPresetErrorStatus(t *testing.T) {
	doc := `{"root":[]}`
	for _, mode := range []string{"json", "form", "multipart"} {
		value := any(doc)
		if mode == "json" {
			value = json.RawMessage(doc)
		}
		svc := &presetCreateStub{}
		w := createPresetResponse(t, mode, map[string]any{"projectId": "project", "name": "普通区块", "document": value}, svc)
		if w.Code != http.StatusOK || svc.request == nil || string(svc.request.Document) != doc {
			t.Fatalf("普通文档丢失：%d %s", w.Code, w.Body.String())
		}
	}
	svc := &presetCreateStub{err: blockenums.Err(blockenums.ErrBlockPresetUnavailable)}
	w := createPresetResponse(t, "json", map[string]any{"projectId": "project", "name": "预设", "presetKey": "missing", "presetVersion": 1}, svc)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), blockenums.FallbackFor(blockenums.ErrBlockPresetUnavailable)) {
		t.Fatalf("预设缺失状态或文案错误：%d %s", w.Code, w.Body.String())
	}
}
