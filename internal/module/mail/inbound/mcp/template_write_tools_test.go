package mailmcp

// template_write_tools_test.go — 邮件模板工具的边界。
//
// 这一组要钉住的是**模板的身份是 (templateKey, locale) 这一对**，以及它带来的三件事：
// ① save 是 upsert（覆盖），所以 template_get 必须先把原文取回来 ——
//    不知道原文就改一个字，等于把剩下的正文全丢掉；
// ② delete 只删一个语言版本，不能把另一个也带走；
// ③ locale 没对上时必须说出来，否则模型会把英文版的内容当中文版改。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	maildto "go_wp/internal/module/mail/dto"
)

type stubTemplateStore struct {
	saveReq *maildto.SaveTemplateReq
	saveRes *maildto.TemplateItem

	delKey    string
	delLocale string

	listKey string
	list    []*maildto.TemplateItem
}

func (s *stubTemplateStore) UpsertTemplate(_ context.Context, req *maildto.SaveTemplateReq) (*maildto.TemplateItem, error) {
	s.saveReq = req
	return s.saveRes, nil
}

func (s *stubTemplateStore) DeleteTemplate(_ context.Context, key, locale string) error {
	s.delKey, s.delLocale = key, locale
	return nil
}

func (s *stubTemplateStore) ListTemplates(_ context.Context, key string) ([]*maildto.TemplateItem, error) {
	s.listKey = key
	return s.list, nil
}

