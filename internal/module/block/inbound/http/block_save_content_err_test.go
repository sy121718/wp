package blockhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/pkg/facing"

	"github.com/gin-gonic/gin"
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
	return blockenums.ErrBlockVersionConflict, blockenums.FallbackFor(blockenums.ErrBlockVersionConflict)
}
func (e *stubVersionConflict) Version() string { return e.version }
func (e *stubVersionConflict) Is(target error) bool {
	return target != nil && target.Error() == blockenums.ErrBlockVersionConflict
}

func saveContentResp(t *testing.T, svc blockcontract.BlockService, body string) (status int, raw string, parsed struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/admin/blocks/save-content", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	NewBlockPageHandle(svc, nil).SaveBlockContent(c)

	raw = w.Body.String()
	status = w.Code
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("响应不是合法 JSON —— 调用方 r.json() 会 reject，表现为「保存状态变红但没有提示」\n"+
			"  status=%d\n  body=%q\n  err=%v", status, raw, err)
	}
	return status, raw, parsed
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
			status, _, parsed := saveContentResp(t, svc, tc.body)

			if status != http.StatusBadRequest {
				t.Fatalf("绑定失败应为 400，实际 %d", status)
			}
			if parsed.Code != http.StatusBadRequest {
				t.Fatalf("响应体 code 应与状态码一致（调用方按 code 判定失败），实际 %d", parsed.Code)
			}
			if strings.TrimSpace(parsed.Message) == "" {
				t.Fatal("失败响应没有文案 —— 工作台会 alert 出一句空话")
			}

			for _, leak := range []string{"json:", "cannot unmarshal", "Go struct", "binding", "reflect"} {
				if strings.Contains(parsed.Message, leak) {
					t.Fatalf("绑定错误原文泄漏到响应里（%q 出现在 %q）", leak, parsed.Message)
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
	status, _, parsed := saveContentResp(t, svc, `{"id":"b-1","expectedVersion":1}`)

	if status != http.StatusNotFound {
		t.Fatalf("块不存在应为 404，实际 %d", status)
	}
	if parsed.Code != http.StatusNotFound {
		t.Fatalf("响应体 code 应为 404，实际 %d", parsed.Code)
	}
	assertControlledMessage(t, parsed.Message, blockenums.ErrBlockNotFound, "全局块不存在")
}

func TestSaveBlockContentNilBlockWithoutErrorIsNotFound(t *testing.T) {
	svc := &stubBlockService{}
	status, _, parsed := saveContentResp(t, svc, `{"id":"b-1","expectedVersion":1}`)

	if status != http.StatusNotFound {
		t.Fatalf("(nil, nil) 应落 404，实际 %d", status)
	}
	assertControlledMessage(t, parsed.Message, blockenums.ErrBlockNotFound, "全局块不存在")
	if svc.updateCall != 0 {
		t.Fatal("未定位到块时不应写库")
	}
}

func TestSaveBlockContentDetailInternalErrorHidesDetail(t *testing.T) {
	const internal = `pq: relation "blocks" does not exist (SQLSTATE 42P01)`
	svc := &stubBlockService{detailErr: errors.New(internal)}
	status, raw, parsed := saveContentResp(t, svc, `{"id":"b-1","expectedVersion":1}`)

	if status != http.StatusInternalServerError {
		t.Fatalf("内部故障应为 500（不能伪装成 404），实际 %d", status)
	}
	if parsed.Code != http.StatusInternalServerError {
		t.Fatalf("响应体 code 应为 500，实际 %d", parsed.Code)
	}
	assertControlledMessage(t, parsed.Message, blockenums.MsgInternalError, "系统内部错误，请稍后重试")
	if strings.Contains(raw, "pq:") || strings.Contains(raw, "SQLSTATE") || strings.Contains(raw, "relation") {
		t.Fatalf("内部错误原文出现在响应里：%q", raw)
	}
}

func TestSaveBlockContentSuccessStillJSON(t *testing.T) {
	svc := &stubBlockService{detail: &blockdto.BlockResp{ID: "b-1", Name: "页脚"}}
	status, _, parsed := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]},"expectedVersion":1}`)

	if status != http.StatusOK {
		t.Fatalf("成功路径应为 200，实际 %d", status)
	}
	if parsed.Code != http.StatusOK {
		t.Fatalf("响应体 code 应为 200，实际 %d", parsed.Code)
	}
	if svc.updateCall != 1 {
		t.Fatalf("成功路径应恰好调用一次 Update，实际 %d", svc.updateCall)
	}
}

func assertControlledMessage(t *testing.T, got string, allowed ...string) {
	t.Helper()
	for _, want := range allowed {
		if got == want {
			return
		}
	}
	t.Fatalf("文案不在受控集合里：got %q，允许 %v（写侧改了 key 或读侧白名单失配都会落这里）", got, allowed)
}

func TestSaveBlockContentVersionConflictIs409WithVersion(t *testing.T) {

	conflict := facing.WithStatus(&stubVersionConflict{version: "7"}, http.StatusConflict)

	svc := &stubBlockService{
		detail:    &blockdto.BlockResp{ID: "b-1", Name: "页脚", Version: 7},
		updateErr: conflict,
	}
	status, raw, parsed := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]},"expectedVersion":3}`)

	if status != http.StatusConflict {
		t.Fatalf("版本冲突应回 409，实际 %d：%s", status, raw)
	}
	if parsed.Code != http.StatusConflict {
		t.Errorf("JSON code 应与 HTTP 状态一致（409），实际 %d", parsed.Code)
	}

	if !strings.Contains(parsed.Message, "7") {
		t.Errorf("冲突文案应带当前版本号 7，实际 %q", parsed.Message)
	}

	for _, leak := range []string{"ErrBlockVersionConflict", "blockenums"} {
		if strings.Contains(parsed.Message, leak) {
			t.Errorf("文案里出现了内部标识 %q：%s", leak, parsed.Message)
		}
	}
}

func TestSaveBlockContentBusinessErrorPreservesDeclaredStatus(t *testing.T) {
	svc := &stubBlockService{
		detail:    &blockdto.BlockResp{ID: "b-1", Name: "页脚", Version: 7},
		updateErr: blockenums.Err(blockenums.ErrBlockInvalidDoc),
	}
	status, raw, parsed := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]},"expectedVersion":7}`)
	if status != http.StatusBadRequest || parsed.Code != http.StatusBadRequest {
		t.Fatalf("业务校验应400: status=%d body=%s", status, raw)
	}
	if parsed.Message != blockenums.FallbackFor(blockenums.ErrBlockInvalidDoc) {
		t.Fatalf("应输出声明文案且不含内部标识: %s", raw)
	}
}
