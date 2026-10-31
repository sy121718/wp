package feature

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/crypto"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const (
	testCipherSecret = "test-ai-cipher-secret"
	testPlainKey     = "sk-ai-test-plain-0123456789abcdef"
)

func applyProductionSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行生产种子失败：%v", err)
	}
}

func newAIProviderService(t *testing.T) (*aiservice.Service, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	applyProductionSchema(t, db)
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	svc.SetCipherSecret(testCipherSecret)
	return svc, db
}

func TestAIProviderLifecycleKeepsSecretCipherOnly(t *testing.T) {
	svc, db := newAIProviderService(t)
	ctx := context.Background()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "DeepSeek",
		DisplayName: "深度求索",
		APIKey:      testPlainKey,
	})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}
	if p.ProviderKey != "deepseek" {
		t.Errorf("provider_key 应归一为小写，实际 %q", p.ProviderKey)
	}
	if !p.HasAPIKey {
		t.Error("存了密钥，HasAPIKey 应为 true")
	}
	if p.Protocol != aienums.ProtocolOpenAIChatCompletions {
		t.Errorf("protocol 缺省应为 openai_chat_completions，实际 %q", p.Protocol)
	}
	if p.Status != aienums.StatusEnabled {
		t.Errorf("缺省状态应为启用，实际 %d", p.Status)
	}
	if p.Version != 1 {
		t.Errorf("新建版本号应为 1，实际 %d", p.Version)
	}

	var cipherText string
	if err := db.Raw("SELECT COALESCE(api_key_cipher, '') FROM ai_provider WHERE id = ?", p.ID).
		Scan(&cipherText).Error; err != nil {
		t.Fatalf("读密文失败：%v", err)
	}
	if cipherText == "" || cipherText == testPlainKey {
		t.Fatalf("落库不是密文：%q", cipherText)
	}
	plain, err := crypto.Decrypt(cipherText, testCipherSecret)
	if err != nil || plain != testPlainKey {
		t.Fatalf("密文解不回原文：plain=%q err=%v", plain, err)
	}

	list, err := svc.ListProviders(ctx)
	if err != nil {
		t.Fatalf("列表失败：%v", err)
	}
	blob, _ := json.Marshal(list)
	if strings.Contains(string(blob), testPlainKey) {
		t.Fatalf("列表响应里出现了密钥明文：%s", blob)
	}
	if len(list) != 1 || !list[0].HasAPIKey {
		t.Fatalf("列表应含一个已配置密钥的供应商：%+v", list)
	}

	upd, err := svc.SaveModels(ctx, &aidto.SaveModelsReq{
		ProviderID: p.ID,
		Version:    p.Version,
		Models: []aidto.ModelEntry{
			{ID: "deepseek-chat", DisplayName: "DeepSeek Chat", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: []string{aienums.InputTypeText}},
			{ID: "deepseek-reasoner"},
		},
	})
	if err != nil {
		t.Fatalf("保存模型目录失败：%v", err)
	}
	if len(upd.Models) != 2 {
		t.Fatalf("期望 2 个模型，实际 %d：%+v", len(upd.Models), upd.Models)
	}
	if upd.Models[1].DisplayName != "deepseek-reasoner" {
		t.Errorf("裸行显示名应回退 ID，实际 %q", upd.Models[1].DisplayName)
	}
	if upd.Version != p.Version+1 {
		t.Errorf("保存后版本号应 +1：%d → %d", p.Version, upd.Version)
	}

	_, err = svc.SaveModels(ctx, &aidto.SaveModelsReq{ProviderID: p.ID, Version: p.Version})
	if !errors.Is(err, aiservice.ErrVersionConflict) {
		t.Fatalf("旧版本号应报版本冲突，实际 %v", err)
	}

	defaults, ok := aiservice.BuiltinModels("deepseek")
	if !ok {
		t.Fatal("deepseek 应有内置默认模型清单")
	}
	restored, err := svc.RestoreDefaultModels(ctx, &aidto.ProviderActionReq{ProviderID: p.ID, Version: upd.Version})
	if err != nil {
		t.Fatalf("恢复默认模型失败：%v", err)
	}
	if len(restored.Models) != len(defaults) {
		t.Fatalf("恢复后应有 %d 个内置模型，实际 %d", len(defaults), len(restored.Models))
	}
	for i := range defaults {
		if restored.Models[i].ID != defaults[i].ID || restored.Models[i].ContextWindow != defaults[i].ContextWindow {
			t.Fatalf("第 %d 个内置模型不一致：%+v vs %+v", i, restored.Models[i], defaults[i])
		}
	}

	kept, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ID: p.ID, DisplayName: "深度求索（改名）", Version: restored.Version,
	})
	if err != nil {
		t.Fatalf("更新供应商失败：%v", err)
	}
	if !kept.HasAPIKey {
		t.Error("空 apiKey 不应清空已存密钥")
	}
	var keptCipher string
	_ = db.Raw("SELECT COALESCE(api_key_cipher, '') FROM ai_provider WHERE id = ?", p.ID).Scan(&keptCipher).Error
	if keptCipher != cipherText {
		t.Error("空 apiKey 不应改写密文")
	}

	if err := svc.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := svc.GetProvider(ctx, p.ID); !errors.Is(err, aiservice.ErrProviderNotFound) {
		t.Fatalf("删除后应报供应商不存在，实际 %v", err)
	}
	var count int64
	_ = db.Raw("SELECT COUNT(*) FROM ai_provider WHERE id = ?", p.ID).Scan(&count).Error
	if count != 0 {
		t.Errorf("删除后库里还有行：%d", count)
	}
}

