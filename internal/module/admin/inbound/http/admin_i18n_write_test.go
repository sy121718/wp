package adminhttp

// admin_i18n_write_test.go — 词条写端点（POST /api/i18n/save）的**请求形状判据**。
//
// 端点与形状实现见 admin_i18n_write.go：请求体是严格 tagged union —— action=create / update /
// delete 三档，外加「键集恰好 {ids}」的批量删除档；未知 action、动作与字段混用、未知字段、
// 缺必需字段、字段类型不对一律 400。
//
// 本文件只钉形状与文案归属，**不连库**。写库行为（create upsert / update 缺席 404 / delete
// 幂等 / 批量逐条结果）由真实链用例覆盖：
// public/test/admin/feature/admin_i18n_edit_drawer_test.go（真实 gin + 真实 PG + CSRF + Casbin）。
// 因此这里「接受形状」的用例只断言**不再是 400**（形状过了），不断言 200 —— 无库时落到
// 基础设施错误档是预期行为，把 200 写死在这里等于把「本包不该连库」变成耦合。
//
// 语言归属：端点把 enums 的 key 交给 pkg/response，由它按请求语言取词（本模块不自己翻一遍）。
// 用例用 i18n.InjectForTest 注入**合成词条**（哨兵值），于是「响应里出现哨兵」等价于
// 「key 真的走到了取词层」，而不是「本模块把中文原文写进了响应」。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	adminenums "go_wp/internal/module/admin/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

// adminI18nWriteResponse 端点响应体（pkg/response.Response）。
type adminI18nWriteResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// adminI18nWriteBulkData 批量档的 data 体（json_write.js 直接读这几个字段）。
type adminI18nWriteBulkData struct {
	Deleted int `json:"deleted"`
	Skipped int `json:"skipped"`
	Items   []struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
		Message string `json:"message"`
	} `json:"items"`
}

// adminI18nWriteCall 走**真实 handler**（不是形状助手），语言由 Accept-Language 决定。
func adminI18nWriteCall(t *testing.T, body, accept string) (int, adminI18nWriteResponse) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/i18n/save", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept-Language", accept)
	}
	c.Request = req
	// 不注入 page marker：markI18nStale 对 nil 端口静默返回（见 admin_i18n_stale_test.go）。
	NewAdminI18nEntryHandle().I18nEntryWrite(c)

	var resp adminI18nWriteResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是 JSON：%v（原文 %s）", err, rec.Body.String())
		}
	}
	return rec.Code, resp
}

// injectAdminI18nWriteEntries 注入合成词条，结束时还原原值。
//
// 必须还原：i18n 缓存是进程级单例，同包其它用例（页面/抽屉/失效）都走它 ——
// 注进去不管，别处的取词会拿到这里的哨兵值，以「与本次改动无关的失败」变红。
func injectAdminI18nWriteEntries(t *testing.T, entries map[string]map[string]string) {
	t.Helper()
	previous := make(map[string]map[string]string, len(entries))
	for key, langs := range entries {
		snapshot := make(map[string]string, len(langs))
		for lang := range langs {
			snapshot[lang] = i18n.GetText(key, lang)
		}
		previous[key] = snapshot
	}
	i18n.InjectForTest(entries, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(previous, map[string]int{}) })
}

