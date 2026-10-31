package blockhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

type bulkDeleteStubService struct {
	fail  map[string]error
	calls []string
}

func (s *bulkDeleteStubService) Delete(_ context.Context, req *blockdto.DeleteReq) error {
	s.calls = append(s.calls, req.ID)
	if err, ok := s.fail[req.ID]; ok {
		return err
	}
	return nil
}

func (s *bulkDeleteStubService) List(context.Context, *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 List")
}

func (s *bulkDeleteStubService) Detail(context.Context, *blockdto.DetailReq) (*blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 Detail")
}

func (s *bulkDeleteStubService) Create(context.Context, *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 Create")
}

func (s *bulkDeleteStubService) Update(context.Context, *blockdto.UpdateReq) (*blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 Update")
}

func (s *bulkDeleteStubService) CloneAST(context.Context, *blockdto.CloneReq) (*blockdto.CloneResp, error) {
	panic("BulkDelete 不应调用 CloneAST")
}

func (s *bulkDeleteStubService) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	panic("BulkDelete 不应调用 ListBlockSourceRefs")
}

func bulkDeleteResp(t *testing.T, svc blockcontract.BlockService, body string) (int, string, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/block/bulk-delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	(&Handle{svc: svc}).BulkDelete(c)

	raw := w.Body.String()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("响应不是合法 JSON —— 前端 r.json() 会 reject，表现为「点了没反应」\n"+
			"  status=%d\n  body=%q\n  err=%v", w.Code, raw, err)
	}
	return w.Code, raw, parsed
}

func dataOf(t *testing.T, parsed map[string]any) map[string]any {
	t.Helper()
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data 对象（前端按 data.deleted / data.items 渲染结果），实际 %v", parsed)
	}
	return data
}

func itemsOf(t *testing.T, parsed map[string]any) []map[string]any {
	t.Helper()
	raw, ok := dataOf(t, parsed)["items"].([]any)
	if !ok {
		t.Fatalf("响应缺少 data.items 列表，实际 %v", parsed)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		item, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("data.items 的元素不是对象：%v", v)
		}
		out = append(out, item)
	}
	return out
}