func tplTool(t *testing.T, name string, s *stubTemplateStore) mcp.Tool {
	t.Helper()
	tools, err := TemplateTools(s, s)
	if err != nil {
		t.Fatalf("装配模板工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

// invokeTpl 调一个模板工具。withConfirm=false 用于**读工具** ——
// template_get 用 mcp.New 建，它的 schema 里没有 confirm / idempotencyKey，
// 多传一个键会被「未知参数」直接拒掉（写工具才由 NewWrite 自动补这两个键）。
func invokeTpl(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
	return invokeTplMode(t, tool, args, true)
}

func invokeTplRead(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
	return invokeTplMode(t, tool, args, false)
}

func invokeTplMode(t *testing.T, tool mcp.Tool, args map[string]any, withConfirm bool) (string, error) {
	t.Helper()
	full := map[string]any{}
	if withConfirm {
		full["confirm"] = true
		full["idempotencyKey"] = "k-" + t.Name()
	}
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

func TestTemplateRejectsNilDeps(t *testing.T) {
	if _, err := TemplateTools(nil, &stubTemplateStore{}); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
	if _, err := TemplateTools(&stubTemplateStore{}, nil); err == nil {
		t.Fatal("reader 为 nil 应报错")
	}
}

func TestTemplateToolNames(t *testing.T) {
	tools, err := TemplateTools(&stubTemplateStore{}, &stubTemplateStore{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("应有 3 个工具，实得 %d", len(tools))
	}
	for _, name := range []string{"template_get", "template_save", "template_delete"} {
		found := false
		for _, tool := range tools {
			if tool.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("缺工具 %q", name)
		}
	}
	// 「用模板发一封」不该存在：service 有 SendTemplate 但没有 HTTP 路由、
	// 没有权限点，硬凑相近的权限点等于用「能改模板」换「能发信」。
	for _, tool := range tools {
		if tool.Name() == "template_send" {
			t.Error("template_send 不该被注册（没有对应的权限点）")
		}
	}
}

// get 命中同语言版本时要报全文，并提示保存要整套重发。
func TestTemplateGetReturnsFullBody(t *testing.T) {
	s := &stubTemplateStore{list: []*maildto.TemplateItem{
		{ID: 1, TemplateKey: "welcome", Locale: "zh-CN", Name: "欢迎信", Subject: "欢迎 {{.name}}",
			BodyHTML: "<p>你好</p>", BodyText: "你好", Variables: []string{"name"}},
		{ID: 2, TemplateKey: "welcome", Locale: "en-US", Name: "Welcome", Subject: "Hi {{.name}}",
			BodyHTML: "<p>hi</p>"},
	}}
	text, err := invokeTplRead(t, tplTool(t, "template_get", s), map[string]any{"templateKey": "welcome"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	for _, want := range []string{"欢迎信", "zh-CN", "<p>你好</p>", "name", "整套重发"} {
		if !strings.Contains(text, want) {
			t.Errorf("全文应含 %q，实得：%s", want, text)
		}
	}
	if strings.Contains(text, "没有 zh-CN 版本") {
		t.Errorf("语言对上了不该出现回退提示：%s", text)
	}
}

// 语言没对上必须说出来 —— 否则模型会把英文版内容当成中文版改。
func TestTemplateGetWarnsOnLocaleFallback(t *testing.T) {
	s := &stubTemplateStore{list: []*maildto.TemplateItem{
		{ID: 2, TemplateKey: "welcome", Locale: "en-US", Name: "Welcome", Subject: "Hi", BodyHTML: "<p>hi</p>"},
	}}
	text, err := invokeTplRead(t, tplTool(t, "template_get", s), map[string]any{"templateKey": "welcome"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "没有 zh-CN 版本") {
		t.Errorf("回退到别的语言时必须显式说明，实得：%s", text)
	}
}

func TestTemplateGetGuidesWhenMissing(t *testing.T) {
	s := &stubTemplateStore{}
	text, err := invokeTplRead(t, tplTool(t, "template_get", s), map[string]any{"templateKey": "nope"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "mail_templates") {
		t.Errorf("找不到模板时应指向 mail_templates，实得：%s", text)
	}
}

// save 是覆盖写：回执要说清旧版本已被替换，以及要发信得走哪条路。
func TestTemplateSaveReportsOverwrite(t *testing.T) {
	s := &stubTemplateStore{saveRes: &maildto.TemplateItem{
		ID: 3, TemplateKey: "october_sale", Locale: "zh-CN", Name: "十月促销", BodyHTML: "<p>x</p>",
	}}
	text, err := invokeTpl(t, tplTool(t, "template_save", s), map[string]any{
		"templateKey": "october_sale", "name": "十月促销", "subject": "十月好货",
		"bodyHtml": "<p>x</p>", "variables": []string{"name"},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "已被替换") {
		t.Errorf("覆盖写必须说明旧版本被替换，实得：%s", text)
	}
	if !strings.Contains(text, "campaign") {
		t.Errorf("回执应指向能真正发信的路径，实得：%s", text)
	}
	if s.saveReq.Locale != "zh-CN" {
		t.Errorf("不传 locale 应落 zh-CN，实得 %q", s.saveReq.Locale)
	}
	if s.saveReq.OperatorID != 9 {
		t.Errorf("操作人应取自登录身份，实得 %d", s.saveReq.OperatorID)
	}
}

// save 的描述必须写明「先 get 再整体覆盖」这条操作纪律。
func TestTemplateSaveDescriptionRequiresGetFirst(t *testing.T) {
	desc := tplTool(t, "template_save", &stubTemplateStore{}).Description()
	if !strings.Contains(desc, "template_get") {
		t.Errorf("描述必须要求先取回全文：\n%s", desc)
	}
	if !strings.Contains(desc, "覆盖") {
		t.Errorf("描述必须点明这是覆盖写：\n%s", desc)
	}
}

// delete 只删一个语言版本 —— 参数传的 locale 必须原样到达。
func TestTemplateDeleteTargetsOneLocale(t *testing.T) {
	s := &stubTemplateStore{}
	text, err := invokeTpl(t, tplTool(t, "template_delete", s), map[string]any{
		"templateKey": "welcome", "locale": "en-US",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.delKey != "welcome" || s.delLocale != "en-US" {
		t.Fatalf("删除参数不对: key=%q locale=%q", s.delKey, s.delLocale)
	}
	if !strings.Contains(text, "其它语言版本不受影响") {
		t.Errorf("回执应说明只删了这一版，实得：%s", text)
	}
}

func TestTemplateDeleteDefaultsToZhCN(t *testing.T) {
	s := &stubTemplateStore{}
	if _, err := invokeTpl(t, tplTool(t, "template_delete", s), map[string]any{"templateKey": "welcome"}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.delLocale != "zh-CN" {
		t.Errorf("不传 locale 应落 zh-CN，实得 %q", s.delLocale)
	}
}
