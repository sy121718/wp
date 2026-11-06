package adminhttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
)

func adminI18nWriteCall(t *testing.T, body, language string) (int, string) {
	t.Helper()
	r := drawerEngine()
	r.POST("/api/i18n/save", view.HTMLFragment, NewAdminI18nEntryHandle().I18nEntryWrite)
	req := httptest.NewRequest(http.MethodPost, "/api/i18n/save", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", language)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, assertDrawerHTML(t, w, w.Code)
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

func TestAdminI18nWriteRequiresExplicitAction(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.MsgBadRequest: {"en-US": "BAD-SHAPE-EN", "zh-CN": "BAD-SHAPE-ZH"},
	})
	for _, body := range []string{"", "ids=a%7Czh-CN", "action=upsert&key=a&lang=zh-CN&value=v"} {
		code, html := adminI18nWriteCall(t, body, "en-US")
		if code != http.StatusBadRequest || !strings.Contains(html, "BAD-SHAPE-EN") || strings.Contains(html, "BAD-SHAPE-ZH") {
			t.Fatalf("非法 action 应返回当前语言的 HTML 400：%d %s", code, html)
		}
	}
}

func TestAdminI18nWriteMissingRequiredFields(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.ErrI18nKeyEmpty:   {"en-US": "NEED-KEY"},
		adminenums.ErrI18nLangEmpty:  {"en-US": "NEED-LANG"},
		adminenums.ErrI18nValueEmpty: {"en-US": "NEED-VALUE"},
	})
	for _, tc := range []struct{ body, message string }{
		{"action=create&lang=zh-CN&value=v", "NEED-KEY"},
		{"action=create&key=a&value=v", "NEED-LANG"},
		{"action=create&key=a&lang=zh-CN&value=+++", "NEED-VALUE"},
		{"action=update&key=a&lang=zh-CN", "NEED-VALUE"},
		{"action=delete&lang=zh-CN", "NEED-KEY"},
		{"action=delete&key=a", "NEED-LANG"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			code, html := adminI18nWriteCall(t, tc.body, "en-US")
			if code != http.StatusBadRequest || !strings.Contains(html, tc.message) {
				t.Fatalf("必填失败应返回 HTML 400 与 %s：%d %s", tc.message, code, html)
			}
		})
	}
}

func TestAdminI18nFormBindingUsesOnlyBody(t *testing.T) {
	for _, tc := range []struct{ action, fields string }{
		{"create", "&key=a&lang=en-US&value=+正文%0A+&category=&remark="},
		{"update", "&key=a&lang=en-US&value=+正文%0A+"},
		{"delete", "&key=a&lang=en-US"},
		{"bulkDelete", "&ids=a%7Czh-CN&ids=b%7Cen-US&ids=a%7Czh-CN"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/?key=query&lang=query&ids=query", strings.NewReader("action="+tc.action+tc.fields))
			c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			var req admindto.I18nWriteReq
			if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
				t.Fatal(err)
			}
			if err := req.Validate(); err != nil {
				t.Fatal(err)
			}
			if tc.action == "bulkDelete" {
				if !reflect.DeepEqual(req.IDs, []string{"a|zh-CN", "b|en-US", "a|zh-CN"}) {
					t.Fatalf("重复字段绑定错误：%+v", req)
				}
			} else if req.Key != "a" || req.Lang != "en-US" || len(req.IDs) != 0 {
				t.Fatalf("query 不得补写 body：%+v", req)
			}
			if (tc.action == "create" || tc.action == "update") && req.Value != " 正文\n " {
				t.Fatalf("正文不得裁剪：%q", req.Value)
			}
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/?key=query&lang=en-US&value=filled", strings.NewReader("action=create"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var req admindto.I18nWriteReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		t.Fatal(err)
	}
	if req.Validate() == nil || req.Key != "" || req.Lang != "" || req.Value != "" {
		t.Fatalf("query 不得补齐缺失字段：%+v", req)
	}
}

func bulkBody(distinct, repeat int) string {
	values := url.Values{"action": {"bulkDelete"}}
	for i := 0; i < distinct; i++ {
		for j := 0; j < repeat; j++ {
			values.Add("ids", fmt.Sprintf("key-%d|zh-CN", i))
		}
	}
	return values.Encode()
}

func TestAdminI18nWriteBulkRejectsUnshapedIDWholeBatch(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{adminenums.MsgBadRequest: {"zh-CN": "BAD-SHAPE-ZH"}})
	// 空项与非 key|lang 形状一样整批拒绝：空项先被 Normalize 滤掉再当成成功删除是缺陷。
	for _, id := range []string{"no-separator", "|zh-CN", "a|", "", "   ", "a|zh-CN|extra"} {
		code, html := adminI18nWriteCall(t, url.Values{"action": {"bulkDelete"}, "ids": {"valid|zh-CN", id}}.Encode(), "zh-CN")
		if code != http.StatusBadRequest || !strings.Contains(html, "BAD-SHAPE-ZH") {
			t.Fatalf("坏标识 %q 应整批 400：%d %s", id, code, html)
		}
		if strings.TrimSpace(id) == "" {
			continue
		}
		if strings.Contains(html, id) {
			t.Fatalf("坏标识 %q 不得回显：%s", id, html)
		}
	}
}

// TestAdminI18nWriteBulkEmptySelectionRejects 空选择不是成功删除 0 条。
func TestAdminI18nWriteBulkEmptySelectionRejects(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{
		adminenums.ErrI18nSelectionEmpty: {"zh-CN": "NOTHING-SELECTED"},
	})
	code, html := adminI18nWriteCall(t, url.Values{"action": {"bulkDelete"}}.Encode(), "zh-CN")
	if code != http.StatusBadRequest || !strings.Contains(html, "NOTHING-SELECTED") {
		t.Fatalf("空选择应 400 并显示受控文案：%d %s", code, html)
	}
}

func TestAdminI18nWriteBulkTooManyRejectsWholeBatch(t *testing.T) {
	injectAdminI18nWriteEntries(t, map[string]map[string]string{bulkids.MsgTooMany: {"zh-CN": "TOO-MANY-上限%s-本次%s"}})
	code, html := adminI18nWriteCall(t, bulkBody(bulkids.MaxIDs+1, 1), "zh-CN")
	want := fmt.Sprintf("TOO-MANY-上限%d-本次%d", bulkids.MaxIDs, bulkids.MaxIDs+1)
	if code != http.StatusBadRequest || !strings.Contains(html, want) {
		t.Fatalf("超限应整批拒绝并显示参数：%d %s", code, html)
	}
	values, err := url.ParseQuery(bulkBody(bulkids.MaxIDs, 3))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := bulkids.Normalize(values["ids"])
	if err != nil || len(ids) != bulkids.MaxIDs {
		t.Fatalf("应按去重后数量计限：%d %v", len(ids), err)
	}
}