func TestBulkDeleteBindFailIsControlledJSON(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"非法 JSON", `{"ids":`},
		{"缺必填 ids", `{}`},
		{"ids 为空数组", `{"ids":[]}`},
		{"元素为空串", `{"ids":[""]}`},
		{"元素类型不符", `{"ids":[{"nested":1}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &bulkDeleteStubService{}
			status, _, parsed := bulkDeleteResp(t, svc, tc.body)

			if status != http.StatusBadRequest {
				t.Fatalf("绑定失败应为 400，实际 %d", status)
			}
			if code, _ := parsed["code"].(float64); int(code) != http.StatusBadRequest {
				t.Fatalf("响应体 code 应与状态码一致（前端按 code 判失败），实际 %v", parsed["code"])
			}
			msg, _ := parsed["message"].(string)
			if strings.TrimSpace(msg) == "" {
				t.Fatal("失败响应没有文案 —— 前端只能弹一句空话")
			}
			for _, leak := range []string{"json:", "cannot unmarshal", "Go struct", "binding", "reflect", "dto.BulkDeleteReq"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("绑定错误原文泄漏到响应里（%q 出现在 %q）", leak, msg)
				}
			}
			if len(svc.calls) != 0 {
				t.Fatalf("参数都没绑定成功就不该删任何东西，实际调用 %v", svc.calls)
			}
		})
	}
}

func TestBulkDeleteOverLimitRejectsWholeBatch(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"shell.err.bulkIdsTooMany": {
			"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行",
			"en-US": "At most %s items can be operated at once; %s selected, please split into batches.",
		},
	}, nil)

	const over = 201
	ids := make([]string, 0, over)
	for i := 0; i < over; i++ {
		ids = append(ids, "b-"+strconv.Itoa(i))
	}
	body, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}

	svc := &bulkDeleteStubService{}
	status, _, parsed := bulkDeleteResp(t, svc, string(body))

	if status != http.StatusBadRequest {
		t.Fatalf("超限应为 400（整批拒绝），实际 %d", status)
	}
	msg, _ := parsed["message"].(string)
	if !strings.Contains(msg, "200") || !strings.Contains(msg, "201") {
		t.Fatalf("超限文案应带上限与本次条数（200 / 201），实际 %q", msg)
	}
	if strings.Contains(msg, "shell.err.bulkIdsTooMany") {
		t.Fatalf("词条没取到，前端会看到裸 key：%q", msg)
	}
	if len(svc.calls) != 0 {
		t.Fatalf("超限必须整批拒绝（不静默截断执行），实际删了 %d 条", len(svc.calls))
	}
}

func TestBulkDeleteDedupesBeforeDelete(t *testing.T) {
	svc := &bulkDeleteStubService{}

	status, _, parsed := bulkDeleteResp(t, svc, `{"ids":[" b1 ","b1","  ","b2","b1"]}`)

	if status != http.StatusOK {
		t.Fatalf("部分重复不是错误，应 200，实际 %d", status)
	}
	if len(svc.calls) != 2 || svc.calls[0] != "b1" || svc.calls[1] != "b2" {
		t.Fatalf("应去重保序后逐条删除，实际调用 %v", svc.calls)
	}
	data := dataOf(t, parsed)
	if data["deleted"].(float64) != 2 || data["skipped"].(float64) != 0 {
		t.Fatalf("计数应如实：deleted=2 skipped=0，实际 %v", data)
	}
	items := itemsOf(t, parsed)
	if len(items) != 2 {
		t.Fatalf("items 应逐条回报（1 条 1 项），实际 %d 项", len(items))
	}
	if items[0]["id"] != "b1" || items[1]["id"] != "b2" {
		t.Fatalf("items 顺序应与提交顺序一致，实际 %v", items)
	}
}

func TestBulkDeletePartialFailureKeepsGoing(t *testing.T) {

	inUse := blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
		{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
	})
	svc := &bulkDeleteStubService{fail: map[string]error{"b2": inUse}}
	status, _, parsed := bulkDeleteResp(t, svc, `{"ids":["b1","b2","b3"]}`)

	if status != http.StatusOK {
		t.Fatalf("部分成功应 200（不伪装成整批失败），实际 %d", status)
	}
	if len(svc.calls) != 3 {
		t.Fatalf("单条失败不得中断整批：三条都应尝试，实际 %v", svc.calls)
	}
	data := dataOf(t, parsed)
	if data["deleted"].(float64) != 2 || data["skipped"].(float64) != 1 {
		t.Fatalf("计数应如实：deleted=2 skipped=1，实际 %v", data)
	}
	items := itemsOf(t, parsed)
	if len(items) != 3 {
		t.Fatalf("每条都要有结果项，实际 %d 项", len(items))
	}
	if items[1]["deleted"] != false {
		t.Fatalf("失败项 deleted 必须为 false，实际 %v", items[1])
	}
	failMsg, _ := items[1]["message"].(string)
	if strings.TrimSpace(failMsg) == "" {
		t.Fatal("失败项必须带原因（前端要显示「为什么这条删不掉」）")
	}
	if !strings.Contains(failMsg, "/pricing") {
		t.Fatalf("原因应带引用明细（运营据此去解除引用），实际 %q", failMsg)
	}

	if _, ok := items[0]["message"]; ok {
		t.Fatalf("成功项不该带 message，实际 %v", items[0])
	}
}

func TestBulkDeleteAllBlankSucceedsEmpty(t *testing.T) {
	svc := &bulkDeleteStubService{}
	status, _, parsed := bulkDeleteResp(t, svc, `{"ids":["   "," \t "]}`)

	if status != http.StatusOK {
		t.Fatalf("全空白不是错误，应 200（前端据此说「没有选中任何块」），实际 %d", status)
	}
	if len(svc.calls) != 0 {
		t.Fatalf("没有有效 id 不该调用 service，实际 %v", svc.calls)
	}
	items := itemsOf(t, parsed)
	if len(items) != 0 {
		t.Fatalf("items 应为空列表（不是 null —— 前端直接遍历它），实际 %v", items)
	}
	data := dataOf(t, parsed)
	if data["deleted"].(float64) != 0 || data["skipped"].(float64) != 0 {
		t.Fatalf("空结果计数应为 0，实际 %v", data)
	}
}

func TestBulkDeleteUnknownErrorStillControlledJSON(t *testing.T) {
	svc := &bulkDeleteStubService{fail: map[string]error{"b1": errors.New(`pq: relation "blocks" does not exist`)}}
	status, raw, parsed := bulkDeleteResp(t, svc, `{"ids":["b1"]}`)

	if status != http.StatusOK {
		t.Fatalf("单条未知错误仍应 200 + 逐条结果，实际 %d", status)
	}
	items := itemsOf(t, parsed)
	msg, _ := items[0]["message"].(string)
	if strings.Contains(msg, "pq:") || strings.Contains(msg, `relation "blocks"`) {
		t.Fatalf("驱动原文泄漏到逐条原因里：%q（body=%s）", msg, raw)
	}
	if strings.TrimSpace(msg) == "" {
		t.Fatal("未知错误也要给一句可读原因")
	}
}
