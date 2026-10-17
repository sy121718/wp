package mailhttp

// mail_contact_write_test.go — 联系人 JSON 写端点（POST /api/mail/contact/{save,delete,tag}）的契约守卫。
//
// 这三条路径在本批之前是「有权限点、有策略、没有路由」的幽灵对象：页面 POST 借用它们当 obj，
// 而 AI 工具的判权（permission.RoutesOf）查不到路由就 fail closed。现在它们是真实端点，
// 于是必须像所有 JSON 端点一样被钉住三件事：
//
//	① **严格形状**：白名单外的字段、显式 null、类型不符一律 400。
//	   （显式 null 必须单独判 —— encoding/json 把 null 解进 string / []string 都不报错，
//	    落到零值就变成「一次空批量」或「把字段清空」，而请求看起来完全合法。）
//	② **恰好一种形状**：删除端点的单条 {id} 与批量 {ids} 只能给一个 ——
//	   这是权限模型的硬约束（一个 code 只有一列 api_path），不是省事。
//	③ **逐条结果与口径**：删除回 {deleted,skipped,items}（成功条目 deleted=true，前端据此移除行）；
//	   打标签回 {updated,skipped,items}（成功条目也在，且**不带 deleted 语义**）。
//
// 库侧（真实 PG + 角色授权链路）在 public/test/mail/feature 与 public/test/feature 里，
// 本文件不连库：service 用桩，只验「handler 怎么收、怎么判、怎么回」。
//
// 端点注册与页面 POST 的下线同在本文件：前者扫 mail_router.go（真实注册写法），
// 后者扫 mail_page_router.go 的**反向**断言（本批的交付物就是「页面 POST 消失」，
// 与 task-13 刻意不写「必须不是真实路由」的方向相反 —— 那条断言当时会挡住新 API 上线）。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
)

// —— 桩 service：只实现本文件用到的四个写方法，其余由嵌入接口兜住（被调用即 panic） ——

type mailContactWriteStub struct {
	mailcontract.MailService

	created []*maildto.SaveContactReq
	updated []*maildto.SaveContactReq
	deleted [][]uint64
	tagged  []*maildto.TagContactsReq

	deleteErr  error
	deleteMiss bool // 模拟「这一条本来就不存在」：删 0 行且不报错
	tagErr     error
	tagMiss    bool // 模拟「该 id 不存在」：changed=0 且无错
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
		return 0, nil
	}
	return int64(len(req.IDs)), nil
}

func (s *mailContactWriteStub) TagContacts(_ context.Context, req *maildto.TagContactsReq) (int, int, error) {
	s.tagged = append(s.tagged, &maildto.TagContactsReq{
		IDs: append([]uint64(nil), req.IDs...), Add: append([]string(nil), req.Add...),
		Remove: append([]string(nil), req.Remove...), OperatorID: req.OperatorID,
	})
	if s.tagErr != nil {
		return 0, 0, s.tagErr
	}
	if s.tagMiss {
		return 0, 0, nil
	}
	return 1, 0, nil
}

func cloneSaveReq(req *maildto.SaveContactReq) *maildto.SaveContactReq {
	out := *req
	out.Tags = append([]string(nil), req.Tags...)
	return &out
}

// —— 装配与请求辅助 ——

const mailContactWriteUserID = int64(987654401)

func newMailContactWriteEngine(h *Handle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/mail", func(c *gin.Context) {
		c.Set("user_id", mailContactWriteUserID)
		c.Next()
	})
	api.POST("/contact/save", h.ContactSave)
	api.POST("/contact/delete", h.ContactDelete)
	api.POST("/contact/tag", h.ContactTag)
	return r
}

// postMailContactJSON 发一个 JSON 请求；lang 非空时用它协商响应语言（Accept-Language 首段）。
func postMailContactJSON(t *testing.T, r *gin.Engine, path, body, lang string) (int, map[string]interface{}) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	r.ServeHTTP(rec, req)
	parsed := map[string]interface{}{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("响应不是 JSON（%s %s）：%s", path, body, rec.Body.String())
		}
	}
	return rec.Code, parsed
}

func mailContactData(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("响应缺少 data 对象：%#v", body)
	}
	return data
}

