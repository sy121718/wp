package unit

import (
	"context"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
)

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

	langs, lerr := svc.EnabledLangs(ctx, projectID)
	if lerr != nil {
		t.Fatalf("读取启用语言失败: %v", lerr)
	}
	if len(langs) != 2 {
		t.Errorf("被拒绝的保存不应改动清单，实际 %v", langs)
	}

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
