package pagehttp

// page_lang_err_facing_test.go — 语言排除 / 恢复四条哨兵的**对外文案跟着错误声明走**。
//
// 这四条原先靠 inbound/http 的 pageLangErrMsg switch 给中文兜底；那个 switch 已删除，
// 兜底搬到了 pkg/facing（service 包声明错误的地方，见 page_lang.go 的哨兵块）。
//
// 为什么要专门钉一条：删 switch 之后**没有任何东西会红** —— 出口仍然给出 key，
// 取词也对，唯一的差别是 fallback 从「一句中文」退化成「key 字面量」，
// 而那只在「词条没 seed / 新装库 / 英文界面缺词条」时才看得见（页面上显示
// `page.err.cannot_exclude_default_lang`）。这正是本轮要消掉的二开损耗。
//
// 三条都红得起来：
//   - 把某个哨兵改回 errors.New(key) → Fallback 变成 key，第 ② 条红；
//   - 声明处的中文被改字 → 第 ② 条红（文案是承诺，不是实现细节）；
//   - 出口换回「按 key 形态猜兜底」→ 第 ② 条红。

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
			// ① 出口认得出它是业务错误（否则用户看到的是「系统内部错误」）。
			fact := adminui.PageErrorFactQuiet(c, pageErrScene, tc.err)
			if !fact.Business {
				t.Fatalf("%v 没被判定成业务错误", tc.err)
			}
			// ② key 与中文兜底都来自声明处。
			msg := pageErrMsg(c, tc.err)
			if msg.Key != tc.key {
				t.Errorf("Key = %q，want %q", msg.Key, tc.key)
			}
			if msg.Fallback != tc.text {
				t.Errorf("Fallback = %q\n want %q\n兜底没跟着错误走：词条缺失时页面会显示 key 字面量",
					msg.Fallback, tc.text)
			}
			// ③ 出口拼出的整句里含那句话（测试环境没有词条表 → 取词回落兜底）。
			if text := adminui.MsgText(c, msg); !strings.Contains(text, tc.text) {
				t.Errorf("出口文案 = %q，不含 %q", text, tc.text)
			}
		})
	}
}