// TestAdminI18nWriteRejectsMalformedShapes 形状不合法的请求体一律 400（受控文案，不泄漏细节）。
func TestAdminI18nWriteRejectsMalformedShapes(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.MsgBadRequest: {"en-US": "BAD-SHAPE-EN", "zh-CN": "BAD-SHAPE-ZH"},
	})

	cases := []struct {
		name string
		body string
	}{
		{"未知 action", `{"action":"upsert","key":"a","lang":"zh-CN","value":"v"}`},
		{"批量档的 action 标签不可伪造", `{"action":"bulkDelete","ids":["a|zh-CN"]}`},
		{"未知字段", `{"action":"create","key":"a","lang":"zh-CN","value":"v","extra":1}`},
		{"delete 不接受 value（单条档混用）", `{"action":"delete","key":"a","lang":"zh-CN","value":"v"}`},
		{"delete 不接受 category", `{"action":"delete","key":"a","lang":"zh-CN","category":"ui"}`},
		{"批量档不接受 action 混合", `{"ids":["a|zh-CN"],"action":"create"}`},
		{"批量档不接受单条字段", `{"ids":["a|zh-CN"],"key":"a"}`},
		{"字段类型不对（key 不是字符串）", `{"action":"create","key":123,"lang":"zh-CN","value":"v"}`},
		{"字段类型不对（ids 不是字符串数组）", `{"ids":[1,2]}`},
		{"空请求体", ``},
		{"action 类型不对", `{"action":7,"key":"a","lang":"zh-CN","value":"v"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := adminI18nWriteCall(t, tc.body, "en-US")
			if code != http.StatusBadRequest {
				t.Fatalf("形状不合法的请求体应回 400，实际 %d（message=%q）", code, resp.Message)
			}
			if resp.Message != "BAD-SHAPE-EN" {
				t.Fatalf("形状错误应按请求语言出受控文案（ErrInvalidParams 的 en-US 词条），实际 %q —— "+
					"内部描述（哪个字段不该出现）只该进日志", resp.Message)
			}
		})
	}
}

// TestAdminI18nWriteMissingRequiredFields 缺必需字段回**具体**缺哪一项（不是笼统的「有必填项没填」）。
func TestAdminI18nWriteMissingRequiredFields(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.ErrI18nKeyEmpty:   {"en-US": "NEED-KEY"},
		adminenums.ErrI18nLangEmpty:  {"en-US": "NEED-LANG"},
		adminenums.ErrI18nValueEmpty: {"en-US": "NEED-VALUE"},
	})

	cases := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"create 缺 key", `{"action":"create","lang":"zh-CN","value":"v"}`, "NEED-KEY"},
		{"create 缺 lang", `{"action":"create","key":"a","value":"v"}`, "NEED-LANG"},
		{"create 内容全是空白", `{"action":"create","key":"a","lang":"zh-CN","value":"   "}`, "NEED-VALUE"},
		{"update 缺内容", `{"action":"update","key":"a","lang":"zh-CN"}`, "NEED-VALUE"},
		{"delete 缺 key", `{"action":"delete","lang":"zh-CN"}`, "NEED-KEY"},
		{"delete 缺 lang", `{"action":"delete","key":"a"}`, "NEED-LANG"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := adminI18nWriteCall(t, tc.body, "en-US")
			if code != http.StatusBadRequest {
				t.Fatalf("缺必需字段应回 400，实际 %d", code)
			}
			if resp.Message != tc.wantMsg {
				t.Fatalf("应回「缺哪一项」的受控文案（%s），实际 %q", tc.wantMsg, resp.Message)
			}
		})
	}
}

// TestAdminI18nWriteAcceptsWellFormedShapes 合法形状不被抢在形状层拒绝。
func TestAdminI18nWriteAcceptsWellFormedShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"create 全字段", `{"action":"create","key":"a","lang":"zh-CN","value":"v","category":"ui","remark":"r"}`},
		{"create 只给必需字段", `{"action":"create","key":"a","lang":"zh-CN","value":"v"}`},
		{"update 全字段", `{"action":"update","key":"a","lang":"en-US","value":"v2","category":"ui","remark":"r"}`},
		{"delete", `{"action":"delete","key":"a","lang":"zh-CN"}`},
		{"delete 带空白的身份串（去空白后合法）", `{"action":"delete","key":"  a  ","lang":" zh-CN "}`},
		{"批量删除", `{"ids":["a|zh-CN","b|en-US"]}`},
		{"批量删除：重复 id 去重后不超限", bulkBody(200, 3)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := adminI18nWriteCall(t, tc.body, "zh-CN")
			if code == http.StatusBadRequest {
				t.Fatalf("合法形状不该被形状层拒绝（400，message=%q）", resp.Message)
			}
		})
	}
}

// bulkBody 造一个批量请求体：distinct 个不同 id，每个重复 repeat 次。
func bulkBody(distinct, repeat int) string {
	ids := make([]string, 0, distinct*repeat)
	for i := 0; i < distinct; i++ {
		for j := 0; j < repeat; j++ {
			ids = append(ids, fmt.Sprintf("key-%d|zh-CN", i))
		}
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	return `{"ids":[` + strings.Join(quoted, ",") + `]}`
}

// TestAdminI18nWriteBulkRejectsUnshapedIdPerItem 复合键不成形状的条目：跳过并逐条给原因，不猜。
//
// 判据是「key|lang」二元组（分隔符见 adminI18nEntryKeySeparator）：猜一条 id 的含义等于
// 删错行，所以不成形状的条目整条计为跳过 —— 且这条路径在**没有库**时也成立（先判形状后落库）。
func TestAdminI18nWriteBulkRejectsUnshapedIdPerItem(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.ErrI18nKeyEmpty: {"zh-CN": "NEED-KEY"},
	})

	code, resp := adminI18nWriteCall(t, `{"ids":["no-separator","|zh-CN","a|","  "]}`, "zh-CN")
	if code != http.StatusOK {
		t.Fatalf("批量档不成形状的条目应整批回 200（逐条说明原因），实际 %d", code)
	}
	var data adminI18nWriteBulkData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("批量档 data 解析失败：%v（原文 %s）", err, string(resp.Data))
	}
	if data.Deleted != 0 || data.Skipped != 3 {
		t.Fatalf("不成形状的 3 条应全部跳过（空串在第 4 条被 Normalize 去掉了），实际 deleted=%d skipped=%d", data.Deleted, data.Skipped)
	}
	if len(data.Items) != 3 {
		t.Fatalf("逐条结果必须覆盖每一条收到的 id（前端按 items[].deleted 决定是否移除行），实际 %d 条", len(data.Items))
	}
	for _, item := range data.Items {
		if item.Deleted {
			t.Errorf("id=%q 不该被判为已删除", item.ID)
		}
		if item.Message != "NEED-KEY" {
			t.Errorf("id=%q 的逐条原因应是受控文案「key 不能为空」（按请求语言取词），实际 %q", item.ID, item.Message)
		}
	}
}

// TestAdminI18nWriteBulkTooManyRejectsWholeBatch 超限整批拒绝，且两个数字是「去重后的条数」。
func TestAdminI18nWriteBulkTooManyRejectsWholeBatch(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "TOO-MANY-上限%s-本次%s"},
	})

	// 201 个不同 id → 超限；整批拒绝（不截断、不部分执行）。
	code, resp := adminI18nWriteCall(t, bulkBody(bulkids.MaxIDs+1, 1), "zh-CN")
	if code != http.StatusBadRequest {
		t.Fatalf("超过 %d 条应整批拒绝（400），实际 %d", bulkids.MaxIDs, code)
	}
	want := fmt.Sprintf("TOO-MANY-上限%d-本次%d", bulkids.MaxIDs, bulkids.MaxIDs+1)
	if resp.Message != want {
		t.Fatalf("超限文案应带 (上限, 去重后条数) 两个参数，实际 %q，期望 %q", resp.Message, want)
	}

	// 同一个 200 条上限、但请求里有重复：去重后不超限，因此不该被拒。
	code, resp = adminI18nWriteCall(t, bulkBody(bulkids.MaxIDs, 3), "zh-CN")
	if code == http.StatusBadRequest {
		t.Fatalf("超过上限的原始条数但去重后不超限时不该拒绝，实际 400（message=%q）", resp.Message)
	}
}

// TestAdminI18nBulkTooManyMessage 超限消息的带参协议形态（key|max|count）。
func TestAdminI18nBulkTooManyMessage(t *testing.T) {
	_, err := bulkids.Normalize(func() []string {
		ids := make([]string, 0, bulkids.MaxIDs+5)
		for i := 0; i < bulkids.MaxIDs+5; i++ {
			ids = append(ids, fmt.Sprintf("k%d", i))
		}
		return ids
	}())
	msg := adminI18nBulkTooManyMessage(err)
	want := fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, bulkids.MaxIDs, bulkids.MaxIDs+5)
	if msg != want {
		t.Fatalf("协议形态应为 key|max|count，实际 %q，期望 %q", msg, want)
	}
}

// TestAdminI18nWriteRejectsExplicitNull 「显式 null」是形状错：encoding/json 把 null 解进
// string / []string 都不报错（落到零值 "" / nil），不判原文就会静默变成「空批量」或
// 「把已有分类清空」—— 所以 null 必须与合法省略区分开，逐处 400。
func TestAdminI18nWriteRejectsExplicitNull(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.MsgBadRequest: {"en-US": "BAD-SHAPE-EN", "zh-CN": "BAD-SHAPE-ZH"},
	})

	cases := []struct {
		name string
		body string
	}{
		{"批量档 ids 是 null（曾解成 nil 空批量 → 200 空结果）", `{"ids":null}`},
		{"批量档 ids 数组里有 null 项", `{"ids":["a|zh-CN",null]}`},
		{"批量档 ids 只有 null 项", `{"ids":[null]}`},
		{"批量档 ids 不是数组（字符串）", `{"ids":"a|zh-CN"}`},
		{"批量档 ids 不是数组（数字）", `{"ids":5}`},
		{"批量档 ids 是对象", `{"ids":{"a":"zh-CN"}}`},
		{"action 是 null", `{"action":null,"key":"a","lang":"zh-CN","value":"v"}`},
		{"create 的 key 是 null", `{"action":"create","key":null,"lang":"zh-CN","value":"v"}`},
		{"create 的 lang 是 null", `{"action":"create","key":"a","lang":null,"value":"v"}`},
		{"create 的 value 是 null", `{"action":"create","key":"a","lang":"zh-CN","value":null}`},
		{"可选 category 显式 null（曾静默清空已有分类）", `{"action":"create","key":"a","lang":"zh-CN","value":"v","category":null}`},
		{"可选 remark 显式 null", `{"action":"create","key":"a","lang":"zh-CN","value":"v","remark":null}`},
		{"update 的 category 显式 null", `{"action":"update","key":"a","lang":"zh-CN","value":"v","category":null}`},
		{"delete 的 lang 是 null", `{"action":"delete","key":"a","lang":null}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := adminI18nWriteCall(t, tc.body, "en-US")
			if code != http.StatusBadRequest {
				t.Fatalf("显式 null 应回 400（形状错），实际 %d（message=%q）", code, resp.Message)
			}
			if resp.Message != "BAD-SHAPE-EN" {
				t.Fatalf("null 属形状错，应回归口参数文案，实际 %q —— 与「业务缺项」的文案不是同一条", resp.Message)
			}
		})
	}
}

