package mailhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

type mailContactWriteStub struct {
	mailcontract.MailService

	created []*maildto.SaveContactReq
	updated []*maildto.SaveContactReq
	deleted [][]uint64
	tagged  []*maildto.TagContactsReq

	deleteErr  error
	deleteMiss bool
	tagErr     error
	tagMiss    bool
	tagNoop    bool
}

func (s *mailContactWriteStub) CreateContact(_ context.Context, req *maildto.SaveContactReq) (uint64, error) {
	s.created = append(s.created, cloneSaveReq(req))
	return 77, nil
}

func (s *mailContactWriteStub) UpdateContact(_ context.Context, req *maildto.SaveContactReq) error {
	s.updated = append(s.updated, cloneSaveReq(req))
	return nil
}

func (s *mailContactWriteStub) DeleteContacts(_ context.Context, req *maildto.DeleteContactsReq) (int64, error) {
	s.deleted = append(s.deleted, append([]uint64(nil), req.IDs...))
	if s.deleteErr != nil {
		return 0, s.deleteErr
	}
	if s.deleteMiss {
		return 0, &mailTestBlockedError{itemID: "9", reasonKey: mailenums.ErrContactNotFound, requested: len(req.IDs)}
	}
	return int64(len(req.IDs)), nil
}

func (s *mailContactWriteStub) TagContacts(_ context.Context, req *maildto.TagContactsReq) (int, error) {
	s.tagged = append(s.tagged, &maildto.TagContactsReq{
		IDs: append([]uint64(nil), req.IDs...), Add: append([]string(nil), req.Add...),
		Remove: append([]string(nil), req.Remove...), OperatorID: req.OperatorID,
	})
	if s.tagErr != nil {
		return 0, s.tagErr
	}
	if s.tagMiss {
		return 0, &mailTestBlockedError{itemID: "9", reasonKey: mailenums.ErrContactNotFound, requested: len(req.IDs)}
	}
	if s.tagNoop {
		return 0, nil
	}
	return len(req.IDs), nil
}

// mailTestBlockedError 复刻 service 的整批拒绝事实：
// 单条请求取该条的具体原因、其余取公共整批文案；两种形状都保留 items，状态按原因推导。
type mailTestBlockedError struct {
	itemID    string
	reasonKey string
	requested int
}

func (e *mailTestBlockedError) key() string {
	if e.requested == 1 {
		return e.reasonKey
	}
	return mailenums.ErrBatchReject
}

func (e *mailTestBlockedError) Error() string            { return e.key() }
func (e *mailTestBlockedError) Facing() (string, string) { return e.key(), "" }
func (e *mailTestBlockedError) HTTPStatus() int          { return mailenums.StatusFor(e.reasonKey) }
func (e *mailTestBlockedError) BlockedItems() []facing.BlockedItem {
	return []facing.BlockedItem{{ID: e.itemID, ReasonKey: e.reasonKey}}
}

func cloneSaveReq(req *maildto.SaveContactReq) *maildto.SaveContactReq {
	out := *req
	out.Tags = append([]string(nil), req.Tags...)
	return &out
}

const mailContactWriteUserID = int64(987654401)

// newMailContactWriteEngine 复刻生产接线：失败出口、请求取词与 HTML 渲染都必须在场，
// 否则失败会落成 JSON 或空响应，测出来的不是真实行为。
func newMailContactWriteEngine(h *Handle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Set("user_id", mailContactWriteUserID)
		c.Next()
	})
	api := r.Group("/api/mail")
	api.POST("/contact/save", pageview.HTMLFragment, h.ContactSave)
	api.POST("/contact/delete", pageview.HTMLFragment, h.ContactDelete)
	api.POST("/contact/tag", pageview.HTMLFragment, h.ContactTag)
	return r
}