func mailContactItems(t *testing.T, data map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := data["items"].([]interface{})
	if !ok {
		t.Fatalf("响应缺少 items 数组：%#v", data)
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("items 元素不是对象：%#v", item)
		}
		out = append(out, obj)
	}
	return out
}

// injectMailContactWriteEntries 注入合成词条，结束时还原（i18n 缓存是进程级单例）。
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

// —— ① 严格形状 ——

func TestMailContactWriteRejectsMalformedShapes(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrInvalidParam: {"en-US": "BAD-SHAPE-EN", "zh-CN": "BAD-SHAPE-ZH"},
	})

	h := NewHandle(&mailContactWriteStub{})
	r := newMailContactWriteEngine(h)

	cases := []struct {
		name string
		path string
		body string
	}{
		{"save 未知字段", "/api/mail/contact/save", `{"email":"a@b.c","bogus":1}`},
		{"save 邮箱显式 null", "/api/mail/contact/save", `{"email":null}`},
		{"save 标签显式 null", "/api/mail/contact/save", `{"tags":null}`},
		{"save id 是字符串数字（两种形态不许表达同一个值）", "/api/mail/contact/save", `{"id":"5","email":"a@b.c"}`},
		{"save id 是负数", "/api/mail/contact/save", `{"id":-1}`},
		{"save 名字是数字", "/api/mail/contact/save", `{"name":42}`},
		{"save 顶层不是对象", "/api/mail/contact/save", `[]`},
		{"save 空体", "/api/mail/contact/save", ``},
		{"delete 两个都给", "/api/mail/contact/delete", `{"id":1,"ids":["1"]}`},
		{"delete 两个都不给", "/api/mail/contact/delete", `{}`},
		{"delete id 为 0", "/api/mail/contact/delete", `{"id":0}`},
		{"delete ids 是 null", "/api/mail/contact/delete", `{"ids":null}`},
		{"delete ids 不是数组", "/api/mail/contact/delete", `{"ids":"1"}`},
		{"delete ids 里混了 null", "/api/mail/contact/delete", `{"ids":["1",null]}`},
		{"delete ids 里混了数字", "/api/mail/contact/delete", `{"ids":[1]}`},
		{"delete 未知字段", "/api/mail/contact/delete", `{"id":1,"reason":"x"}`},
		{"tag ids 是 null", "/api/mail/contact/tag", `{"ids":null,"add":"vip"}`},
		{"tag ids 非数组", "/api/mail/contact/tag", `{"ids":{},"add":"vip"}`},
		{"tag add 显式 null", "/api/mail/contact/tag", `{"ids":["1"],"add":null}`},
		{"tag 未知字段", "/api/mail/contact/tag", `{"ids":["1"],"add":"vip","replace":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := postMailContactJSON(t, r, c.path, c.body, "en-US")
			if code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d（%#v）", code, body)
			}
			if msg, _ := body["message"].(string); msg != "BAD-SHAPE-EN" {
				t.Fatalf("形状错的文案应是受控句（按请求语言取词），实际 %q", msg)
			}
		})
	}
}

// —— ② 合法形状确实到 service，且操作人来自会话（不是请求体） ——

func TestMailContactSaveCreateAndUpdate(t *testing.T) {
	stub := &mailContactWriteStub{}
	r := newMailContactWriteEngine(NewHandle(stub))

	code, body := postMailContactJSON(t, r, "/api/mail/contact/save",
		`{"email":"a@b.c","name":"张三","tags":"vip, 华南 ,vip","status":"pending","consent_source":"官网表单"}`, "")
	if code != http.StatusOK {
		t.Fatalf("新建应 200，实际 %d（%#v）", code, body)
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
	if id, _ := mailContactData(t, body)["id"].(float64); id != 77 {
		t.Fatalf("回执里应带新建的 id，实际 %#v", body["data"])
	}

	code, body = postMailContactJSON(t, r, "/api/mail/contact/save", `{"id":5,"email":"a@b.c","name":"李四"}`, "")
	if code != http.StatusOK {
		t.Fatalf("编辑应 200，实际 %d（%#v）", code, body)
	}
	if len(stub.updated) != 1 || stub.updated[0].ID != 5 {
		t.Fatalf("id>0 应走编辑，实际 %#v", stub.updated)
	}
	if stub.updated[0].OperatorID != uint64(mailContactWriteUserID) {
		t.Fatalf("编辑的操作人也必须取自会话，实际 %d", stub.updated[0].OperatorID)
	}
}

// —— ③ 两种口径的逐条结果 ——

func TestMailContactDeleteSingleAndBulk(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrInvalidParam: {"zh-CN": "参数-非法", "en-US": "PARAM-INVALID-EN"},
	})

	stub := &mailContactWriteStub{}
	r := newMailContactWriteEngine(NewHandle(stub))

	code, body := postMailContactJSON(t, r, "/api/mail/contact/delete", `{"id":7}`, "")
	if code != http.StatusOK {
		t.Fatalf("单条删除应 200，实际 %d（%#v）", code, body)
	}
	if len(stub.deleted) != 1 || len(stub.deleted[0]) != 1 || stub.deleted[0][0] != 7 {
		t.Fatalf("单条删除应把 [7] 交给 service，实际 %#v", stub.deleted)
	}

	// 批量：去重保序（与 shell.BulkIDs / pkg/bulkids 同口径），非法 id 逐条跳过并给原因。
	code, body = postMailContactJSON(t, r, "/api/mail/contact/delete", `{"ids":["1","2","2","x","0"]}`, "en-US")
	if code != http.StatusOK {
		t.Fatalf("批量删除应 200，实际 %d（%#v）", code, body)
	}
	data := mailContactData(t, body)
	if done, _ := data["deleted"].(float64); done != 2 {
		t.Fatalf("成功条数应为 2，实际 %#v", data)
	}
	if skipped, _ := data["skipped"].(float64); skipped != 2 {
		t.Fatalf("非法 id（x / 0）应各计一次跳过，实际 %#v", data)
	}
	items := mailContactItems(t, data)
	if len(items) != 4 {
		t.Fatalf("去重后 4 条（1 / 2 / x / 0），实际 %d 条：%#v", len(items), items)
	}
	for _, item := range items {
		id, _ := item["id"].(string)
		switch id {
		case "1", "2":
			if deleted, _ := item["deleted"].(bool); !deleted {
				t.Fatalf("%s 应标 deleted=true（前端据此移除行），实际 %#v", id, item)
			}
		case "x", "0":
			if deleted, _ := item["deleted"].(bool); deleted {
				t.Fatalf("%s 不该标 deleted=true", id)
			}
			if msg, _ := item["message"].(string); msg != "PARAM-INVALID-EN" {
				t.Fatalf("%s 的失败原因应按请求语言取词，实际 %q", id, msg)
			}
		default:
			t.Fatalf("出现了未预期的 id：%q", id)
		}
	}
}

// TestMailContactDeleteMissingRowIsNotReportedAsSuccess 删 0 行不能报成功。
//
// 这是逐条调用的代价换来的真问题：service 返回的是**真实删除行数**，忽略它就会把
// 「本来就不存在的 id」算成成功（回执说「已删除 3 位」而实际删了 2 位），
// 运营据此以为整批干净了，刷新才发现不对。
func TestMailContactDeleteMissingRowIsNotReportedAsSuccess(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})
	r := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{deleteMiss: true}))

	code, body := postMailContactJSON(t, r, "/api/mail/contact/delete", `{"ids":["9"]}`, "en-US")
	if code != http.StatusOK {
		t.Fatalf("删不存在的 id 不是 400（幂等语义），实际 %d（%#v）", code, body)
	}
	data := mailContactData(t, body)
	if deleted, _ := data["deleted"].(float64); deleted != 0 {
		t.Fatalf("真实删除 0 行：deleted 必须是 0，实际 %#v", data)
	}
	if skipped, _ := data["skipped"].(float64); skipped != 1 {
		t.Fatalf("0 行应计跳过，实际 %#v", data)
	}
	items := mailContactItems(t, data)
	if deleted, _ := items[0]["deleted"].(bool); deleted {
		t.Fatalf("这一条不许标 deleted=true（前端会把它从列表里移除）：%#v", items[0])
	}
	if msg, _ := items[0]["message"].(string); msg != "NOT-FOUND-EN" {
		t.Fatalf("跳过原因应是「联系人不存在」（当前语言），实际 %q", msg)
	}
}

// TestMailContactDeleteSingleMissingRowIsNotReportedAsSuccess 单条档删不存在的 id 也不能报成功。
//
// 批量档把 0 行计成「跳过 + 原因」；单条档没有「部分成功」这个 channel，所以整请求按
// 「这一条不存在」失败（400 + 联系人不存在，与 UpdateContact 及本模块其它删除端点同一口径）。
// 两条路说的是同一个事实：0 行不是删除成功，只是这一条本来就不在。
func TestMailContactDeleteSingleMissingRowIsNotReportedAsSuccess(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})
	r := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{deleteMiss: true}))

	code, body := postMailContactJSON(t, r, "/api/mail/contact/delete", `{"id":9}`, "en-US")
	if code != http.StatusBadRequest {
		t.Fatalf("单条删 0 行应 400（不许说「删除成功」），实际 %d（%#v）", code, body)
	}
	if msg, _ := body["message"].(string); msg != "NOT-FOUND-EN" {
		t.Fatalf("失败原因应是「联系人不存在」（按请求语言取词），实际 %q", msg)
	}

	// 对面必须仍然成立：真实删掉 1 行的单条删除照旧 200 成功。
	ok := newMailContactWriteEngine(NewHandle(&mailContactWriteStub{}))
	code, body = postMailContactJSON(t, ok, "/api/mail/contact/delete", `{"id":9}`, "en-US")
	if code != http.StatusOK {
		t.Fatalf("删掉 1 行的单条删除应 200，实际 %d（%#v）", code, body)
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
	raw, _ := json.Marshal(map[string]interface{}{"ids": ids})
	code, body := postMailContactJSON(t, r, "/api/mail/contact/delete", string(raw), "en-US")
	if code != http.StatusBadRequest {
		t.Fatalf("超过 %d 条应整批 400，实际 %d（%#v）", bulkids.MaxIDs, code, body)
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(msg, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("超限文案要同时说清上限与本次条数，实际 %q", msg)
	}
}

func TestMailContactTagResultShape(t *testing.T) {
	injectMailContactWriteEntries(t, map[string]map[string]string{
		mailenums.ErrContactTagEmpty: {"zh-CN": "差量-空", "en-US": "TAG-EMPTY-EN"},
		mailenums.ErrContactNotFound: {"zh-CN": "联系人-不存在", "en-US": "NOT-FOUND-EN"},
	})

	stub := &mailContactWriteStub{}
	r := newMailContactWriteEngine(NewHandle(stub))

	code, body := postMailContactJSON(t, r, "/api/mail/contact/tag", `{"ids":["1"],"add":"vip","remove":"trial"}`, "")
	if code != http.StatusOK {
		t.Fatalf("打标签应 200，实际 %d（%#v）", code, body)
	}
	if len(stub.tagged) != 1 || strings.Join(stub.tagged[0].Add, "|") != "vip" || strings.Join(stub.tagged[0].Remove, "|") != "trial" {
		t.Fatalf("增与减必须在同一条请求里转给 service，实际 %#v", stub.tagged)
	}
	data := mailContactData(t, body)
	if updated, _ := data["updated"].(float64); updated != 1 {
		t.Fatalf("非破坏动作的成功计数键是 updated，实际 %#v", data)
	}
	if _, exists := data["deleted"]; exists {
		t.Fatalf("打标签的摘要不得用 deleted 口径（联系人还在，说「已删除」是假话）：%#v", data)
	}
	items := mailContactItems(t, data)
	if len(items) != 1 {
		t.Fatalf("成功条目也要回（调用方要知道哪几条改好了），实际 %#v", items)
	}
	if up, _ := items[0]["updated"].(bool); !up {
		t.Fatalf("成功条目标 updated=true，实际 %#v", items[0])
	}
	if _, exists := items[0]["deleted"]; exists {
		t.Fatalf("成功条目不得带 deleted 键（前端会据此删行）：%#v", items[0])
	}

	// 增与减都为空：既不是成功也不是失败，直接说清。
	code, body = postMailContactJSON(t, r, "/api/mail/contact/tag", `{"ids":["1"],"add":"  ","remove":""}`, "en-US")
	if code != http.StatusBadRequest {
		t.Fatalf("空差量应 400，实际 %d（%#v）", code, body)
	}
	if msg, _ := body["message"].(string); msg != "TAG-EMPTY-EN" {
		t.Fatalf("空差量文案应按请求语言取词，实际 %q", msg)
	}

	// 该 id 不存在（service 既不报错也不改动）：逐条跳过 + 原因。
	missing := &mailContactWriteStub{tagMiss: true}
	code, body = postMailContactJSON(t, newMailContactWriteEngine(NewHandle(missing)),
		"/api/mail/contact/tag", `{"ids":["9"],"add":"vip"}`, "en-US")
	if code != http.StatusOK {
		t.Fatalf("不存在的 id 不该让整批失败，实际 %d（%#v）", code, body)
	}
	data = mailContactData(t, body)
	if skipped, _ := data["skipped"].(float64); skipped != 1 {
		t.Fatalf("不存在的 id 应计跳过，实际 %#v", data)
	}
	items = mailContactItems(t, data)
	if msg, _ := items[0]["message"].(string); msg != "NOT-FOUND-EN" {
		t.Fatalf("跳过原因应是「联系人不存在」（当前语言），实际 %q", msg)
	}
}

// —— ④ 端点是真的（注册处），页面 POST 是真的没了 ——

var (
	mailAPIContactPostRe  = regexp.MustCompile(`g\.POST\("(/contact/[a-z]+)",\s*permission\.(MailContact[A-Za-z]+)`)
	mailPageContactPostRe = regexp.MustCompile(`pages\.POST\("(/mail/contact[^"]*)"`)
)

func TestMailContactWriteEndpointsAreRealRoutes(t *testing.T) {
	raw, err := os.ReadFile("mail_router.go")
	if err != nil {
		t.Fatalf("读 mail_router.go 失败：%v", err)
	}
	found := map[string]string{}
	for _, m := range mailAPIContactPostRe.FindAllStringSubmatch(string(raw), -1) {
		found["/api/mail"+m[1]] = "permission." + m[2]
	}
	want := map[string]string{
		"/api/mail/contact/save":   "permission.MailContactSave",
		"/api/mail/contact/delete": "permission.MailContactDelete",
		"/api/mail/contact/tag":    "permission.MailContactTag",
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

	// 判权只按 (路径, 方法) 匹配：同一个对象被两个权限点声明就会互相串权。
	declareMailPageObjects()
	for path, permName := range want {
		var target permission.Perm
		switch permName {
		case "permission.MailContactSave":
			target = permission.MailContactSave
		case "permission.MailContactDelete":
			target = permission.MailContactDelete
		case "permission.MailContactTag":
			target = permission.MailContactTag
		}
		var matched []permission.Perm
		for _, p := range permission.All() {
			for _, r := range permission.RoutesOf(p) {
				if r.Path == path && r.Method == "POST" {
					matched = append(matched, p)
				}
			}
		}
		if len(matched) != 1 || matched[0] != target {
			t.Errorf("%s 应恰好反解出 %v，实际 %v", path, target, matched)
		}
	}
}

// TestMailContactPageWritePostsAreGone 四条联系人页面 POST 必须真的下线。
//
// 与 task-13 刻意**不写**的「必须不是真实路由」方向相反：那条断言当时会挡住新 API 上线，
// 而这里断言的是本批的交付物本身 —— 页面写操作已经由浏览器直调 JSON 端点，
// 页面 POST 留着就是第二套通道（权限、审核、错误语义都会分叉）。
// 仍留在页面 POST 的只有批量改状态（没有 JSON 端点）。
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
			t.Errorf("页面 POST %s 还在 —— 写动作已经由浏览器直调 JSON 端点，这条通道必须删掉", path)
		}
	}
	if !got["/mail/contacts/bulk-status"] {
		t.Errorf("批量改状态没有 JSON 端点，它的页面 POST 不该被一起删掉")
	}
}
