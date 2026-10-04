package aiservice

// ai_model_catalog_test.go — 模型目录的纯逻辑（不碰库、不装配）。
//
// 这些助手是 config_data 与界面之间的**唯一**翻译层：JSONB 是自由格式，管理员能在
// 页面上填任何东西，所以「解析失败回退、坏行跳过、重复 ID 报错」这几条必须是确定的。
// 放在包内是因为它们不导出 —— 它们是实现细节，只有 inbound 之外的同包测试能钉住。

import (
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

// TestParseModelsFallsBackOnBadShape 任何非「数组套对象」的形状都回退空目录，不 panic。
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

// TestParseModelsSkipsBadRowsKeepsGoodOnes 坏行跳过、好行留下，坏行不影响邻居。
func TestParseModelsSkipsBadRowsKeepsGoodOnes(t *testing.T) {
	cfg := aimodel.JSONMap{"models": []any{
		"不是对象",
		map[string]any{"id": ""},              // 缺 id → 丢
		map[string]any{},                      // 无字段 → 丢
		map[string]any{"id": "deepseek-chat"}, // 好行：显示名回退 ID
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
	// 输入类型白名单：bogus 被丢、大小写归一、text 在前。
	want := []string{aienums.InputTypeText, aienums.InputTypeImage}
	if len(got[1].InputTypes) != 2 || got[1].InputTypes[0] != want[0] || got[1].InputTypes[1] != want[1] {
		t.Errorf("输入类型归一错：%v", got[1].InputTypes)
	}
}

// TestParseModelsEmptyInputTypesFallback 缺 input_types 的行回退 [text]。
func TestParseModelsEmptyInputTypesFallback(t *testing.T) {
	got := parseModels(aimodel.JSONMap{"models": []any{map[string]any{"id": "m1"}}})
	if len(got) != 1 || len(got[0].InputTypes) != 1 || got[0].InputTypes[0] != aienums.InputTypeText {
		t.Fatalf("空输入类型应回退 [text]，实际 %+v", got)
	}
}

// TestModelsToConfigKeepsSiblingKeys 写回 models 时不动 config_data 的其它键。
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
	// 原 map 不被改写（避免调用方共享 map 时产生意外）。
	if _, mutated := cfg["models"]; mutated {
		t.Errorf("输入 map 被就地改写：%+v", cfg)
	}
}

// TestNormalizeModelsDropsBlankRowsKeepsValid 空行丢弃（不算错），DisplayName 回退 ID，负数归零。
func TestNormalizeModelsDropsBlankRowsKeepsValid(t *testing.T) {
	out, err := normalizeModels([]aidto.ModelEntry{
		{ID: "   "}, // 点「+ 添加模型」没填 → 丢弃
		{ID: "", DisplayName: "空 ID 但有名字"}, // 一样丢
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

// TestNormalizeModelsRejectsDuplicateID 两个行填同一个 ID 必须报错（静默留一行会骗用户）。
func TestNormalizeModelsRejectsDuplicateID(t *testing.T) {
	_, err := normalizeModels([]aidto.ModelEntry{{ID: "m1"}, {ID: " m1 "}})
	if err == nil {
		t.Fatal("重复 ID 应报错")
	}
	// sentinel 是 errors.New(enums key)：比较文本，不用 errors.Is。
	if err.Error() != aienums.ErrModelIDDuplicated {
		t.Errorf("错误应是 %s，实际 %v", aienums.ErrModelIDDuplicated, err)
	}
}

// TestNormalizeModelsEmptyIsLegal 保存空目录是合法操作（用户可以把目录清空）。
func TestNormalizeModelsEmptyIsLegal(t *testing.T) {
	out, err := normalizeModels(nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("空目录应成功且为空：out=%+v err=%v", out, err)
	}
}

// TestNormalizeInputTypes 白名单：只认 text / image，text 固定在前，未知值丢弃。
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

// TestMergeCatalogPreservesExistingRows 拉取只加新 ID：已存在行的元数据一个字节都不动。
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

// TestMergeCatalogExistingDuplicatesCollapsed 库里的脏数据（重复 ID）在合并时收敛成一行。
func TestMergeCatalogExistingDuplicatesCollapsed(t *testing.T) {
	out, added := mergeCatalog([]aidto.ModelEntry{{ID: "m1"}, {ID: "m1"}}, nil)
	if len(out) != 1 || len(added) != 0 {
		t.Fatalf("期望收敛为 1 行 0 新增，实际 out=%+v added=%v", out, added)
	}
}

// TestBuiltinModelsReturnsCopy 内置表不可被调用方改写（页面渲染拿到的是副本）。
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

	// 大小写与空白归一。
	if _, ok := BuiltinModels("  OpenAI  "); !ok {
		t.Error("provider_key 应归一大小写与空白")
	}
}

// TestBuiltinModelsAbsentForProvidersWithoutList 未登记的供应商必须报 ok=false
// （「恢复默认模型」据此回错，而不是静默清空）。
func TestBuiltinModelsAbsentForProvidersWithoutList(t *testing.T) {
	if _, ok := BuiltinModels("完全不存在的供应商"); ok {
		t.Error("未登记供应商应 ok=false")
	}
	if BuiltinBaseURL("完全不存在的供应商") != "" {
		t.Error("未登记供应商的默认地址应为空")
	}
	// openrouter 过去是「登记了地址、没有模型清单」的孤例，随 41 家预设导入后这个形态消失；
	// 这里改为钉住「它现在两者都有」——若哪天预设表漏了 openrouter，
	// 「恢复默认模型」会回 ErrNoBuiltinModels，而这条断言会先失败。
	models, ok := BuiltinModels("openrouter")
	if !ok || len(models) == 0 {
		t.Error("openrouter 已随预设导入，应有内置模型清单")
	}
	if BuiltinBaseURL("openrouter") == "" {
		t.Error("openrouter 登记了默认地址，应非空")
	}
}

// TestBuiltinProviderKeysSorted 页面提示用的键列表必须有序（否则每次渲染顺序抖动）。
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

// TestBuiltinPresetsComplete 预设表的看守：41 家都在、每家可选（有展示名与端点地址）、
// 且总数与本次导入的 dsh 快照一致。
//
// 41 / 1495 是**快照数字**：dsh 侧数据更新时这里会红，那是提醒「同步一次预设 + 更新这两个数字」，
// 而不是要把它改成宽松范围 —— 松了就看不出「某几家被静默丢掉」。
func TestBuiltinPresetsComplete(t *testing.T) {
	presets := BuiltinPresets()
	if len(presets) != 41 {
		t.Fatalf("预设家数应为 41，实际 %d", len(presets))
	}
	total := 0
	// dsh 侧本身没给地址的家：azure 的端点由部署（资源名 / 区域）决定，数据源里就是空串。
	// 其余家必须带默认地址 —— 空地址会把「跟随预设」变成用户手填，填错的代价是调用全失败。
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

// TestBuiltinPresetsWithoutProtocolAlias 协议在 dsh 侧无可映射的家必须留空 ——
// 静默回落成 openai_chat_completions 会让用户拿到「看起来配好了、调用必失败」的供应商。
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
	// 有映射的家反过来必须给出协议（否则「按提供商预设」这条兜底等于没有）。
	for _, key := range []string{"anthropic", "deepseek", "openai", "google", "openrouter"} {
		opt, _ := BuiltinPreset(key)
		if opt.Protocol == "" {
			t.Errorf("%s 应带内部协议枚举", key)
		}
	}
}
