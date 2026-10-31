package aiservice

import (
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

func TestParseModelsFallsBackOnBadShape(t *testing.T) {
	cases := []struct {
		name string
		cfg  aimodel.JSONMap
	}{
		{"空配置", nil},
		{"无 models 键", aimodel.JSONMap{"settings": map[string]any{"x": 1}}},
		{"models 是字符串", aimodel.JSONMap{"models": "deepseek-chat"}},
		{"models 是对象", aimodel.JSONMap{"models": map[string]any{"id": "x"}}},
		{"models 是 null", aimodel.JSONMap{"models": nil}},
	}
	for _, c := range cases {
		if got := parseModels(c.cfg); len(got) != 0 {
			t.Errorf("%s：期望空目录，实际 %d 行", c.name, len(got))
		}
	}
}

func TestParseModelsSkipsBadRowsKeepsGoodOnes(t *testing.T) {
	cfg := aimodel.JSONMap{"models": []any{
		"不是对象",
		map[string]any{"id": ""},
		map[string]any{},
		map[string]any{"id": "deepseek-chat"},
		map[string]any{"id": "gpt-x", "display_name": "GPT X", "context_window": float64(128000), "max_output_tokens": 4096, "input_types": []any{"TEXT", "bogus", "image"}},
	}}
	got := parseModels(cfg)
	if len(got) != 2 {
		t.Fatalf("期望 2 行好数据，实际 %d 行：%+v", len(got), got)
	}
	if got[0].ID != "deepseek-chat" || got[0].DisplayName != "deepseek-chat" {
		t.Errorf("显示名应回退 ID，实际 %+v", got[0])
	}
	if got[1].ContextWindow != 128000 || got[1].MaxOutputTokens != 4096 {
		t.Errorf("数值字段解析错：%+v", got[1])
	}

	want := []string{aienums.InputTypeText, aienums.InputTypeImage}
	if len(got[1].InputTypes) != 2 || got[1].InputTypes[0] != want[0] || got[1].InputTypes[1] != want[1] {
		t.Errorf("输入类型归一错：%v", got[1].InputTypes)
	}
}

func TestParseModelsEmptyInputTypesFallback(t *testing.T) {
	got := parseModels(aimodel.JSONMap{"models": []any{map[string]any{"id": "m1"}}})
	if len(got) != 1 || len(got[0].InputTypes) != 1 || got[0].InputTypes[0] != aienums.InputTypeText {
		t.Fatalf("空输入类型应回退 [text]，实际 %+v", got)
	}
}

func TestModelsToConfigKeepsSiblingKeys(t *testing.T) {
	cfg := aimodel.JSONMap{"settings": map[string]any{"timeout": 30}, "note": "手填"}
	out := modelsToConfig(cfg, []aidto.ModelEntry{{
		ID: "m1", DisplayName: "M1", ContextWindow: 1000, MaxOutputTokens: 100,
		InputTypes: []string{aienums.InputTypeText},
	}})
	if out["note"] != "手填" {
		t.Errorf("其它键被抹掉：%+v", out)
	}
	if _, ok := out["settings"]; !ok {
		t.Errorf("settings 键丢失：%+v", out)
	}
	rows, ok := out["models"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("models 形状错：%+v", out["models"])
	}
	if rows[0]["display_name"] != "M1" || rows[0]["context_window"] != int64(1000) {
		t.Errorf("落库键名/值错：%+v", rows[0])
	}

	if _, mutated := cfg["models"]; mutated {
		t.Errorf("输入 map 被就地改写：%+v", cfg)
	}
}

func TestNormalizeModelsDropsBlankRowsKeepsValid(t *testing.T) {
	out, err := normalizeModels([]aidto.ModelEntry{
		{ID: "   "},
		{ID: "", DisplayName: "空 ID 但有名字"},
		{ID: " m1 ", DisplayName: " M1 ", ContextWindow: -5, MaxOutputTokens: -1},
	})
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if len(out) != 1 {
		t.Fatalf("期望 1 行，实际 %d 行：%+v", len(out), out)
	}
	if out[0].ID != "m1" || out[0].DisplayName != "M1" {
		t.Errorf("trim 失效：%+v", out[0])
	}
	if out[0].ContextWindow != 0 || out[0].MaxOutputTokens != 0 {
		t.Errorf("负数应归零：%+v", out[0])
	}
}

func TestNormalizeModelsRejectsDuplicateID(t *testing.T) {
	_, err := normalizeModels([]aidto.ModelEntry{{ID: "m1"}, {ID: " m1 "}})
	if err == nil {
		t.Fatal("重复 ID 应报错")
	}

	if err.Error() != aienums.ErrModelIDDuplicated {
		t.Errorf("错误应是 %s，实际 %v", aienums.ErrModelIDDuplicated, err)
	}
}

func TestNormalizeModelsEmptyIsLegal(t *testing.T) {
	out, err := normalizeModels(nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("空目录应成功且为空：out=%+v err=%v", out, err)
	}
}

func TestNormalizeInputTypes(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, []string{aienums.InputTypeText}},
		{[]string{"bogus"}, []string{aienums.InputTypeText}},
		{[]string{"image"}, []string{aienums.InputTypeImage}},
		{[]string{"IMAGE", "image"}, []string{aienums.InputTypeImage}},
		{[]string{"image", "text"}, []string{aienums.InputTypeText, aienums.InputTypeImage}},
		{[]string{" text ", "text"}, []string{aienums.InputTypeText}},
	}
	for _, c := range cases {
		got := normalizeInputTypes(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("输入 %v：期望 %v，实际 %v", c.in, c.want, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("输入 %v：期望 %v，实际 %v", c.in, c.want, got)
			}
		}
	}
}

