package contenttemplatehttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
)

type ctDeleteStub struct {
	contenttemplatecontract.ContentTemplateService
	calls []string
}

func (s *ctDeleteStub) Delete(_ context.Context, req *contenttemplatedto.DeleteReq) error {
	s.calls = append(s.calls, req.ID)
	if req.ID == "blocked" {
		return errors.New("ErrTemplateInUse")
	}
	if req.ID == "broken" {
		return errors.New(`pq: relation "content_templates" does not exist (SQLSTATE 42P01)`)
	}
	return nil
}

func callContentTemplateDelete(t *testing.T, s *ctDeleteStub, body string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/contenttemplate/delete", NewHandle(s).Delete)
	req := httptest.NewRequest(http.MethodPost, "/api/contenttemplate/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid response: %s: %v", rec.Body.String(), err)
	}
	return rec.Code, result
}

func TestContentTemplateDeleteJSON(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		code             int
		calls            []string
		deleted, skipped float64
	}{
		{"single", `{"id":"one"}`, 200, []string{"one"}, 1, 0},
		{"mixed and deduplicated", `{"ids":[" one ","blocked","one","broken","two"]}`, 200, []string{"one", "blocked", "broken", "two"}, 2, 2},
		{"empty", `{"ids":[" ",""]}`, 200, nil, 0, 0},
		{"invalid", `{"id":" "}`, 400, nil, 0, 0},
		{"too many", fmt.Sprintf(`{"ids":[%s]}`, func() string {
			values := make([]string, 201)
			for i := range values {
				values[i] = fmt.Sprintf(`"id-%d"`, i)
			}
			return strings.Join(values, ",")
		}()), 400, nil, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &ctDeleteStub{}
			code, body := callContentTemplateDelete(t, stub, tc.body)
			if code != tc.code {
				t.Fatalf("status=%d body=%v", code, body)
			}
			if fmt.Sprint(stub.calls) != fmt.Sprint(tc.calls) {
				t.Errorf("calls=%v want %v", stub.calls, tc.calls)
			}
			if code != 200 {
				return
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				t.Fatalf("data missing: %v", body)
			}
			if data["deleted"] != tc.deleted || data["skipped"] != tc.skipped {
				t.Fatalf("counts=%v", data)
			}
			items, ok := data["items"].([]any)
			if !ok || len(items) != len(tc.calls) {
				t.Fatalf("items=%v", data["items"])
			}
			if strings.Contains(fmt.Sprint(body), "SQLSTATE") || strings.Contains(fmt.Sprint(body), "content_templates") {
				t.Fatalf("internal error leaked: %v", body)
			}
			if tc.name == "mixed and deduplicated" {
				blocked := items[1].(map[string]any)
				broken := items[2].(map[string]any)
				if blocked["deleted"] != false || broken["deleted"] != false || blocked["message"] == "" || broken["message"] == "" {
					t.Fatalf("item errors missing: %v", items)
				}
			}
		})
	}
}
