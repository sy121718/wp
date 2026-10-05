package mailmcp

// campaign_write_tools_test.go — 群发活动写工具的边界。
//
// 这一组里最要紧的两条不是参数校验，而是**回执要说人话**：
// ① 没给 targetTags 时，回执必须把「会发给所有已订阅的人」说出来 ——
//    漏填标签的后果是全站群发，而默认行为本身不报错；
// ② campaign_start 的描述里必须留着「先确认收件规模」这条硬要求 ——
//    它是这个工具唯一的人为刹车，被顺手改掉就没了。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	maildto "go_wp/internal/module/mail/dto"
)

type stubCampaignWriter struct {
	saveReq *maildto.SaveCampaignReq
	saveRes *maildto.CampaignItem
	saveErr error

	startReq *maildto.StartCampaignReq
	startRes *maildto.StartCampaignResp
	startErr error

	deleteID  uint64
	deleteErr error
}

func (s *stubCampaignWriter) SaveCampaign(_ context.Context, req *maildto.SaveCampaignReq) (*maildto.CampaignItem, error) {
	s.saveReq = req
	return s.saveRes, s.saveErr
}

func (s *stubCampaignWriter) StartCampaign(_ context.Context, req *maildto.StartCampaignReq) (*maildto.StartCampaignResp, error) {
	s.startReq = req
	return s.startRes, s.startErr
}

func (s *stubCampaignWriter) DeleteCampaign(_ context.Context, id uint64) error {
	s.deleteID = id
	return s.deleteErr
}

type stubMailQueryReader struct {
	accounts  []*maildto.AccountItem
	templates []*maildto.TemplateItem
}

func (s *stubMailQueryReader) ListContacts(_ context.Context, _ *maildto.ContactFilterReq) (*maildto.ContactListResp, error) {
	return &maildto.ContactListResp{}, nil
}

func (s *stubMailQueryReader) ListCampaigns(_ context.Context, _ *maildto.CampaignListReq) (*maildto.CampaignListResp, error) {
	return &maildto.CampaignListResp{}, nil
}

func (s *stubMailQueryReader) CampaignReport(_ context.Context, _ uint64, _, _ int) (*maildto.CampaignReport, error) {
	return &maildto.CampaignReport{}, nil
}

func (s *stubMailQueryReader) ListTemplates(_ context.Context, _ string) ([]*maildto.TemplateItem, error) {
	return s.templates, nil
}

func (s *stubMailQueryReader) ListAccounts(_ context.Context, _ string) ([]*maildto.AccountItem, error) {
	return s.accounts, nil
}