func postMailContactForm(t *testing.T, r *gin.Engine, path string, form url.Values, lang string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if lang != "" {
		req.Header.Set("Cookie", "lang="+lang)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func assertMailWriteHTML(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("界面响应必须是 HTML，实际 %q，正文：%s", rec.Header().Get("Content-Type"), body)
	}
	for _, forbidden := range []string{"<!DOCTYPE", "<html", "<body", "SQLSTATE", "mail_contacts"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("局部响应不得携带整页或内部细节 %q：%s", forbidden, body)
		}
	}
	for _, header := range []string{"Location", "HX-Redirect", "HX-Location", "HX-Refresh"} {
		if rec.Header().Get(header) != "" {
			t.Fatalf("后端不得控制导航：%s=%q", header, rec.Header().Get(header))
		}
	}
	return body
}

func injectMailContactWriteEntries(t *testing.T, entries map[string]map[string]string) {
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

func TestMailContactSaveRejectsMalformedID(t *testing.T) {
	r := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{}))

	for _, raw := range []string{"abc", "-1", "1.5"} {
		rec := postMailContactForm(t, r, "/api/mail/contact/save", url.Values{"id": {raw}, "email": {"a@b.c"}}, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id=%q 应 400，实际 %d：%s", raw, rec.Code, rec.Body.String())
		}
		assertMailWriteHTML(t, rec)
	}
}

