package runtimefragment

// fragment_err_test.go — 片段端点错误出口的受控性守卫（AGENTS.md「响应与错误处理」形态①）。
//
// 三条判据，各管一段：
//  1. 判定表本身（哨兵 → 状态码 + 文案）不漏、不误判；
//  2. 出口不把任何 error 原文 / 请求方可控输入铺进响应（**注射测试**：构造含标记的内部错误）；
//  3. 文案 key 与迁移 409 同源（词条没登记 = 英文站点上回落到中文原文，静默但可见）。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// fragmentErrLeakMarker 注射用的内部信息标记。
//
// 形状刻意像一条真实泄漏（驱动原文 + 表名）：它同时会被当作「非法的 context 值」送进端点，
// 于是同一条断言既覆盖「响应不回显请求输入」，也覆盖「响应不含内部错误原文」。
const fragmentErrLeakMarker = `SECRET_MARKER relation "pw_pages" does not exist`

// newFragmentFailContext 造一个不带路由的 gin 上下文（直调归口出口用）。
func newFragmentFailContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

// TestFragmentErrResponseMapping 判定表：四个哨兵各映射 400 + 受控文案，未知来源归口 500。
//
// 取词传 nil（取 fallback 中文原文）——这同时是「词条缺失时绝不输出裸 key / 空串」的守卫：
// 单测环境 i18n 缓存未加载，i18n 的兜底链产出的就是 fallback。
func TestFragmentErrResponseMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		text   string
		known  bool
	}{
		{"表单解析失败", errFragmentFormParse, http.StatusBadRequest, fragmentErrTextFormParse.fallback, true},
		{"参数过多", errFragmentParamCount, http.StatusBadRequest, fragmentErrTextTooMany.fallback, true},
		{"参数非法", errFragmentParamIllegal, http.StatusBadRequest, fragmentErrTextParam.fallback, true},
		{"上下文非法", errFragmentContext, http.StatusBadRequest, fragmentErrTextContext.fallback, true},
		{"未知来源（判定表未命中）", errors.New("pq: relation \"pw_pages\" does not exist"), http.StatusInternalServerError, fragmentErrTextInternal.fallback, false},
		{"nil 也走归口（不 panic）", nil, http.StatusInternalServerError, fragmentErrTextInternal.fallback, false},
	}
	for _, tc := range cases {
		status, text, known := fragmentErrResponse(tc.err, nil)
		if status != tc.status || text != tc.text || known != tc.known {
			t.Errorf("%s: 期望 (%d, %q, %v)，实际 (%d, %q, %v)",
				tc.name, tc.status, tc.text, tc.known, status, text, known)
		}
	}
}

// TestFragmentErrSentinelsAreAllMapped 新增哨兵必须登记进判定表。
//
// 这是「值域封闭」这件事实的守卫：漏登记的哨兵不会报错，只会让用户从「参数过多」
// 变成一句通用归口文案（既不报错也不记日志）—— 最难发现的那类回归。
func TestFragmentErrSentinelsAreAllMapped(t *testing.T) {
	for _, err := range []error{errFragmentFormParse, errFragmentParamCount, errFragmentParamIllegal, errFragmentContext} {
		status, text, known := fragmentErrResponse(err, nil)
		if !known {
			t.Errorf("哨兵 %q 未登记进判定表（会静默落归口文案）", err)
		}
		if status != http.StatusBadRequest {
			t.Errorf("哨兵 %q 是客户端请求问题，应 400，实际 %d", err, status)
		}
		if strings.TrimSpace(text) == "" {
			t.Errorf("哨兵 %q 的受控文案为空", err)
		}
	}
}

// TestFragmentFailNeverLeaksErrorDetail 归口出口的注射测试：**任何** error 都不会进响应。
//
// 第 1 条（未知来源）覆盖的是将来：这个出口的调用方最自然的演化是把渲染 / 数据源错误
// 也接到这里，那一刻原文里会是驱动原文、表名、SQL 片段 —— 出口必须把它们只送日志。
// fields 里也塞了标记串：日志字段同样不得进响应。
func TestFragmentFailNeverLeaksErrorDetail(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		want   string
	}{
		{"未知来源（模拟下游错误原文）", fmt.Errorf("pq: relation %q does not exist", fragmentErrLeakMarker), http.StatusInternalServerError, fragmentErrTextInternal.fallback},
		{"表单解析失败", errFragmentFormParse, http.StatusBadRequest, fragmentErrTextFormParse.fallback},
		{"参数过多", errFragmentParamCount, http.StatusBadRequest, fragmentErrTextTooMany.fallback},
		{"参数非法", errFragmentParamIllegal, http.StatusBadRequest, fragmentErrTextParam.fallback},
		{"上下文非法", errFragmentContext, http.StatusBadRequest, fragmentErrTextContext.fallback},
	}
	for _, tc := range cases {
		c, w := newFragmentFailContext(http.MethodGet, "/_fragments/loginPanel")
		fragmentFail(c, tc.err, nil, map[string]any{"context": fragmentErrLeakMarker})
		body := w.Body.String()
		if w.Code != tc.status {
			t.Errorf("%s: 期望 %d，实际 %d", tc.name, tc.status, w.Code)
		}
		if strings.Contains(body, "SECRET_MARKER") {
			t.Errorf("%s: 响应体泄漏了内部错误原文或请求方可控输入: %s", tc.name, body)
		}
		if body != tc.want {
			t.Errorf("%s: 响应体应是受控文案 %q，实际 %q", tc.name, tc.want, body)
		}
	}
}

