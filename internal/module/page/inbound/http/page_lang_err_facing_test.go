package pagehttp

import (
	"strings"
	"testing"

	"go_wp/internal/adminui"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
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

	c := pageErrCtx(t, "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			fact := adminui.PageErrorFactQuiet(c, pageErrScene, tc.err)
			if !fact.Business {
				t.Fatalf("%v 没被判定成业务错误", tc.err)
			}

			msg := pageErrMsg(c, tc.err)
			if msg.Key != tc.key {
				t.Errorf("Key = %q，want %q", msg.Key, tc.key)
			}
			if msg.Fallback != tc.text {
				t.Errorf("Fallback = %q\n want %q\n兜底没跟着错误走：词条缺失时页面会显示 key 字面量",
					msg.Fallback, tc.text)
			}

			if text := adminui.MsgText(c, msg); !strings.Contains(text, tc.text) {
				t.Errorf("出口文案 = %q，不含 %q", text, tc.text)
			}
		})
	}
}