func TestMailContactSaveCreateAndUpdate(t *testing.T) {
	stub := &mailContactWriteStub{}
	r := newMailContactWriteEngine(NewHandle(stub))

	rec := postMailContactForm(t, r, "/api/mail/contact/save", url.Values{
		"email": {"a@b.c"}, "name": {"张三"}, "tags": {"vip, 华南 ,vip"},
		"status": {"pending"}, "consent_source": {"官网表单"},
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("新建应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if body := assertMailWriteHTML(t, rec); !strings.Contains(body, "data-write-result") && !strings.Contains(body, "role=\"status\"") {
		t.Fatalf("成功应答应是 Jet 片段：%s", body)
	}
	if len(stub.created) != 1 || len(stub.updated) != 0 {
		t.Fatalf("id 缺省应走新建，实际 created=%d updated=%d", len(stub.created), len(stub.updated))
	}
	got := stub.created[0]
	if got.ID != 0 || got.Email != "a@b.c" || got.Name != "张三" || got.ConsentSource != "官网表单" {
		t.Fatalf("字段没有原样转给 service：%#v", got)
	}
	if strings.Join(got.Tags, "|") != "vip|华南|vip" {
		t.Fatalf("标签按 ; | , 切分并去空白（与导入同口径），实际 %#v", got.Tags)
	}
	if got.OperatorID != uint64(mailContactWriteUserID) {
		t.Fatalf("操作人必须取自会话（builtin.GetUserID），实际 %d", got.OperatorID)
	}

	rec = postMailContactForm(t, r, "/api/mail/contact/save", url.Values{
		"id": {"5"}, "email": {"a@b.c"}, "name": {"李四"},
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("编辑应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if len(stub.updated) != 1 || stub.updated[0].ID != 5 {
		t.Fatalf("id>0 应走编辑，实际 %#v", stub.updated)
	}
	if stub.updated[0].OperatorID != uint64(mailContactWriteUserID) {
		t.Fatalf("编辑的操作人也必须取自会话，实际 %d", stub.updated[0].OperatorID)
	}
}

func TestMailContactDeleteSingleAndBulk(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrInvalidParam:    {"zh-CN": "参数-非法", "en-US": "PARAM-INVALID-EN"},
		mailenums.ErrBatchReject:     {"zh-CN": "整批未执行", "en-US": "BATCH-REJECTED-EN"},
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})

	stub := &mailContactWriteStub{}
	r := newMailContactWriteEngine(NewHandle(stub))

	rec := postMailContactForm(t, r, "/api/mail/contact/delete", url.Values{"id": {"7"}}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("单条删除应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertMailWriteHTML(t, rec)
	if len(stub.deleted) != 1 || len(stub.deleted[0]) != 1 || stub.deleted[0][0] != 7 {
		t.Fatalf("单条删除应把 [7] 交给 service，实际 %#v", stub.deleted)
	}

	// 合法批量只调用一次 service，id 去重后整批转交。
	rec = postMailContactForm(t, r, "/api/mail/contact/delete",
		url.Values{"ids": {"1", "2", "2"}}, "en-US")
	if rec.Code != http.StatusOK {
		t.Fatalf("批量删除应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertMailWriteHTML(t, rec)
	if len(stub.deleted) != 2 || len(stub.deleted[1]) != 2 {
		t.Fatalf("批量删除必须一次调用 service 且整批传 id，实际 %#v", stub.deleted)
	}

	// 混合非法/合法 id 整批拒绝，不进 service。
	before := len(stub.deleted)
	rec = postMailContactForm(t, r, "/api/mail/contact/delete",
		url.Values{"ids": {"1", "x", "0"}}, "en-US")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("混合非法 id 应整批 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := assertMailWriteHTML(t, rec)
	if !strings.Contains(body, "BATCH-REJECTED-EN") || strings.Contains(body, "PARAM-INVALID-EN") {
		t.Fatalf("失败区只播报公共一句，不逐条拼明细：%s", body)
	}
	if len(stub.deleted) != before {
		t.Fatalf("非法批次不该进 service，实际多调用 %d 次", len(stub.deleted)-before)
	}
}

func TestMailContactDeleteMissingRowIsNotReportedAsSuccess(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrBatchReject:     {"zh-CN": "整批未执行", "en-US": "BATCH-REJECTED-EN"},
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})
	r := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{deleteMiss: true}))

	rec := postMailContactForm(t, r, "/api/mail/contact/delete", url.Values{"ids": {"9"}}, "en-US")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("批量里删不存在的 id 应整批失败且按缺失给 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := assertMailWriteHTML(t, rec)
	if strings.Contains(body, "Deleted 1 contacts") {
		t.Fatalf("真实删除 0 行却报了成功：%s", body)
	}
	if !strings.Contains(body, "NOT-FOUND-EN") {
		t.Fatalf("单项拒绝应给具体业务原因，逐条明细仍只进 data.items：%s", body)
	}
}

func TestMailContactDeleteSingleMissingRowIsNotReportedAsSuccess(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrBatchReject:     {"zh-CN": "整批未执行", "en-US": "BATCH-REJECTED-EN"},
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})
	r := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{deleteMiss: true}))

	rec := postMailContactForm(t, r, "/api/mail/contact/delete", url.Values{"id": {"9"}}, "en-US")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("单条删 0 行应 404（不许说「删除成功」），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := assertMailWriteHTML(t, rec)
	if !strings.Contains(body, "NOT-FOUND-EN") || !strings.Contains(body, "data-ui-error") {
		t.Fatalf("单条失败应给具体业务原因并落在错误区，实际：%s", body)
	}

	ok := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{}))
	rec = postMailContactForm(t, ok, "/api/mail/contact/delete", url.Values{"id": {"9"}}, "en-US")
	if rec.Code != http.StatusOK {
		t.Fatalf("删掉 1 行的单条删除应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

func TestMailContactBulkIDsLimit(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "上限 %s 条，本次 %s 条", "en-US": "LIMIT %s / GOT %s"},
	})
	r := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{}))

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, strconv.Itoa(i+1))
	}
	rec := postMailContactForm(t, r, "/api/mail/contact/delete", url.Values{"ids": ids}, "en-US")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超过 %d 条应整批 400，实际 %d：%s", bulkids.MaxIDs, rec.Code, rec.Body.String())
	}
	body := assertMailWriteHTML(t, rec)
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限文案要同时说清上限与本次条数，实际：%s", body)
	}
}

