package feature

import (
	"context"
	"errors"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
)

// ===== 缺陷 1：预设新建留空显示名 / 协议 =====
//
// 页面在预设表单里承诺「留空使用提供商默认名称」「按提供商预设」，归一只在 service 做。
// 用例显式点名预设（deepseek 有标准协议、amazon-bedrock 没有），不靠 presets[0] ——
// 目录是排序过的，取第一条等于让测试随目录内容漂移。

const (
	presetWithProtocol = "deepseek"       // 预设协议 openai_chat_completions
	presetNoProtocol   = "amazon-bedrock" // 预设协议为空，必须被 IsSupportedProtocol 拒
)

// 命中带协议的预设时，留空的名字与协议都回退到预设值。
func TestSaveProviderFallsBackToPresetDefaults(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	opt, ok := aiservice.BuiltinPreset(presetWithProtocol)
	if !ok {
		t.Fatalf("内置目录里没有 %s 预设", presetWithProtocol)
	}
	if opt.Protocol == "" {
		t.Fatalf("%s 预设协议不应为空（本用例前提）", presetWithProtocol)
	}

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: presetWithProtocol,
		// DisplayName 与 Protocol 都留空：正是浏览器里提交的形态。
		APIKey: testPlainKey,
	})
	if err != nil {
		t.Fatalf("预设留空显示名应成功，实际失败：%v", err)
	}
	if p.DisplayName != opt.DisplayName {
		t.Errorf("显示名应回退到预设名 %q，实际 %q", opt.DisplayName, p.DisplayName)
	}
	if p.Protocol != opt.Protocol {
		t.Errorf("协议应回退到预设协议 %q，实际 %q", opt.Protocol, p.Protocol)
	}
	if p.BaseURL != opt.BaseURL {
		t.Errorf("baseUrl 应回退到预设地址 %q，实际 %q", opt.BaseURL, p.BaseURL)
	}
}

// 预设协议为空时保持空，由 IsSupportedProtocol 拒 —— 不静默改成 openai。
// 静默改协议会让用户以为存的是 bedrock 的协议，实际存了另一种。
func TestSaveProviderKeepsEmptyPresetProtocolAndRejects(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	opt, ok := aiservice.BuiltinPreset(presetNoProtocol)
	if !ok {
		t.Fatalf("内置目录里没有 %s 预设", presetNoProtocol)
	}
	if opt.Protocol != "" {
		t.Fatalf("%s 预设协议应为空（本用例前提），实际 %q", presetNoProtocol, opt.Protocol)
	}

	_, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: presetNoProtocol,
		APIKey:      testPlainKey,
	})
	if err == nil {
		t.Fatal("预设协议为空时应被协议校验拒绝，实际通过了（说明被静默改成了别的协议）")
	}
	if !errors.Is(err, aienums.Err(aienums.ErrProtocolUnsupported)) {
		t.Fatalf("应回 ErrProtocolUnsupported，实际 %v", err)
	}
}

// 未知的自定义网关没有内置清单可依据：名称必须由用户给，不能凭空造一个。
func TestSaveProviderRequiresNameForUnknownKey(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	_, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "acme-unknown-gateway",
		APIKey:      testPlainKey,
	})
	if err == nil {
		t.Fatal("未知 providerKey 留空显示名应被拒，实际通过了")
	}
	if !errors.Is(err, aienums.Err(aienums.ErrDisplayNameRequired)) {
		t.Fatalf("应回 ErrDisplayNameRequired，实际 %v", err)
	}
}

// baseUrl 留空语义保留：自定义网关留空就是留空，不替它编地址。
func TestSaveProviderBaseURLStaysEmptyForCustomKey(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: "acme-blank-base",
		DisplayName: "Acme Blank",
		APIKey:      testPlainKey,
	})
	if err != nil {
		t.Fatalf("自定义网关留空 baseUrl 应可保存：%v", err)
	}
	if p.BaseURL != "" {
		t.Errorf("自定义网关 baseUrl 留空应保持空串，实际 %q", p.BaseURL)
	}
}

// 更新路径不因预设回退改写名字：留空名就是 400，DB 里的原值必须原样保留。
// 只断言「更新后不为空」是无效测试 —— 名字被预设名覆盖也满足它。
func TestSaveProviderUpdateRejectsEmptyNameAndKeepsStored(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	const original = "我自己的名字"
	created, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: presetWithProtocol,
		DisplayName: original,
		APIKey:      testPlainKey,
	})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}

	// 只清空名字，其余字段沿用已有值 —— 避免撞上 baseUrl 变更要求补 key 的分支。
	_, err = svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ID:          created.ID,
		ProviderKey: created.ProviderKey,
		DisplayName: "",
		BaseURL:     created.BaseURL,
		Protocol:    created.Protocol,
		Version:     created.Version,
		APIKey:      testPlainKey,
	})
	if err == nil {
		t.Fatal("更新留空显示名应被拒，实际通过了（说明被预设名静默覆盖）")
	}
	if !errors.Is(err, aienums.Err(aienums.ErrDisplayNameRequired)) {
		t.Fatalf("应回 ErrDisplayNameRequired，实际 %v", err)
	}

	after, gerr := svc.GetProvider(ctx, created.ID)
	if gerr != nil {
		t.Fatalf("回读失败：%v", gerr)
	}
	if after.DisplayName != original {
		t.Errorf("被拒的更新不得改动存储值：期望 %q，实际 %q", original, after.DisplayName)
	}
}

// 更新时给非空的自己名字，必须原样生效且不被预设名覆盖。
func TestSaveProviderUpdateKeepsCustomName(t *testing.T) {
	svc, _ := newAIProviderService(t)
	ctx := context.Background()

	created, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ProviderKey: presetWithProtocol,
		DisplayName: "初始名",
		APIKey:      testPlainKey,
	})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}

	const renamed = "改名后的自定义名"
	updated, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{
		ID:          created.ID,
		ProviderKey: created.ProviderKey,
		DisplayName: renamed,
		BaseURL:     created.BaseURL,
		Protocol:    created.Protocol,
		Version:     created.Version,
		APIKey:      testPlainKey,
	})
	if err != nil {
		t.Fatalf("更新失败：%v", err)
	}
	if updated.DisplayName != renamed {
		t.Errorf("自定义名应原样保存 %q，实际 %q", renamed, updated.DisplayName)
	}
}
