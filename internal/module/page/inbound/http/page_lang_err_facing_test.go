package pagehttp

import (
	"strings"
	"testing"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	view "go_wp/pkg/view"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

func TestPageLangSentinelsCarryTheirOwnFacingText(t *testing.T) {
	cases := []struct {
		name string
		err  error
		key  string
		text string
	}{
		{
			name: "不能排除默认语言",
			err:  pageservice.ErrCannotExcludeDefaultLang,
			key:  pageenums.ErrCannotExcludeDefaultLang,
			text: "不能排除站点默认语言：它的产物承载 x-default，且「默认语言无前缀」的路径映射以它为锚点",
		},
		{
			name: "该语言已被排除",
			err:  pageservice.ErrPageLangExcluded,
			key:  pageenums.ErrPageLangExcluded,
			text: "该语言已被本页排除",
		},
		{
			name: "重复排除",
			err:  pageservice.ErrLangAlreadyExcluded,
			key:  pageenums.ErrLangAlreadyExcluded,
			text: "该语言已被本页排除，无需重复操作",
		},
		{
			name: "未排除无从恢复",
			err:  pageservice.ErrLangNotExcluded,
			key:  pageenums.ErrLangNotExcluded,
			text: "该语言未被本页排除，无从恢复",
		},
	}

	// 取词收口后业务错误的 Fallback 恒为空串，出口文案只能来自词库：按同一份 cases 注入，
	// 别在断言里写死中文当第二真源。
	dict := make(map[string]map[string]string, len(cases))
	for _, tc := range cases {
		dict[tc.key] = map[string]string{"zh-CN": tc.text}
	}
	i18n.InjectForTest(dict, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := pageErrCtx(t, "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			fact := view.PageErrorFactQuiet(c, pageErrScene, tc.err)
			if !fact.Business {
				t.Fatalf("%v 没被判定成业务错误", tc.err)
			}

			msg := pageErrMsg(c, tc.err)
			if msg.Key != tc.key {
				t.Errorf("Key = %q，want %q", msg.Key, tc.key)
			}
			// facing.FactOf 先走 selfFacingFact：facing.New(key) 的 Facing() 给 (key, "")，
			// 所以业务错误的 Fallback 恒为空串 —— 文案归词库，不回到声明处。
			// 别为了迁就这条断言把中文写回 enums，那是 AGENTS 点名的「自造界面词条」。
			if msg.Fallback != "" {
				t.Errorf("业务错误的 Fallback 应为空串（文案归词库），实际 %q", msg.Fallback)
			}

			if text := response.MsgText(c, msg); !strings.Contains(text, tc.text) {
				t.Errorf("出口文案 = %q，不含 %q", text, tc.text)
			}
		})
	}
}
