package blockhttp

// block_bulk_delete_test.go — 批量删除 JSON 端点（POST /api/block/bulk-delete）的护栏。
//
// 这个端点是「前端直调 JSON API」的第一条批量写路径（页面 POST 的薄转发会被逐批删掉），
// 它身上有四件**错了不会有人发现**的事，本文件逐条钉住：
//
//  1. **上限与去重必须在服务端**。客户端那份 200 上限改一行 JS 就没了；ids 是勾选框给的，
//     长度完全由调用方决定 —— 一次上万个 id 会把「逐条走 svc.Delete」放大成上万次事务往返。
//  2. **判断上限用的是去重后的条数**（重复提交不该把用户挡在门外），且超限整批拒绝、
//     不静默截断（截断会让界面显示「已删除 200 个」看着像成功，剩下 300 条无声消失）。
//  3. **单条失败不中断整批**，部分成功回 200 + 真实计数 + 逐条原因：
//     整批回滚会让用户以为「一个都没删」然后反复重试。
//  4. 失败响应是**受控 JSON**：绑定错误原文（Go 结构体、字段名）只能进日志。
//
// 断言的是响应形状与调用次数，不碰数据库：service 用桩。

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

// bulkDeleteStubService 只实现 Delete；其余方法 panic。
//
// panic 而不是零值：批量删除**不该**碰 List/Detail/Create/Update/CloneAST，
// 任何多余调用都说明语义跑偏了（例如「先查一遍引用再删」），那种情况必须立刻炸。
type bulkDeleteStubService struct {
	fail  map[string]error // 按 id 指定失败
	calls []string         // 实际调用顺序（用于断言去重与「不中断」）
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

// bulkDeleteResp 调一次端点，返回状态码、原始体与解析结果。
//
// parsed 用 map 而不是结构体：本文件要断言的是**协议形状**（code / message / data.deleted /
// data.items[].deleted|message），用 map 才能在字段被改名/被省略时立刻发现
// （结构体 unmarshal 对缺失字段是静默的，那正是这类缺陷最难发现的形态）。
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

// dataOf 取响应体里的 data 对象。
func dataOf(t *testing.T, parsed map[string]any) map[string]any {
	t.Helper()
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data 对象（前端按 data.deleted / data.items 渲染结果），实际 %v", parsed)
	}
	return data
}

// itemsOf 取 data.items 列表。
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

// TestBulkDeleteBindFailIsControlledJSON 绑定失败 → 400 受控 JSON，且不碰 service。
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

// TestBulkDeleteOverLimitRejectsWholeBatch 超过上限 → 400，整批拒绝，文案带上限与去重后条数。
//
// 这里注入 277 的词条（生产由迁移 seed 进 sys_i18n），因此断言的是**用户看到的那句话**，
// 而不是「响应里有个非空字符串」——后者在词条丢失时会静默变绿。
func TestBulkDeleteOverLimitRejectsWholeBatch(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"shell.err.bulkIdsTooMany": {
			"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行",
			"en-US": "At most %s items can be operated at once; %s selected, please split into batches.",
		},
	}, nil)

	const over = 201 // 上限 200，超一条即整批拒绝
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

// TestBulkDeleteDedupesBeforeDelete 去重与去空白在服务端生效：重复 id 只删一次、顺序保持。
func TestBulkDeleteDedupesBeforeDelete(t *testing.T) {
	svc := &bulkDeleteStubService{}
	// 注意：「元素是空串」在**绑定期**就被拒（BulkDeleteReq 的 dive,required），见上面的绑定用例；
	// 能走到归一化的空白形态是「纯空白字符串」（非零值，required 放行），它必须被去空白后丢掉。
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

// TestBulkDeletePartialFailureKeepsGoing 单条失败不中断整批：200 + 真实计数 + 逐条原因。
func TestBulkDeletePartialFailureKeepsGoing(t *testing.T) {
	// 中间那条被页面引用（global 块）：带引用明细的拒绝错误 —— 前端要把明细显示出来，
	// 否则运营不知道去哪儿解除引用。
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
	// 成功项不回 message：前端据 deleted 判定，多一句「已删除」只是噪声。
	if _, ok := items[0]["message"]; ok {
		t.Fatalf("成功项不该带 message，实际 %v", items[0])
	}
}

// TestBulkDeleteAllBlankSucceedsEmpty 归一化后一条不剩 → 200 + 空结果（由前端说「没有选中」）。
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

// TestBulkDeleteUnknownErrorStillControlledJSON 未知错误（既非引用拒绝、也非 not found）也要受控回执。
//
// 前端只看 item.message：这里若不接住，运营看到的就是驱动原文。
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
