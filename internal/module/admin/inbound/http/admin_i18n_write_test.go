package adminhttp

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	adminenums "go_wp/internal/module/admin/enums"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

type adminI18nWriteResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type adminI18nWriteBulkData struct {
	Deleted int `json:"deleted"`
	Skipped int `json:"skipped"`
	Items   []struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
		Message string `json:"message"`
	} `json:"items"`
}

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

	NewAdminI18nEntryHandle().I18nEntryWrite(c)

	var resp adminI18nWriteResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是 JSON：%v（原文 %s）", err, rec.Body.String())
		}
	}
	return rec.Code, resp
}

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

func TestAdminI18nWriteBulkTooManyRejectsWholeBatch(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "TOO-MANY-上限%s-本次%s"},
	})

	code, resp := adminI18nWriteCall(t, bulkBody(bulkids.MaxIDs+1, 1), "zh-CN")
	if code != http.StatusBadRequest {
		t.Fatalf("超过 %d 条应整批拒绝（400），实际 %d", bulkids.MaxIDs, code)
	}
	want := fmt.Sprintf("TOO-MANY-上限%d-本次%d", bulkids.MaxIDs, bulkids.MaxIDs+1)
	if resp.Message != want {
		t.Fatalf("超限文案应带 (上限, 去重后条数) 两个参数，实际 %q，期望 %q", resp.Message, want)
	}

	code, resp = adminI18nWriteCall(t, bulkBody(bulkids.MaxIDs, 3), "zh-CN")
	if code == http.StatusBadRequest {
		t.Fatalf("超过上限的原始条数但去重后不超限时不该拒绝，实际 400（message=%q）", resp.Message)
	}
}

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