func campaignTool(t *testing.T, name string, w *stubCampaignWriter) mcp.Tool {
	t.Helper()
	tools, err := CampaignWriteTools(w)
	if err != nil {
		t.Fatalf("装配群发写工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func invokeCampaign(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
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

func TestCampaignWriteRejectsNilWriter(t *testing.T) {
	if _, err := CampaignWriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestMailSetupRejectsNilReader(t *testing.T) {
	if _, err := MailSetupTools(nil); err == nil {
		t.Fatal("reader 为 nil 应报错")
	}
}

func TestCampaignWriteToolNames(t *testing.T) {
	tools, err := CampaignWriteTools(&stubCampaignWriter{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("应有 3 个写工具，实得 %d", len(tools))
	}
	tools, err = MailSetupTools(&stubMailQueryReader{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("应有 2 个配置工具，实得 %d", len(tools))
	}
}

func TestCampaignSavePassesArgs(t *testing.T) {
	stub := &stubCampaignWriter{saveRes: &maildto.CampaignItem{ID: 7, Name: "十月新品通知", Subject: "新品到了"}}
	_, err := invokeCampaign(t, campaignTool(t, "campaign_save", stub), map[string]any{
		"name": "十月新品通知", "accountId": 2, "templateId": 3, "subject": "新品到了",
		"targetTags":    []string{"vip", " vip ", ""},
		"variablesJson": `{"name":"张三"}`,
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	req := stub.saveReq
	if req == nil {
		t.Fatal("service 未收到请求")
	}
	if req.AccountID != 2 || req.TemplateID != 3 {
		t.Errorf("账号 / 模板 id 传错: %+v", req)
	}
	// 标签要去空白、去空串（空标签会被当成「带空标签的人」，谁也匹配不上）。
	if len(req.TargetTags) != 1 || req.TargetTags[0] != "vip" {
		t.Errorf("标签清洗失败: %#v", req.TargetTags)
	}
	if req.Variables["name"] != "张三" {
		t.Errorf("模板变量没解析: %#v", req.Variables)
	}
	if req.OperatorID != 9 {
		t.Errorf("操作人应取自登录身份，实得 %d", req.OperatorID)
	}
}

// 没给标签 → 回执必须把后果说出来。这是本组最重要的一条。
func TestCampaignSavedTextWarnsOnEmptyTags(t *testing.T) {
	stub := &stubCampaignWriter{saveRes: &maildto.CampaignItem{ID: 7, Name: "十月新品通知"}}
	text, err := invokeCampaign(t, campaignTool(t, "campaign_save", stub), map[string]any{
		"name": "十月新品通知", "accountId": 2, "templateId": 3, "subject": "新品到了",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "所有已订阅") {
		t.Errorf("未指定标签时必须警告会群发全站，实得：%s", text)
	}
	if !strings.Contains(text, "草稿") {
		t.Errorf("回执必须说明这是草稿、还没发，实得：%s", text)
	}
}

func TestCampaignSavedTextShowsTagScope(t *testing.T) {
	stub := &stubCampaignWriter{saveRes: &maildto.CampaignItem{ID: 7, Name: "十月新品通知"}}
	text, err := invokeCampaign(t, campaignTool(t, "campaign_save", stub), map[string]any{
		"name": "十月新品通知", "accountId": 2, "templateId": 3, "subject": "新品到了",
		"targetTags": []string{"vip"},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if strings.Contains(text, "所有已订阅") {
		t.Errorf("指定了标签就不该出现全站群发警告：%s", text)
	}
	if !strings.Contains(text, "vip") {
		t.Errorf("回执应报出收件范围，实得：%s", text)
	}
}

// 变量 JSON 坏了要当场报错，不能静默当成「没有变量」。
func TestCampaignSaveRejectsBadVariablesJSON(t *testing.T) {
	stub := &stubCampaignWriter{}
	_, err := invokeCampaign(t, campaignTool(t, "campaign_save", stub), map[string]any{
		"name": "x", "accountId": 1, "templateId": 1, "subject": "y",
		"variablesJson": "{不是 JSON",
	})
	if err == nil {
		t.Fatal("非法 variablesJson 应报错")
	}
	if stub.saveReq != nil {
		t.Error("参数不合法时不应该调用 service")
	}
}

func TestCampaignSaveEmptyVariablesIsFine(t *testing.T) {
	stub := &stubCampaignWriter{saveRes: &maildto.CampaignItem{ID: 7}}
	if _, err := invokeCampaign(t, campaignTool(t, "campaign_save", stub), map[string]any{
		"name": "x", "accountId": 1, "templateId": 1, "subject": "y",
	}); err != nil {
		t.Fatalf("不传变量应可用: %v", err)
	}
	if stub.saveReq.Variables != nil {
		t.Errorf("空输入应落 nil，实得 %#v", stub.saveReq.Variables)
	}
}

func TestCampaignStartedTextReportsRecipients(t *testing.T) {
	stub := &stubCampaignWriter{startRes: &maildto.StartCampaignResp{CampaignID: 7, Total: 128, Queued: true}}
	text, err := invokeCampaign(t, campaignTool(t, "campaign_start", stub), map[string]any{"campaignId": 7})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.startReq == nil || stub.startReq.CampaignID != 7 {
		t.Fatalf("活动 id 没传到: %+v", stub.startReq)
	}
	if !strings.Contains(text, "128") {
		t.Errorf("回执应报出收件人数，实得：%s", text)
	}
	// 用户会立刻去数收件箱，所以要说明是排队发。
	if !strings.Contains(text, "逐批") {
		t.Errorf("回执应说明是逐批发送，实得：%s", text)
	}
}

// campaign_start 是本仓唯一会向站外真实收件人批量发信的动作，
// 描述里那句「先确认收件规模」是它唯一的人为刹车 —— 钉住它，别被顺手改掉。
func TestCampaignStartDescriptionKeepsConfirmRequirement(t *testing.T) {
	tool := campaignTool(t, "campaign_start", &stubCampaignWriter{})
	desc := tool.Description()
	for _, want := range []string{"收件规模", "确认"} {
		if !strings.Contains(desc, want) {
			t.Errorf("campaign_start 的描述丢了「%s」这条要求：\n%s", want, desc)
		}
	}
}

func TestCampaignDeleteTextSaysIrreversible(t *testing.T) {
	stub := &stubCampaignWriter{}
	text, err := invokeCampaign(t, campaignTool(t, "campaign_delete", stub), map[string]any{"campaignId": 7})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.deleteID != 7 {
		t.Errorf("活动 id 没传到: %d", stub.deleteID)
	}
	if !strings.Contains(text, "收不回") {
		t.Errorf("删除回执应说明已发出的信收不回，实得：%s", text)
	}
}

func TestAccountsTextGuidesWhenEmpty(t *testing.T) {
	q := &stubMailQueryReader{}
	tools, _ := MailSetupTools(q)
	var text string
	for _, tool := range tools {
		if tool.Name() == "mail_accounts" {
			raw, _ := json.Marshal(map[string]any{})
			res, err := tool.Invoke(context.Background(), raw)
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			text = res.Text
		}
	}
	if !strings.Contains(text, "后台") {
		t.Errorf("没有账号时应引导去后台配置，实得：%s", text)
	}
}

func TestTemplatesTextCarriesIDAndVariables(t *testing.T) {
	q := &stubMailQueryReader{templates: []*maildto.TemplateItem{
		{ID: 3, Name: "欢迎信", TemplateKey: "welcome", Locale: "zh-CN", Subject: "欢迎", Variables: []string{"name"}},
	}}
	tools, _ := MailSetupTools(q)
	var text string
	for _, tool := range tools {
		if tool.Name() == "mail_templates" {
			raw, _ := json.Marshal(map[string]any{})
			res, err := tool.Invoke(context.Background(), raw)
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			text = res.Text
		}
	}
	// id 必须出现：活动存的是 template_id，看不到它就建不了活动。
	if !strings.Contains(text, "id=3") {
		t.Errorf("模板清单必须带 id，实得：%s", text)
	}
	if !strings.Contains(text, "name") {
		t.Errorf("模板清单必须带变量名，实得：%s", text)
	}
}