func TestMergeCatalogPreservesExistingRows(t *testing.T) {
	existing := []aidto.ModelEntry{{
		ID: "deepseek-chat", DisplayName: "我改过的名字", ContextWindow: 64000,
		MaxOutputTokens: 4096, InputTypes: []string{aienums.InputTypeText, aienums.InputTypeImage},
	}}
	out, added := mergeCatalog(existing, []string{"deepseek-chat", " deepseek-reasoner ", "", "deepseek-reasoner"})

	if len(out) != 2 {
		t.Fatalf("期望 2 行（1 保留 + 1 新增），实际 %d 行：%+v", len(out), out)
	}
	if out[0].DisplayName != "我改过的名字" || out[0].ContextWindow != 64000 {
		t.Errorf("已存在行的元数据被抹：%+v", out[0])
	}
	if out[1].ID != "deepseek-reasoner" || out[1].DisplayName != "deepseek-reasoner" {
		t.Errorf("新增行应是裸行（显示名 = ID）：%+v", out[1])
	}
	if len(added) != 1 || added[0] != "deepseek-reasoner" {
		t.Errorf("added 应只含真正的新 ID，实际 %v", added)
	}
}

func TestMergeCatalogExistingDuplicatesCollapsed(t *testing.T) {
	out, added := mergeCatalog([]aidto.ModelEntry{{ID: "m1"}, {ID: "m1"}}, nil)
	if len(out) != 1 || len(added) != 0 {
		t.Fatalf("期望收敛为 1 行 0 新增，实际 out=%+v added=%v", out, added)
	}
}

func TestBuiltinModelsReturnsCopy(t *testing.T) {
	models, ok := BuiltinModels("openai")
	if !ok || len(models) == 0 {
		t.Fatalf("openai 应有内置清单：ok=%v len=%d", ok, len(models))
	}
	models[0].ID = "被改坏了"
	if models[0].InputTypes != nil {
		models[0].InputTypes[0] = "被改坏了"
	}
	again, _ := BuiltinModels("openai")
	if again[0].ID == "被改坏了" {
		t.Fatal("内置表被调用方改写（未返回副本）")
	}

	if _, ok := BuiltinModels("  OpenAI  "); !ok {
		t.Error("provider_key 应归一大小写与空白")
	}
}

func TestBuiltinModelsAbsentForProvidersWithoutList(t *testing.T) {
	if _, ok := BuiltinModels("完全不存在的供应商"); ok {
		t.Error("未登记供应商应 ok=false")
	}
	if BuiltinBaseURL("完全不存在的供应商") != "" {
		t.Error("未登记供应商的默认地址应为空")
	}

	models, ok := BuiltinModels("openrouter")
	if !ok || len(models) == 0 {
		t.Error("openrouter 已随预设导入，应有内置模型清单")
	}
	if BuiltinBaseURL("openrouter") == "" {
		t.Error("openrouter 登记了默认地址，应非空")
	}
}

func TestBuiltinProviderKeysSorted(t *testing.T) {
	keys := BuiltinProviderKeys()
	if len(keys) < 7 {
		t.Fatalf("内置供应商数量异常：%v", keys)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("键列表未按字典序：%v", keys)
		}
	}
}

func TestBuiltinPresetsComplete(t *testing.T) {
	presets := BuiltinPresets()
	if len(presets) != 41 {
		t.Fatalf("预设家数应为 41，实际 %d", len(presets))
	}
	total := 0

	withoutBaseURL := map[string]bool{"azure-openai-responses": true}
	for _, p := range presets {
		if p.Key == "" || p.DisplayName == "" {
			t.Errorf("预设缺少 key 或展示名：%+v", p)
		}
		if p.ModelCount <= 0 {
			t.Errorf("%s 没有模型清单（选中后「恢复默认模型」会回错）：%+v", p.Key, p)
		}
		if p.BaseURL == "" && !withoutBaseURL[p.Key] {
			t.Errorf("%s 没有内置默认地址：%+v", p.Key, p)
		}
		if _, ok := BuiltinPreset("  " + strings.ToUpper(p.Key) + "  "); !ok {
			t.Errorf("%s 经大小写与空白归一时查不到", p.Key)
		}
		total += p.ModelCount
	}
	if total != 1495 {
		t.Errorf("预设模型总数应为 1495（dsh 快照），实际 %d", total)
	}
	if _, ok := BuiltinPreset("acme-gateway"); ok {
		t.Error("未登记的 provider_key 不应命中预设")
	}
}

func TestBuiltinPresetsWithoutProtocolAlias(t *testing.T) {
	for _, key := range []string{
		"amazon-bedrock", "azure-openai-responses", "google-vertex", "mistral", "openai-codex", "radius",
	} {
		opt, ok := BuiltinPreset(key)
		if !ok {
			t.Fatalf("%s 应在预设表里", key)
		}
		if opt.Protocol != "" {
			t.Errorf("%s 的协议没有内部枚举可映射，应为空，实际 %q", key, opt.Protocol)
		}
	}

	for _, key := range []string{"anthropic", "deepseek", "openai", "google", "openrouter"} {
		opt, _ := BuiltinPreset(key)
		if opt.Protocol == "" {
			t.Errorf("%s 应带内部协议枚举", key)
		}
	}
}
