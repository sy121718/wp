package unit

import (
	json "go_wp/pkg/jsonx"
	"reflect"
	"regexp"
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/public/migrations"
)

func TestAIMigrationSeedRegistered(t *testing.T) {
	const version = "511-ai-provider"

	var found *migrations.Seed
	seeds := migrations.AllSeeds()
	for i := range seeds {
		if seeds[i].Version == version {
			found = &seeds[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("种子 %s 未注册（检查 public/migrations/register_ai.go 与 register.go 的调用）", version)
	}
	if found.TableName != "ai_provider" {
		t.Errorf("TableName 应为 ai_provider，实际 %q", found.TableName)
	}
	if !strings.Contains(found.ConditionSQL, "ai_provider") {
		t.Errorf("ConditionSQL 必须枚举本批对象（ai_provider），实际 %q", found.ConditionSQL)
	}

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS ai_provider",
		"CREATE INDEX IF NOT EXISTS",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Errorf("511 迁移缺少幂等语句 %q", want)
		}
	}

	if !regexp.MustCompile(`COMMENT ON TABLE\s+ai_provider\s+IS`).MatchString(found.SQL) {
		t.Error("511 迁移缺少 ai_provider 的表注释")
	}
	if !regexp.MustCompile(`COMMENT ON COLUMN\s+ai_provider\.config_data\s+IS`).MatchString(found.SQL) {
		t.Error("511 迁移缺少 config_data 的列注释")
	}

	for _, col := range []string{"provider_key", "display_name", "base_url", "protocol", "api_key_cipher", "status", "sort", "config_data", "version", "create_by", "update_by"} {
		if !strings.Contains(found.SQL, col) {
			t.Errorf("511 迁移缺少列 %q", col)
		}
	}
}

func TestAIFacingMessagesComplete(t *testing.T) {
	if len(aienums.FacingMessages) < 20 {
		t.Fatalf("文案白名单条目过少（%d），检查 enums 是否被裁剪", len(aienums.FacingMessages))
	}
	for key, want := range aienums.FacingMessages {
		if !strings.HasPrefix(key, "ai.") {
			t.Errorf("key 必须以 ai. 开头（i18n 命名空间）：%q", key)
		}
		got, ok := aienums.FacingText(key)
		if !ok {
			t.Errorf("%s 未登记进 FacingMessages", key)
		}
		if got != want || strings.TrimSpace(got) == "" {
			t.Errorf("%s 文案异常：%q", key, got)
		}
	}

	if _, ok := aienums.FacingText("ai.err.未登记的底层错误"); ok {
		t.Error("未登记 key 不该命中白名单")
	}
}

func TestAIProtocolOptionsDefaultFirst(t *testing.T) {
	if len(aienums.ProtocolOptions) == 0 {
		t.Fatal("协议下拉不能为空")
	}
	first := aienums.ProtocolOptions[0]
	if first.Value != aienums.ProtocolOpenAIChatCompletions || first.Label != "OpenAI Chat Completions" {
		t.Fatalf("首项应是 OpenAI Chat Completions，实际 %+v", first)
	}
	seen := make(map[string]bool, len(aienums.ProtocolOptions))
	for _, o := range aienums.ProtocolOptions {
		if o.Value == "" || o.Label == "" {
			t.Errorf("选项值/展示名不能为空：%+v", o)
		}
		if seen[o.Value] {
			t.Errorf("协议值重复：%q", o.Value)
		}
		seen[o.Value] = true
		if !aienums.IsSupportedProtocol(o.Value) {
			t.Errorf("下拉里的值必须通过白名单：%q", o.Value)
		}
		if aienums.ProtocolLabel(o.Value) != o.Label {
			t.Errorf("%q 的展示名不一致", o.Value)
		}
	}
	if aienums.IsSupportedProtocol("openai_compatible_自创") {
		t.Error("白名单必须拒绝未登记协议")
	}
	if aienums.IsSupportedProtocol("") {
		t.Error("空协议不是合法白名单值（缺省由 service 补）")
	}
	if got := aienums.ProtocolLabel("未知协议"); got != "未知协议" {
		t.Errorf("未知协议展示名应原样返回，实际 %q", got)
	}
}

func TestAIModelEntryJSONShape(t *testing.T) {
	entry := aidto.ModelEntry{
		ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash",
		ContextWindow: 1000000, MaxOutputTokens: 256000,
		InputTypes: []string{aienums.InputTypeText, aienums.InputTypeImage},
	}
	blob, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	for _, key := range []string{`"id"`, `"displayName"`, `"contextWindow"`, `"maxOutputTokens"`, `"inputTypes"`} {
		if !strings.Contains(string(blob), key) {
			t.Errorf("JSON 里缺少键 %s：%s", key, blob)
		}
	}
	var back aidto.ModelEntry
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if !reflect.DeepEqual(entry, back) {
		t.Errorf("往返不一致：%+v vs %+v", entry, back)
	}
}

func TestAIJSONMapValueScan(t *testing.T) {

	var nilMap aimodel.JSONMap
	v, err := nilMap.Value()
	if err != nil || v != nil {
		t.Fatalf("nil map 应写为 NULL：v=%v err=%v", v, err)
	}

	original := aimodel.JSONMap{"models": []any{map[string]any{"id": "m1"}}}
	raw, err := original.Value()
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("Value 应回 []byte，实际 %T", raw)
	}

	var back aimodel.JSONMap
	if err := back.Scan(bytes); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if _, ok := back["models"]; !ok {
		t.Errorf("往返丢了 models 键：%+v", back)
	}

	var fromNull aimodel.JSONMap = aimodel.JSONMap{"x": 1}
	if err := fromNull.Scan(nil); err != nil || fromNull != nil {
		t.Errorf("Scan(nil) 应回 nil：%+v err=%v", fromNull, err)
	}

	var fromEmpty aimodel.JSONMap
	if err := fromEmpty.Scan([]byte{}); err != nil || len(fromEmpty) != 0 {
		t.Errorf("Scan(空字节) 应回空 map：%+v err=%v", fromEmpty, err)
	}

	var wrongType aimodel.JSONMap
	if err := wrongType.Scan("不是字节切片"); err == nil {
		t.Error("Scan 非 []byte 应报错")
	}
}

func TestAIProviderResponseHasNoSecretField(t *testing.T) {

	forbidden := map[string]bool{
		"apikey": true, "apikeycipher": true, "cipher": true, "secret": true,
		"password": true, "token": true, "accesstoken": true, "refreshtoken": true,
	}

	assertNoForbidden := func(name string, typ reflect.Type, allowAPIKey bool) {
		t.Helper()
		for i := 0; i < typ.NumField(); i++ {
			field := strings.ToLower(typ.Field(i).Name)
			if !forbidden[field] {
				continue
			}
			if allowAPIKey && field == "apikey" {
				continue
			}
			t.Errorf("%s 不允许出现 %q 字段（密钥只存密文、只回布尔）", name, typ.Field(i).Name)
		}
	}

	assertNoForbidden("响应 Provider", reflect.TypeOf(aidto.Provider{}), false)
	assertNoForbidden("响应 ModelEntry", reflect.TypeOf(aidto.ModelEntry{}), false)
	assertNoForbidden("响应 FetchModelsResult", reflect.TypeOf(aidto.FetchModelsResult{}), false)

	assertNoForbidden("请求 SaveProviderReq", reflect.TypeOf(aidto.SaveProviderReq{}), true)

	if _, ok := reflect.TypeOf(aidto.Provider{}).FieldByName("HasAPIKey"); !ok {
		t.Error("响应 Provider 必须回 HasAPIKey（是否已配置）")
	}
}
