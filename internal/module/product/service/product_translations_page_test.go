package productservice

import (
	"context"
	"errors"
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
)

type translationProjectStub struct {
	projectcontract.ProjectService
	rows       []projectdto.ProjectResp
	listErr    error
	langs      []string
	sourceLang string
}

func (s translationProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, s.listErr
}

func (s translationProjectStub) EnabledLangs(context.Context, string) ([]string, error) {
	return s.langs, nil
}

func (s translationProjectStub) DefaultLocale(context.Context, string) (string, error) {
	if s.sourceLang == "" {
		return "", errors.New("没有默认语言清单")
	}
	return s.sourceLang, nil
}

func TestProductTranslationsPageProjectListFailureIsAnError(t *testing.T) {
	s := NewService(nil, translationProjectStub{listErr: errors.New("projects read failed")})
	view, err := s.QueryProductTranslations(context.Background(), &productdto.ProductTranslationsPageReq{ProjectID: "p1"})
	if err == nil {
		t.Fatal("工程清单读不到时工作台必须报错（不能渲染成「没有可翻译内容」）")
	}
	if view != nil {
		t.Fatalf("报错时不返回视图（调用方不渲染本页），实际 %+v", view)
	}
}

func TestTargetTranslationLangsExcludesSource(t *testing.T) {
	cases := []struct {
		name   string
		langs  []string
		source string
		want   []string
	}{
		{"默认语言在前时剔掉源语言", []string{"zh-CN", "en-US", "ja-JP"}, "zh-CN", []string{"en-US", "ja-JP"}},
		{"源语言在中间也剔掉", []string{"en-US", "zh-CN", "ja-JP"}, "zh-CN", []string{"en-US", "ja-JP"}},
		{"只剩源语言", []string{"zh-CN"}, "zh-CN", []string{}},
		{"源语言不在清单里则全部保留", []string{"en-US"}, "zh-CN", []string{"en-US"}},
		{"源语言为空串时不动清单", []string{"zh-CN", "en-US"}, "", []string{"zh-CN", "en-US"}},
	}
	for _, c := range cases {
		got := targetTranslationLangs(c.langs, c.source)
		if len(got) != len(c.want) {
			t.Fatalf("%s：期望 %v，实际 %v", c.name, c.want, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s：期望 %v，实际 %v", c.name, c.want, got)
			}
		}
	}
}

func TestSaveProductTranslationsRejectsLangOutsideEnabled(t *testing.T) {
	s := NewService(nil, translationProjectStub{})
	view, err := s.SaveProductTranslations(context.Background(), &productdto.SaveProductTranslationsReq{
		Lang:     "de-DE",
		Contexts: []string{"product.name"}, Hashes: []string{"h1"}, Targets: []string{"Shirt"},
	})
	if err != nil {
		t.Fatalf("保存编排不应报错（结论走命令结果）: %v", err)
	}
	if view.Saved || len(view.Errors) != 1 {
		t.Fatalf("非法语言应给一条结论且不落库，实际 %+v", view)
	}
	if view.Errors[0].Msg.Key != productenums.MsgTranslationLangInvalid {
		t.Fatalf("结论应是语言不合法词条，实际 %q", view.Errors[0].Msg.Key)
	}
}

func TestSaveProductTranslationsParallelArraysMustAlign(t *testing.T) {
	s := NewService(nil, translationProjectStub{})
	view, err := s.SaveProductTranslations(context.Background(), &productdto.SaveProductTranslationsReq{
		Lang:     i18n.GetDefaultLang(),
		Contexts: []string{"product.name", "product.subtitle"},
		Hashes:   []string{"h1"},
		Targets:  []string{"Shirt"},
	})
	if err != nil {
		t.Fatalf("保存编排不应报错（结论走命令结果）: %v", err)
	}
	if view.Saved || len(view.Errors) != 1 {
		t.Fatalf("长度不齐应整批拒绝，实际 Saved=%v Errors=%+v", view.Saved, view.Errors)
	}
	if view.Errors[0].Msg.Key != productenums.MsgTranslationInvalid {
		t.Fatalf("结论应是表单提交不合法词条，实际 %q", view.Errors[0].Msg.Key)
	}
}

func TestSaveProductTranslationsNoRowsIsSavedWithoutWrite(t *testing.T) {
	s := NewService(nil, translationProjectStub{})
	view, err := s.SaveProductTranslations(context.Background(), &productdto.SaveProductTranslationsReq{
		Lang: i18n.GetDefaultLang(),
	})
	if err != nil {
		t.Fatalf("保存编排不应报错: %v", err)
	}
	if !view.Saved || view.Written != 0 || len(view.Errors) != 0 {
		t.Fatalf("无提交内容应给「已保存、写入 0 条」，实际 %+v", view)
	}
}

type failingTranslationInstances struct{ calls int }

func (s *failingTranslationInstances) MarkStaleByDependency(context.Context, string, string) ([]string, error) {
	s.calls++
	return nil, errors.New("instance invalidation unavailable")
}

func TestTranslationInstanceInvalidationFailureRemainsVisible(t *testing.T) {
	port := &failingTranslationInstances{}
	s := &Service{translationInstances: port}
	groups := []productcontract.TranslationEntity{{EntityType: productcontract.EntityTypeProduct, EntityID: "product-1", Rows: []productcontract.TranslationValue{{Context: "product.name", SourceHash: "hash-1"}}}}
	if !s.markTranslationInstancesStale(context.Background(), groups, []string{i18n.ContentIndexKey("hash-1", "product.name")}) || port.calls != 1 {
		t.Fatalf("实例失效失败必须进入保存结论，calls=%d", port.calls)
	}
}