func TestAIProviderKeyMustBeUnique(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	if _, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "openai", DisplayName: "OpenAI"}); err != nil {
		t.Fatalf("首次新建失败：%v", err)
	}
	_, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "OpenAI", DisplayName: "重复"})
	if err == nil || err.Error() != aienums.ErrProviderKeyExists {
		t.Fatalf("重复 provider_key 应报 %s，实际 %v", aienums.ErrProviderKeyExists, err)
	}
}

func TestAIProviderRejectsMissingFields(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  *aidto.SaveProviderReq
		want string
	}{
		{"缺显示名", &aidto.SaveProviderReq{ProviderKey: "openai"}, aienums.ErrDisplayNameRequired},
		{"缺 provider_key", &aidto.SaveProviderReq{DisplayName: "X"}, aienums.ErrProviderKeyRequired},
		{"协议不支持", &aidto.SaveProviderReq{ProviderKey: "x", DisplayName: "X", Protocol: "自创协议"}, aienums.ErrProtocolUnsupported},
	}
	for _, c := range cases {
		_, err := svc.SaveProvider(ctx, c.req)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s：期望 %s，实际 %v", c.name, c.want, err)
		}
	}

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "openai", DisplayName: "OpenAI"})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}
	if _, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ID: p.ID, DisplayName: "改名"}); err == nil || err.Error() != aienums.ErrVersionRequired {
		t.Errorf("缺版本号应报 %s，实际 %v", aienums.ErrVersionRequired, err)
	}
}

func TestAIProviderStatusToggleIdempotent(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()
	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "openai", DisplayName: "OpenAI"})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}

	same, err := svc.SetProviderStatus(ctx, &aidto.SetStatusReq{ID: p.ID, Status: aienums.StatusEnabled, Version: p.Version})
	if err != nil {
		t.Fatalf("幂等启停失败：%v", err)
	}
	if same.Version != p.Version {
		t.Errorf("已是启用态时不应推进版本号：%d → %d", p.Version, same.Version)
	}

	off, err := svc.SetProviderStatus(ctx, &aidto.SetStatusReq{ID: p.ID, Status: aienums.StatusDisabled, Version: p.Version})
	if err != nil {
		t.Fatalf("停用失败：%v", err)
	}
	if off.Status != aienums.StatusDisabled || off.Version != p.Version+1 {
		t.Errorf("真正变更状态时应推进版本号：%+v", off)
	}
}

func TestAIProviderStatusToleratesUnknownValue(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()
	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "openai", DisplayName: "OpenAI", Status: intPtr(7)})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}
	if p.Status != aienums.StatusDisabled {
		t.Errorf("非法状态值应归为停用，实际 %d", p.Status)
	}
}

func TestRestoreDefaultModelsWithoutBuiltinKeepsCatalog(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "acme-gateway", DisplayName: "Acme Gateway"})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}
	kept, err := svc.SaveModels(ctx, &aidto.SaveModelsReq{
		ProviderID: p.ID, Version: p.Version,
		Models: []aidto.ModelEntry{{ID: "手填模型"}},
	})
	if err != nil {
		t.Fatalf("保存模型失败：%v", err)
	}

	_, err = svc.RestoreDefaultModels(ctx, &aidto.ProviderActionReq{ProviderID: p.ID, Version: kept.Version})
	if !errors.Is(err, aiservice.ErrNoBuiltinModels) {
		t.Fatalf("无内置清单应报 %v，实际 %v", aiservice.ErrNoBuiltinModels, err)
	}
	after, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if len(after.Models) != 1 || after.Models[0].ID != "手填模型" {
		t.Errorf("报错时不该动目录：%+v", after.Models)
	}
	if after.Version != kept.Version {
		t.Errorf("报错时不该推进版本号：%d → %d", kept.Version, after.Version)
	}
}

