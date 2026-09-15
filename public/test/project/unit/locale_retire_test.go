package unit

// locale_retire_test.go — 禁用语言的确认与下线（审计 I18N-017）。
//
// 这条 finding 的现象是「语言禁用了，但 /en/… 还在访问面上服务，且没有任何入口能下掉它」。
// 测试钉住的是两个动作的**边界**：未确认时绝不真下线（否则等于把不可逆操作变成默认可行），
// 确认后才把该语言交给下线端口。

import (
	"context"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
)

// stubRetire 语言下线端口的测试替身：记录调用并回报预设的影响面。
type stubRetire struct {
	impact  map[string]int
	retired []string
}

func (s *stubRetire) LocaleRetireImpact(_ context.Context, _, lang string) (int, error) {
	return s.impact[lang], nil
}

func (s *stubRetire) RetireLocale(_ context.Context, _, lang string) (int, error) {
	s.retired = append(s.retired, lang)
	return s.impact[lang], nil
}

func TestSaveLocalesRequiresConfirmBeforeRetiring(t *testing.T) {
	svc, projectID := newLocaleService(t)
	ctx := context.Background()

	// 初始清单：zh-CN（默认）+ en-US 都启用。此时端口还没注入，保存不会触发下线逻辑。
	if _, err := svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", SortOrder: 1},
			{Lang: "zh-CN", IsDefault: true},
		},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}

	stub := &stubRetire{impact: map[string]int{"en-US": 3}}
	svc.SetLocaleRetirePort(stub)

	// 未确认：拒绝保存，并把受影响路径数回报给运营。
	_, err := svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}},
	})
	if err == nil {
		t.Fatal("禁用语言未确认时应拒绝保存")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("错误里应报出受影响路径数（运营据此判断代价），实际 %q", err.Error())
	}
	if len(stub.retired) != 0 {
		t.Errorf("未确认时不该真的下线任何路由，实际 %v", stub.retired)
	}
	// 清单也不能变：拒绝必须是**整体拒绝**，否则运营会以为「没生效」而反复尝试。
	langs, lerr := svc.EnabledLangs(ctx, projectID)
	if lerr != nil {
		t.Fatalf("读取启用语言失败: %v", lerr)
	}
	if len(langs) != 2 {
		t.Errorf("被拒绝的保存不应改动清单，实际 %v", langs)
	}

	// 确认后：清单落库，该语言交给下线端口。
	if _, err = svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID:     projectID,
		Locales:       []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}},
		ConfirmRetire: true,
	}); err != nil {
		t.Fatalf("确认后应保存成功: %v", err)
	}
	if len(stub.retired) != 1 || stub.retired[0] != "en-US" {
		t.Errorf("应把 en-US 交给下线端口，实际 %v", stub.retired)
	}

	// 影响面为 0 的语言不打扰运营：直接保存成功，且不该被送进下线端口。
	if _, err = svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "ja-JP", Enabled: boolPtr(false)},
			{Lang: "zh-CN", IsDefault: true},
		},
	}); err != nil {
		t.Fatalf("影响面为 0 时不应要求确认: %v", err)
	}
}