func TestMailContactTagResultShape(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrContactTagEmpty: {"zh-CN": "差量-空", "en-US": "TAG-EMPTY-EN"},
		mailenums.ErrBatchReject:     {"zh-CN": "整批未执行", "en-US": "BATCH-REJECTED-EN"},
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})

	stub := &mailContactWriteStub{}
	r := newMailContactWriteEngine(NewHandle(stub))

	rec := postMailContactForm(t, r, "/api/mail/contact/tag",
		url.Values{"ids": {"1"}, "add": {"vip"}, "remove": {"trial"}}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("打标签应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := assertMailWriteHTML(t, rec)
	if len(stub.tagged) != 1 || strings.Join(stub.tagged[0].Add, "|") != "vip" || strings.Join(stub.tagged[0].Remove, "|") != "trial" {
		t.Fatalf("增与减必须在同一条请求里转给 service，实际 %#v", stub.tagged)
	}
	if strings.Contains(body, "data-ui-error") {
		t.Fatalf("成功应走公共成功提示，实际：%s", body)
	}

	rec = postMailContactForm(t, r, "/api/mail/contact/tag",
		url.Values{"ids": {"1"}, "add": {"  "}, "remove": {""}}, "en-US")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空差量应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if body := assertMailWriteHTML(t, rec); !strings.Contains(body, "TAG-EMPTY-EN") {
		t.Fatalf("空差量文案应按请求语言取词，实际：%s", body)
	}

	// 不存在的 id：整批拒绝，状态按原因给 404，逐条原因不进 HTML。
	missing := &mailContactWriteStub{tagMiss: true}
	rec = postMailContactForm(t, newMailContactWriteEngine(NewHandle(missing)),
		"/api/mail/contact/tag", url.Values{"ids": {"9"}, "add": {"vip"}}, "en-US")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的 id 必须让整批失败，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if body := assertMailWriteHTML(t, rec); !strings.Contains(body, "NOT-FOUND-EN") {
		t.Fatalf("单项拒绝应给具体业务原因，实际：%s", body)
	}

	// 合法同值 no-op：不算失败，仍走公共成功提示。
	noop := &mailContactWriteStub{tagNoop: true}
	rec = postMailContactForm(t, newMailContactWriteEngine(NewHandle(noop)),
		"/api/mail/contact/tag", url.Values{"ids": {"9"}, "add": {"vip"}}, "en-US")
	if rec.Code != http.StatusOK {
		t.Fatalf("同值 no-op 应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

var (
	mailAPIContactPostRe  = regexp.MustCompile(`g\.POST\("(/contact/[a-z]+)",\s*(?:pageview\.HTMLFragment,\s*)?mailenums\.(MailContact[A-Za-z]+)`)
	mailPageContactPostRe = regexp.MustCompile(`pages\.POST\("(/mail/contact[^"]*)"`)
)

func TestMailContactWriteEndpointsAreRealRoutes(t *testing.T) {
	raw, err := os.ReadFile("mail_router.go")
	if err != nil {
		t.Fatalf("读 mail_router.go 失败：%v", err)
	}
	found := map[string]string{}
	for _, m := range mailAPIContactPostRe.FindAllStringSubmatch(string(raw), -1) {
		found["/api/mail"+m[1]] = "mailenums." + m[2]
	}
	want := map[string]string{
		"/api/mail/contact/save":   "mailenums.MailContactSave",
		"/api/mail/contact/delete": "mailenums.MailContactDelete",
		"/api/mail/contact/tag":    "mailenums.MailContactTag",
	}
	for path, perm := range want {
		got, ok := found[path]
		if !ok {
			t.Errorf("%s 没有在 mail_router.go 里注册成真实路由 —— 它又会退回「有权限点、没有路由」的幽灵对象（AI 工具 fail closed）", path)
			continue
		}
		if got != perm {
			t.Errorf("%s 注册用的权限点应是 %s，实际 %s", path, perm, got)
		}
	}

}

func TestMailContactPageWritePostsAreGone(t *testing.T) {
	raw, err := os.ReadFile("mail_page_router.go")
	if err != nil {
		t.Fatalf("读 mail_page_router.go 失败：%v", err)
	}
	got := map[string]bool{}
	for _, m := range mailPageContactPostRe.FindAllStringSubmatch(string(raw), -1) {
		got[m[1]] = true
	}
	for _, path := range []string{"/mail/contact/save", "/mail/contact/delete", "/mail/contacts/bulk-delete", "/mail/contacts/bulk-tag"} {
		if got[path] {
			t.Errorf("页面 POST %s 还在 —— 写动作已经由浏览器直调端点，这条通道必须删掉", path)
		}
	}
	if !got["/mail/contacts/bulk-status"] {
		t.Errorf("批量改状态仍是页面端点，不该被一起删掉")
	}
}