// TestFragmentEndpointContextErrorDoesNotEchoInput 端到端：非法 context **不回显**。
//
// 这条用例在收口之前是**红的**：validateContext 原来返回
// `fmt.Errorf("非法的片段上下文: %q", ctx)`，而 endpoint 把它直出 ——
// /_fragments/loginPanel?context=<任意串> 的输出由请求方决定。
func TestFragmentEndpointContextErrorDoesNotEchoInput(t *testing.T) {
	w := doGet(t, newRouter(), "/_fragments/loginPanel?"+url.Values{"context": {fragmentErrLeakMarker}}.Encode())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法上下文应 400，实际 %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "SECRET_MARKER") {
		t.Fatalf("非法 context 被回显进了响应: %s", body)
	}
	// 单测环境 i18n 未初始化 → 取词恒回落包内中文原文（这正是断言可确定的原因）。
	if body != fragmentErrTextContext.fallback {
		t.Fatalf("响应体应是受控文案 %q，实际 %q", fragmentErrTextContext.fallback, body)
	}
}

// TestFragmentEndpointParamErrorDoesNotEchoInput 端到端：三类参数拒绝都只出受控文案。
//
// 标记串分别放在**参数值**与**参数名**里：参数名回路是最容易被忽略的一条
// （原来那些 `errors.New("参数非法")` 恰好没有回显，但形状上没有任何东西保证下一个改动不会）。
func TestFragmentEndpointParamErrorDoesNotEchoInput(t *testing.T) {
	tooLongValue := "/_fragments/loginPanel?" + url.Values{"q": {strings.Repeat("x", maxParamLen+1) + fragmentErrLeakMarker}}.Encode()
	tooLongName := "/_fragments/loginPanel?" + url.Values{strings.Repeat("n", 65): {fragmentErrLeakMarker}}.Encode()

	many := url.Values{}
	for i := 0; i <= maxParamCount; i++ {
		many.Set(fmt.Sprintf("p%02d", i), fragmentErrLeakMarker)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"参数值超长", tooLongValue, fragmentErrTextParam.fallback},
		{"参数名超长", tooLongName, fragmentErrTextParam.fallback},
		{"参数名数量超限", "/_fragments/loginPanel?" + many.Encode(), fragmentErrTextTooMany.fallback},
	}
	r := newRouter()
	for _, tc := range cases {
		w := doGet(t, r, tc.path)
		body := w.Body.String()
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: 应 400，实际 %d", tc.name, w.Code)
		}
		if strings.Contains(body, "SECRET_MARKER") {
			t.Errorf("%s: 响应体泄漏了请求方可控输入: %s", tc.name, body)
		}
		if body != tc.want {
			t.Errorf("%s: 响应体应是受控文案 %q，实际 %q", tc.name, tc.want, body)
		}
	}
}

// TestFragmentErrKeysRegisteredInMigration 代码里的 key 与迁移 409 同源。
//
// 不同源的症状是**静默的**：代码取一个不存在的 key，i18n 兜底链回落到包内中文原文，
// 中文站点看起来完全正常，英文站点上才露出中文 —— 而那时没人会想到是迁移漏登记。
func TestFragmentErrKeysRegisteredInMigration(t *testing.T) {
	const path = "../../../public/migrations/409_i18n_runtimefragment_err.sql"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到迁移文件 %s（路径变了？）: %v", path, err)
	}
	sql := string(data)
	for _, key := range []string{
		fragmentErrKeyFormParse, fragmentErrKeyTooManyParam,
		fragmentErrKeyParamInvalid, fragmentErrKeyContext, fragmentErrKeyInternal,
	} {
		if !strings.Contains(sql, "'"+key+"'") {
			t.Errorf("词条 key %q 未登记进迁移 409 —— 英文站点上会回落到包内中文原文", key)
		}
	}
	// 幂等形态（本批不允许改既有词条的值）。
	if !strings.Contains(sql, "ON CONFLICT (item_key, lang) DO NOTHING") {
		t.Errorf("迁移 409 必须幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING")
	}
}
