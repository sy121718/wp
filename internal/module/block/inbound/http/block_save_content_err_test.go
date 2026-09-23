package blockhttp

// block_save_content_err_test.go — 工作台保存块内容（POST /admin/blocks/save-content）两条
// 失败出口的形状护栏。
//
// 为什么这两处非改不可：它是 **JSON 端点**，调用方是工作台的 saveDraft
// （internal/templates/static/js/workbench/methods/api.js），对响应做 `r.json()`，
// 再按 `j.code >= 400` 取 `j.message` 弹提示。改成 303 + ?err= 是错的（那里没有页面），
// 而原先的 `c.String(400, "参数不合法")` / `c.String(404, "全局块不存在")` 同样是错的：
// 纯文本让 `r.json()` 直接 reject，落进 `.catch()` —— 用户只看到保存状态变红、
// 没有一句可读的提示。
//
// 本文件钉三件事，都是「错了不会有人发现」的那一类：
//  1. 响应体是合法 JSON（纯文本会让调用方的解析 reject，症状是「静默失败」）；
//  2. 状态码如实反映失败类别（400 参数 / 404 不存在 / 500 内部故障）；
//  3. 文案是受控文案，内部错误原文（SQL、驱动、Go 结构体字段名）不上面。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"

	"github.com/gin-gonic/gin"
)

// stubBlockService 只实现本文件需要的两个方法，其余一律 panic。
//
// panic 而不是返回零值：本用例断言的是失败出口的形状，任何多余的服务调用都说明
// 「失败分支没在失败点停下来」——那种情况必须立刻炸，而不是静默走通。
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
	return nil, s.updateErr
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

// saveContentResp 调用 SaveBlockContent 并返回原始状态码、原始响应体与解析后的 JSON。
//
// body 不是合法 JSON 时不报错 —— 这正是「绑定失败」那条路径要覆盖的输入。
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

// TestSaveBlockContentBindFailIsControlledJSON `ShouldBindJSON` 失败 → 400 JSON + 受控文案。
func TestSaveBlockContentBindFailIsControlledJSON(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"非法 JSON", `{"id":`},
		{"缺必填 id", `{"name":"页脚"}`},
		{"字段类型不符", `{"id":{"nested":1}}`},
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
			// 受控文案的判据：gin 的绑定错误会带 Go 结构体与字段名，
			// 那属于实现细节，只能进日志。
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

// TestSaveBlockContentBlockMissingIsControlledJSON 块不存在 → 404 JSON + 模块词条文案。
func TestSaveBlockContentBlockMissingIsControlledJSON(t *testing.T) {
	svc := &stubBlockService{detailErr: blockcontract.ErrNotFound}
	status, _, parsed := saveContentResp(t, svc, `{"id":"b-1"}`)

	if status != http.StatusNotFound {
		t.Fatalf("块不存在应为 404，实际 %d", status)
	}
	if parsed.Code != http.StatusNotFound {
		t.Fatalf("响应体 code 应为 404，实际 %d", parsed.Code)
	}
	assertControlledMessage(t, parsed.Message, blockenums.ErrBlockNotFound, "全局块不存在")
}

// TestSaveBlockContentNilBlockWithoutErrorIsNotFound Detail 返回 (nil, nil) 的防御分支：
// 契约上不该出现，但不能静默继续 —— 那会把一次写操作建在空块上。
func TestSaveBlockContentNilBlockWithoutErrorIsNotFound(t *testing.T) {
	svc := &stubBlockService{}
	status, _, parsed := saveContentResp(t, svc, `{"id":"b-1"}`)

	if status != http.StatusNotFound {
		t.Fatalf("(nil, nil) 应落 404，实际 %d", status)
	}
	assertControlledMessage(t, parsed.Message, blockenums.ErrBlockNotFound, "全局块不存在")
	if svc.updateCall != 0 {
		t.Fatal("未定位到块时不应写库")
	}
}

// TestSaveBlockContentDetailInternalErrorHidesDetail 基础设施故障 → 500 + 归口文案，
// 原文只进日志。
//
// 「把 DB 故障说成块不存在」是原先那条 `err != nil || current == nil` 分支的副作用：
// 用户照着提示去查「块为什么不存在」，而真正的问题在连接池 / 表结构。
func TestSaveBlockContentDetailInternalErrorHidesDetail(t *testing.T) {
	const internal = `pq: relation "blocks" does not exist (SQLSTATE 42P01)`
	svc := &stubBlockService{detailErr: errors.New(internal)}
	status, raw, parsed := saveContentResp(t, svc, `{"id":"b-1"}`)

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

// TestSaveBlockContentSuccessStillJSON 成功路径不受本批改动影响（护栏）：
// 描述符（workbench target）给的键名照常工作，响应仍是 JSON 且带 code=200。
func TestSaveBlockContentSuccessStillJSON(t *testing.T) {
	svc := &stubBlockService{detail: &blockdto.BlockResp{ID: "b-1", Name: "页脚"}}
	status, _, parsed := saveContentResp(t, svc, `{"id":"b-1","document":{"root":[]}}`)

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

// assertControlledMessage 断言文案落在受控集合里：词条 key 或它的译文。
//
// 单测不连库，i18n 缓存未加载时 translate 原样返回 key；连库的 feature 用例拿到译文。
// 两者都是受控结果，硬断言其中一种会让用例随环境变红。
func assertControlledMessage(t *testing.T, got string, allowed ...string) {
	t.Helper()
	for _, want := range allowed {
		if got == want {
			return
		}
	}
	t.Fatalf("文案不在受控集合里：got %q，允许 %v（写侧改了 key 或读侧白名单失配都会落这里）", got, allowed)
}