func TestFetchAvailableModelsRejectsUnsafeEndpoint(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "自建网关", DisplayName: "自建网关", BaseURL: "http://127.0.0.1:9/v1",
	})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}
	_, err = svc.FetchAvailableModels(ctx, &aidto.ProviderActionReq{ProviderID: p.ID, Version: p.Version})
	if err == nil || err.Error() != aienums.ErrURLDenied {
		t.Fatalf("内网地址应被拒并回 %s，实际 %v", aienums.ErrURLDenied, err)
	}
	after, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if after.Version != p.Version || len(after.Models) != 0 {
		t.Errorf("拉取失败不该动库：%+v", after)
	}
}

func TestAIServiceRejectsMissingCipherSecret(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	applyProductionSchema(t, db)
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	_, err := svc.SaveProvider(context.Background(), &aidto.SaveProviderReq{
		ProviderKey: "openai", DisplayName: "OpenAI", APIKey: testPlainKey,
	})
	if !errors.Is(err, aiservice.ErrCipherUnavailable) {
		t.Fatalf("缺口令应报 %v，实际 %v", aiservice.ErrCipherUnavailable, err)
	}
}

func newAIProviderEngine(t *testing.T) (*gin.Engine, *aiservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	applyProductionSchema(t, db)
	svc := aiservice.NewService(aimodel.NewAIModel(db))
	svc.SetCipherSecret(testCipherSecret)

	gin.SetMode(gin.TestMode)
	e := gin.New()
	h := aihttp.NewHandle(svc)
	e.GET("/api/ai/provider/list", h.ListProviders)
	e.GET("/api/ai/provider/get", h.GetProvider)
	e.POST("/api/ai/provider/save", h.SaveProvider)
	e.POST("/api/ai/provider/delete", h.DeleteProvider)
	e.POST("/api/ai/provider/status", h.SetProviderStatus)
	e.GET("/api/ai/provider/models/list", h.ListModels)
	e.POST("/api/ai/provider/models/save", h.SaveModels)
	e.POST("/api/ai/provider/models/restore", h.RestoreDefaultModels)
	e.POST("/api/ai/provider/models/fetch", h.FetchAvailableModels)
	return e, svc
}

func postAIJSON(t *testing.T, e *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestAIProviderAPIErrorMapping(t *testing.T) {
	e, svc := newAIProviderEngine(t)
	ctx := context.Background()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "openrouter", DisplayName: "OpenRouter"})
	if err != nil {
		t.Fatalf("准备供应商失败：%v", err)
	}

	custom, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "acme-gateway", DisplayName: "Acme Gateway"})
	if err != nil {
		t.Fatalf("准备自定义供应商失败：%v", err)
	}

	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"缺显示名", "/api/ai/provider/save", `{"providerKey":"openai"}`, http.StatusBadRequest},
		{"缺 provider_key", "/api/ai/provider/save", `{"displayName":"X"}`, http.StatusBadRequest},
		{"非法 JSON", "/api/ai/provider/save", `{`, http.StatusBadRequest},
		{"重复 provider_key", "/api/ai/provider/save", `{"providerKey":"openrouter","displayName":"重复"}`, http.StatusBadRequest},
		{"启停缺 id", "/api/ai/provider/status", `{"status":1}`, http.StatusBadRequest},

		{"恢复内置清单的预设", "/api/ai/provider/models/restore", `{"providerId":` + itoa(p.ID) + `,"version":1}`, http.StatusOK},
		{"恢复无内置清单的自定义端点", "/api/ai/provider/models/restore", `{"providerId":` + itoa(custom.ID) + `,"version":1}`, http.StatusBadRequest},
		{"删除不存在", "/api/ai/provider/delete?id=999999", ``, http.StatusNotFound},
	}
	for _, c := range cases {
		w := postAIJSON(t, e, c.path, c.body)
		if w.Code != c.want {
			t.Errorf("%s：期望 %d，实际 %d（body=%s）", c.name, c.want, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ai/provider/get?id=999999", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("取不存在的供应商：期望 404，实际 %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/ai/provider/list", nil)
	w = httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列表：期望 200，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), testPlainKey) {
		t.Error("列表响应里出现了密钥明文")
	}
	if !strings.Contains(w.Body.String(), "openrouter") {
		t.Errorf("列表响应里应有供应商：%s", w.Body.String())
	}
}

func TestAISaveProviderAPIKeepsSecretOutOfResponse(t *testing.T) {
	e, _ := newAIProviderEngine(t)

	w := postAIJSON(t, e, "/api/ai/provider/save",
		`{"providerKey":"deepseek","displayName":"深度求索","apiKey":"`+testPlainKey+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("保存失败：%d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, testPlainKey) {
		t.Fatalf("响应体出现了密钥明文：%s", body)
	}
	if !strings.Contains(body, `"hasApiKey":true`) {
		t.Fatalf("响应体应只回是否已配置：%s", body)
	}
}

func intPtr(v int) *int { return &v }

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
