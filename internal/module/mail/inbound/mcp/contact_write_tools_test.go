package mailmcp

// contact_write_tools_test.go — 联系人写工具的边界。
//
// 这一组的关键不在参数校验，而在**回执要把看不见的后果说出来**：
// 置 subscribed 或加标签会触发自动化、当场发信。用户说「给他打个 vip 标签」时
// 想的只是分类，信已经出去了。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

type stubContactWriter struct {
	createReq *maildto.SaveContactReq
	createID  uint64
	createErr error

	updateReq *maildto.SaveContactReq
	updateErr error

	statusReq *maildto.UpdateContactStatusReq
	statusErr error

	deleteReq *maildto.DeleteContactsReq
	deleteN   int64
	deleteErr error

	tagReq     *maildto.TagContactsReq
	tagChanged int
	tagSkipped int
	tagErr     error
}

func (s *stubContactWriter) CreateContact(_ context.Context, req *maildto.SaveContactReq) (uint64, error) {
	s.createReq = req
	return s.createID, s.createErr
}

func (s *stubContactWriter) UpdateContact(_ context.Context, req *maildto.SaveContactReq) error {
	s.updateReq = req
	return s.updateErr
}

func (s *stubContactWriter) UpdateContactStatus(_ context.Context, req *maildto.UpdateContactStatusReq) error {
	s.statusReq = req
	return s.statusErr
}

func (s *stubContactWriter) DeleteContacts(_ context.Context, req *maildto.DeleteContactsReq) (int64, error) {
	s.deleteReq = req
	return s.deleteN, s.deleteErr
}

func (s *stubContactWriter) TagContacts(_ context.Context, req *maildto.TagContactsReq) (int, int, error) {
	s.tagReq = req
	return s.tagChanged, s.tagSkipped, s.tagErr
}

type contactWriteStore struct{ m map[string]mcp.Result }

func (s *contactWriteStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	r, ok := s.m[tool+"|"+key]
	return r, ok, nil
}

func (s *contactWriteStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.m[tool+"|"+key] = res
	return nil
}

