package blockhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

type stubBlockService struct {
	detail     *blockdto.BlockResp
	detailErr  error
	updateErr  error
	updateCall int
	listCall   int
}

func (s *stubBlockService) List(context.Context, *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	s.listCall++
	return nil, nil
}

func (s *stubBlockService) Detail(context.Context, *blockdto.DetailReq) (*blockdto.BlockResp, error) {
	return s.detail, s.detailErr
}

func (s *stubBlockService) Create(context.Context, *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	panic("SaveBlockContent 不应调用 Create")
}

func (s *stubBlockService) Update(context.Context, *blockdto.UpdateReq) (*blockdto.BlockResp, error) {
	s.updateCall++
	return &blockdto.BlockResp{ID: "b-1", Version: 2}, s.updateErr
}

func (s *stubBlockService) Delete(context.Context, *blockdto.DeleteReq) error {
	panic("SaveBlockContent 不应调用 Delete")
}

func (s *stubBlockService) CloneAST(context.Context, *blockdto.CloneReq) (*blockdto.CloneResp, error) {
	panic("SaveBlockContent 不应调用 CloneAST")
}

func (s *stubBlockService) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	panic("SaveBlockContent 不应调用 ListBlockSourceRefs")
}

type stubVersionConflict struct{ version string }

func (e *stubVersionConflict) Error() string { return blockenums.ErrBlockVersionConflict }
func (e *stubVersionConflict) Facing() (string, string) {
	// 第二返回值是「声明处兜底」，取词收口后 enums 不再带成品句，这里给空串。
	return blockenums.ErrBlockVersionConflict + "|" + e.version, ""
}
func (e *stubVersionConflict) Version() string { return e.version }
func (e *stubVersionConflict) Is(target error) bool {
	return target != nil && target.Error() == blockenums.ErrBlockVersionConflict
}

// saveContentEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场。
func saveContentEngine(svc blockcontract.BlockService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	r.POST("/admin/blocks/save-content", pageview.HTMLFragment, NewBlockPageHandle(svc, nil).SaveBlockContent)
	return r
}

func saveContentResp(t *testing.T, svc blockcontract.BlockService, body string) (status int, raw string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	saveContentEngine(svc).ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("界面响应必须是 HTML，实际 %q，正文：%s", ct, rec.Body.String())
	}
	return rec.Code, rec.Body.String()
}