// TestAdminI18nWriteOptionalFieldAbsentIsLegal 「省略」与「显式 null」是两回事：
// 可选字段（category / remark）省略必须合法 —— 这条用例钉住的是区分本身，
// 不是某一个取值（把可选字段一并禁 null 之外的另一种错法是连省略也拒）。
func TestAdminI18nWriteOptionalFieldAbsentIsLegal(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.MsgBadRequest: {"zh-CN": "BAD-SHAPE-ZH"},
	})

	cases := []struct {
		name string
		body string
	}{
		{"create 省略 category / remark", `{"action":"create","key":"a","lang":"zh-CN","value":"v"}`},
		{"create 显式给空串（与省略等价，都是合法取值）", `{"action":"create","key":"a","lang":"zh-CN","value":"v","category":"","remark":""}`},
		{"update 省略 category / remark", `{"action":"update","key":"a","lang":"zh-CN","value":"v"}`},
		{"delete 只有两个身份字段", `{"action":"delete","key":"a","lang":"zh-CN"}`},
		{"批量档 ids 空数组（合法形状，落库层再定结果）", `{"ids":[]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp := adminI18nWriteCall(t, tc.body, "zh-CN")
			if code == http.StatusBadRequest {
				t.Fatalf("省略可选字段是合法形状，不该 400（message=%q）", resp.Message)
			}
		})
	}
}