func mustContactWriteTools(t *testing.T, stub *stubContactWriter) map[string]mcp.Tool {
	t.Helper()
	tools, err := ContactWriteTools(stub)
	if err != nil {
		t.Fatalf("装配联系人写工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

func callContactWrite(t *testing.T, stub *stubContactWriter, name string, args map[string]any) (string, error) {
	t.Helper()
	tool, ok := mustContactWriteTools(t, stub)[name]
	if !ok {
		t.Fatalf("没有工具 %q", name)
	}
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), 9), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestContactWriteRejectsNilDependency(t *testing.T) {
	if _, err := ContactWriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestContactWriteToolsAreFive(t *testing.T) {
	tools := mustContactWriteTools(t, &stubContactWriter{})
	if len(tools) != 5 {
		t.Fatalf("应有 5 个写工具，实得 %d", len(tools))
	}
	for _, name := range []string{"contact_create", "contact_update", "contact_status", "contact_delete", "contact_tag"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("缺工具 %q", name)
		}
	}
}

// 操作人从 ctx 拿（与评论审核同一条口径）。
func TestContactWriteCarriesOperatorFromContext(t *testing.T) {
	stub := &stubContactWriter{createID: 7}
	if _, err := callContactWrite(t, stub, "contact_create", map[string]any{"email": "a@b.c"}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.createReq.OperatorID != 9 {
		t.Errorf("操作人应取自 context，实得 %d", stub.createReq.OperatorID)
	}
}

// 不传 status 时不能替用户决定「能发信」—— 回执必须点明他收不到。
func TestContactCreateTextExplainsPending(t *testing.T) {
	stub := &stubContactWriter{createID: 7}
	text, err := callContactWrite(t, stub, "contact_create", map[string]any{"email": "buyer@example.com"})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"待确认", "收不到"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
	// 空 status 要原样交给 service（由它落 pending），不能在工具层自己填死。
	if stub.createReq.Status != "" {
		t.Errorf("空 status 应原样传（service 落 pending），实得 %q", stub.createReq.Status)
	}
}

// 置 subscribed 会触发订阅类自动化，回执必须提醒 —— 用户以为只是改了个字段。
func TestContactCreateTextWarnsAutomation(t *testing.T) {
	stub := &stubContactWriter{createID: 7}
	text, err := callContactWrite(t, stub, "contact_create", map[string]any{
		"email": "buyer@example.com", "status": mailmodel.ContactStatusSubscribed,
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !strings.Contains(text, "自动化") {
		t.Errorf("置 subscribed 应提醒可能触发自动化发信：\n%s", text)
	}
}

func TestContactStatusTextWarnsAutomation(t *testing.T) {
	stub := &stubContactWriter{}
	text, err := callContactWrite(t, stub, "contact_status", map[string]any{
		"id": 3, "status": mailmodel.ContactStatusSubscribed, "note": "官网勾选",
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !strings.Contains(text, "自动化") {
		t.Errorf("正文缺自动化提醒：\n%s", text)
	}
	if stub.statusReq.Note != "官网勾选" {
		t.Errorf("note 应传下去（那是同意依据的留痕）: %+v", stub.statusReq)
	}
}

func TestContactStatusRejectsBadStatus(t *testing.T) {
	stub := &stubContactWriter{}
	if _, err := callContactWrite(t, stub, "contact_status", map[string]any{"id": 3, "status": "vip"}); err == nil {
		t.Fatal("非法状态应被拒")
	}
	if stub.statusReq != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

// 删联系人必须去重：重复 id 会让「实际删除数」对不上请求数。
func TestContactDeleteDedupesAndReports(t *testing.T) {
	stub := &stubContactWriter{deleteN: 2}
	text, err := callContactWrite(t, stub, "contact_delete", map[string]any{
		"ids": []uint64{3, 3, 4, 3},
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(stub.deleteReq.IDs) != 2 {
		t.Errorf("应按原顺序去重，实得 %v", stub.deleteReq.IDs)
	}
	for _, want := range []string{"已删除 2", "请求 2", "历史发信记录仍然保留"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestContactDeleteRejectsEmptyIDs(t *testing.T) {
	stub := &stubContactWriter{}
	if _, err := callContactWrite(t, stub, "contact_delete", map[string]any{"ids": []uint64{}}); err == nil {
		t.Fatal("空 ids 应被拒")
	}
}

// 打标签：add 与 remove 不能同时为空（一次什么都没改的写调用没有意义）。
func TestContactTagRejectsEmptyChange(t *testing.T) {
	stub := &stubContactWriter{}
	if _, err := callContactWrite(t, stub, "contact_tag", map[string]any{"ids": []uint64{3}}); err == nil {
		t.Fatal("add 与 remove 同时为空应被拒")
	}
	if stub.tagReq != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

func TestContactTagPassesBothDirections(t *testing.T) {
	stub := &stubContactWriter{}
	text, err := callContactWrite(t, stub, "contact_tag", map[string]any{
		"ids": []uint64{3, 3}, "add": []string{"vip", " vip "}, "remove": []string{"trial"},
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(stub.tagReq.Add) != 1 || stub.tagReq.Add[0] != "vip" {
		t.Errorf("标签应去空白去重: %+v", stub.tagReq.Add)
	}
	if len(stub.tagReq.Remove) != 1 || stub.tagReq.Remove[0] != "trial" {
		t.Errorf("去标签没传对: %+v", stub.tagReq.Remove)
	}
	if !strings.Contains(text, "自动化") {
		t.Errorf("加标签应提醒可能触发自动化：\n%s", text)
	}
}

func TestContactUpdatePassesTagsVerbatim(t *testing.T) {
	stub := &stubContactWriter{}
	if _, err := callContactWrite(t, stub, "contact_update", map[string]any{
		"id": 3, "email": "a@b.c", "tags": []string{"vip"},
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(stub.updateReq.Tags) != 1 || stub.updateReq.ID != 3 {
		t.Errorf("映射错了: %+v", stub.updateReq)
	}
}

func TestContactWritePropagatesError(t *testing.T) {
	stub := &stubContactWriter{createErr: errors.New("邮箱已存在")}
	if _, err := callContactWrite(t, stub, "contact_create", map[string]any{"email": "a@b.c"}); err == nil {
		t.Fatal("service 报错必须传出去")
	}
}

// 描述里必须写明 tags 是覆盖（contact_update）还是增删（contact_tag），
// 以及置 subscribed 会触发自动化 —— 三件事模型都推不出来。
func TestContactWriteDescriptionsStateKeySemantics(t *testing.T) {
	tools := mustContactWriteTools(t, &stubContactWriter{})
	if desc := tools["contact_update"].Description(); !strings.Contains(desc, "覆盖") {
		t.Errorf("contact_update 描述没说 tags 是覆盖：\n%s", desc)
	}
	if desc := tools["contact_tag"].Description(); !strings.Contains(desc, "自动化") {
		t.Errorf("contact_tag 描述没说会触发自动化：\n%s", desc)
	}
	if desc := tools["contact_delete"].Description(); !strings.Contains(desc, "退订") {
		t.Errorf("contact_delete 描述没引导「想别再发信应当用退订」：\n%s", desc)
	}
}