func TestSaveBlockContentBindFailIsControlledJSON(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"非法 JSON", `{"id":`},
		{"缺必填 id", `{"name":"页脚"}`},
		{"字段类型不符", `{"id":{"nested":1}}`},
		{"缺少读取版本", `{"id":"b-1"}`},
		{"零版本不能覆盖", `{"id":"b-1","expectedVersion":0}`},
		{"版本超出数据库范围", `{"id":"b-1","expectedVersion":9223372036854775808}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubBlockService{}
			status, body := saveContentResp(t, svc, tc.body)

			if status != http.StatusBadRequest {
				t.Fatalf("绑定失败应为 400，实际 %d", status)
			}
			if !strings.Contains(body, "data-ui-error") || !strings.Contains(body, "<p>") {
				t.Fatalf("失败应渲染带文案的错误片段，实际 %s", firstBlockN(body, 400))
			}

			for _, leak := range []string{"json:", "cannot unmarshal", "Go struct", "binding", "reflect"} {
				if strings.Contains(body, leak) {
					t.Fatalf("绑定错误原文泄漏到响应里（%q 出现在 %s）", leak, firstBlockN(body, 400))
				}
			}
			if svc.listCall != 0 || svc.updateCall != 0 {
				t.Fatal("参数都没绑定成功，不应继续调用 service")
			}
		})
	}
}

func TestSaveBlockContentBlockMissingIsControlledJSON(t *testing.T) {
	svc := &stubBlockService{detailErr: blockcontract.ErrNotFound}
	status, body := saveContentResp(t, svc, `{"id":"b-1","expectedVersion":1}`)

	if status != http.StatusNotFound {
		t.Fatalf("块不存在应为 404，实际 %d", status)
	}
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染错误片段，实际 %s", firstBlockN(body, 400))
	}
	if strings.Contains(body, blockenums.ErrBlockNotFound) {
		t.Fatalf("提示里出现裸 key：%s", firstBlockN(body, 400))
	}
}

func TestSaveBlockContentNilBlockWithoutErrorIsNotFound(t *testing.T) {
	svc := &stubBlockService{}
	status, body := saveContentResp(t, svc, `{"id":"b-1","expectedVersion":1}`)

	if status != http.StatusNotFound {
		t.Fatalf("(nil, nil) 应落 404，实际 %d", status)
	}
	if strings.Contains(body, blockenums.ErrBlockNotFound) {
		t.Fatalf("提示里出现裸 key：%s", firstBlockN(body, 400))
	}
	if svc.updateCall != 0 {
		t.Fatal("未定位到块时不应写库")
	}
}

func TestSaveBlockContentDetailInternalErrorHidesDetail(t *testing.T) {
	const internal = `pq: relation "blocks" does not exist (SQLSTATE 42P01)`
	svc := &stubBlockService{detailErr: errors.New(internal)}
	status, raw := saveContentResp(t, svc, `{"id":"b-1","expectedVersion":1}`)

	if status != http.StatusInternalServerError {
		t.Fatalf("内部故障应为 500（不能伪装成 404），实际 %d", status)
	}
	if !strings.Contains(raw, response.InternalFallback) {
		t.Fatalf("500 应给共享归口文案，实际 %s", firstBlockN(raw, 400))
	}
	if strings.Contains(raw, "pq:") || strings.Contains(raw, "SQLSTATE") || strings.Contains(raw, "relation") {
		t.Fatalf("内部错误原文出现在响应里：%q", raw)
	}
}

func TestSaveBlockContentSuccessStillJSON(t *testing.T) {
	svc := &stubBlockService{detail: &blockdto.BlockResp{ID: "b-1", Name: "页脚"}}
	status, body := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]},"expectedVersion":1}`)

	if status != http.StatusOK {
		t.Fatalf("成功路径应为 200，实际 %d", status)
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功应回 Jet 成功片段，实际 %s", firstBlockN(body, 400))
	}
	if svc.updateCall != 1 {
		t.Fatalf("成功路径应恰好调用一次 Update，实际 %d", svc.updateCall)
	}
}

func TestSaveBlockContentVersionConflictIs409WithVersion(t *testing.T) {

	conflict := facing.WithStatus(&stubVersionConflict{version: "7"}, http.StatusConflict)

	svc := &stubBlockService{
		detail:    &blockdto.BlockResp{ID: "b-1", Name: "页脚", Version: 7},
		updateErr: conflict,
	}
	i18n.InjectForTest(map[string]map[string]string{
		blockenums.ErrBlockVersionConflict: {"zh-CN": "该块已被修改（当前第 %s 版），请刷新后重试"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	status, raw := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]},"expectedVersion":3}`)

	if status != http.StatusConflict {
		t.Fatalf("版本冲突应回 409，实际 %d：%s", status, firstBlockN(raw, 400))
	}
	if !strings.Contains(raw, "当前第 7 版") {
		t.Errorf("冲突文案应带当前版本号 7，实际 %s", firstBlockN(raw, 400))
	}

	for _, leak := range []string{"ErrBlockVersionConflict", "blockenums"} {
		if strings.Contains(raw, leak) {
			t.Errorf("文案里出现了内部标识 %q：%s", leak, firstBlockN(raw, 400))
		}
	}
}

func TestSaveBlockContentBusinessErrorPreservesDeclaredStatus(t *testing.T) {
	svc := &stubBlockService{
		detail:    &blockdto.BlockResp{ID: "b-1", Name: "页脚", Version: 7},
		updateErr: blockenums.Err(blockenums.ErrBlockInvalidDoc),
	}
	status, raw := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]},"expectedVersion":7}`)
	if status != http.StatusBadRequest {
		t.Fatalf("业务校验应400: status=%d body=%s", status, firstBlockN(raw, 400))
	}
	// 本用例没有注入词库：失败片段退成该状态码档的通用文案，
	// 不泄漏裸 key、也不泄漏内部标识。
	if !strings.Contains(raw, "参数有误") && !strings.Contains(raw, response.InternalFallback) {
		t.Fatalf("缺词时应退成通用文案: %s", firstBlockN(raw, 400))
	}
	if strings.Contains(raw, blockenums.ErrBlockInvalidDoc) {
		t.Fatalf("提示里出现裸 key：%s", firstBlockN(raw, 400))
	}
}
