package productservice

import (
	"context"
	"errors"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
)

// translationProjectStub 站点语言桩：这一屏要的三件事（工程清单 / 启用语言 / 默认语言）。
//
// 工程清单给空 = 不选工程，读模型因此**不查商品域候选**（那条要走 model，本组用例没有库）——
// 本文件只钉不依赖库的那些判据；整屏取数与保存的端到端行为在 feature 测试里
// （public/test/product/feature/product_translation_test.go 的真实库链路）。
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

// TestProductTranslationsPageProjectListFailureIsAnError 工程清单读不到 = 这一页打不开
// （返回 error，调用方走提示页出口）。
//
// 与同模块其它屏的取舍不同：那些屏的工程清单读失败会渲染一档失败页（页面骨架还在、只是
// 数据没读到），而工作台**所有**内容都挂在选中工程上 —— 空工程的工作台是一张没有意义的表，
// 给失败页只会让人以为「这个工程没有可翻译的内容」。
func TestProductTranslationsPageProjectListFailureIsAnError(t *testing.T) {
	s := NewService(nil, translationProjectStub{listErr: errors.New("projects read failed")})
	view, err := s.ProductTranslationsPage(context.Background(), &productdto.ProductTranslationsPageReq{ProjectID: "p1"})
	if err == nil {
		t.Fatal("工程清单读不到时工作台必须报错（不能渲染成「没有可翻译内容」）")
	}
	if view != nil {
		t.Fatalf("报错时不返回视图（调用方不渲染本页），实际 %+v", view)
	}
}

// TestTargetTranslationLangsExcludesSource 目标语言 = 启用语言剔除源语言，且**保持清单原顺序**。
//
// 顺序是判据不是巧合：「默认语言在前」时第一个元素就是默认选中的目标语言，
// 换一次顺序（比如排个序）会让工作台默认落在另一种语言上。
//
// 反面判据：不剔源语言时「翻了个寂寞」——默认选中的是源语言，译文写回源语言那一列，
// 前台看到的还是原文，页面上却显示「已翻译」。
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

// TestSaveProductTranslationsRejectsLangOutsideEnabled 目标语言不在启用清单里 = 保存被拒、
// 页面重渲并列出结论（**不是**静默按空语言写库）。
func TestSaveProductTranslationsRejectsLangOutsideEnabled(t *testing.T) {
	s := NewService(nil, translationProjectStub{})
	view, err := s.SaveProductTranslations(context.Background(), &productdto.SaveProductTranslationsReq{
		Lang:     "de-DE",
		Contexts: []string{"product.name"}, Hashes: []string{"h1"}, Targets: []string{"Shirt"},
	})
	if err != nil {
		t.Fatalf("保存编排不应报错（结论走视图）: %v", err)
	}
	if !view.LangInvalid || view.Saved || len(view.Errors) != 1 {
		t.Fatalf("非法语言应给一条结论且不落库，实际 %+v", view)
	}
	if view.Errors[0].Msg.Key != productenums.MsgTranslationLangInvalid {
		t.Fatalf("结论应是语言不合法词条，实际 %q", view.Errors[0].Msg.Key)
	}
}

// TestSaveProductTranslationsParallelArraysMustAlign 三组并行数组长度不齐 = 整批拒绝。
//
// 判据是「不按较短的那个截断」：截断会让用户以为全部保存了，而尾部几行悄悄没写。
func TestSaveProductTranslationsParallelArraysMustAlign(t *testing.T) {
	s := NewService(nil, translationProjectStub{})
	view, err := s.SaveProductTranslations(context.Background(), &productdto.SaveProductTranslationsReq{
		Lang:     i18n.GetDefaultLang(),
		Contexts: []string{"product.name", "product.subtitle"},
		Hashes:   []string{"h1"},
		Targets:  []string{"Shirt"},
	})
	if err != nil {
		t.Fatalf("保存编排不应报错（结论走视图）: %v", err)
	}
	if view.Saved || len(view.Errors) != 1 {
		t.Fatalf("长度不齐应整批拒绝，实际 Saved=%v Errors=%+v", view.Saved, view.Errors)
	}
	if view.Errors[0].Msg.Key != productenums.MsgTranslationInvalid {
		t.Fatalf("结论应是表单提交不合法词条，实际 %q", view.Errors[0].Msg.Key)
	}
}

// TestSaveProductTranslationsNoRowsIsSavedWithoutWrite 一行都没提交（或全部空输入）=
// 保存结论成立、写入 0 条 —— 不能报错，也不能把「没有输入」当成失败。
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
