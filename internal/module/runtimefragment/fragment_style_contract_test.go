package runtimefragment

// fragment_style_contract_test.go — 片段能力 ↔ 片段基座样式表的对表断言（审计 UIK-005）。
//
// 为什么闭环断言的这一半放在本包：判断「这个片段该归哪个样式族」要看**片段模板**
// （哪个 .jet 消费哪些类名），而「系统里到底有哪些能力」只有注册表知道；builder 不能
// 反向 import 本包（依赖方向是 module → builder），于是分工：
//   · builder 侧（fragment_base_test.go）：两张表互斥、每个无样式能力都写了现状说明；
//   · 本文件：三张表（内置能力 / 有样式族 / 无样式清单）互相闭合。
//
// 新增片段能力却忘了登记样式归属时，这条立刻失败 —— 而不是等到「片段刷新出来是裸 HTML」
// 被人偶然发现（那正是 UIK-005 要消灭的隐式耦合）。

import (
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// builtinFragmentCapabilities 内置片段能力对照集（与各文件的 Register 调用一一对应）。
//
// 为什么要这份副本：注册表是**进程级可变**的（测试会在运行期注册探测能力），
// 直接拿 Types() 当「产品能力清单」会把测试残留一起算进来。对照集 + 两边断言
// （注册表 ⊆ 对照集 ∪ 非产品能力、对照集 ⊆ 注册表）既隔离了测试污染，
// 又能抓「新增了能力却没同步这份清单」。
var builtinFragmentCapabilities = []string{
	"accountPanel", "accountPasswordForm", "accountPreferenceForm", "accountProfileForm",
	"accountSessionsPanel",
	"bundleConfigurator", "bundleConfiguratorCheck",
	"cartAdd", "cartClear", "cartSetQty", "cartSummary", "cartView", "checkout",
	"forgotForm", "loginForm", "loginPanel", "orderDetail", "ordersList",
	"productList", "productLivePrice", "productVariantAvailability",
	"registerForm", "resetForm", "returnRequest", "searchResults",
}

// nonProductFragmentCapabilities 运行期注册的**非产品**能力 → 理由。
//
// 登记它们不是「放宽」：这些能力不参与产物注入（没有对应模板与样式诉求），
// 单独列出来是为了让「产品能力」与「测试脚手架」在清单上分得开。
var nonProductFragmentCapabilities = map[string]string{
	"sessionprobetestonly": "端到端测试在运行期注册的探测能力（endpoint_test.go 的认证策略用例），不参与产物注入，也不是产品能力",
	"langprobetestonly":    "端到端测试在运行期注册的探测能力（fragment_lang_test.go 的片段语言取词用例），不参与产物注入，也不是产品能力",
}

// TestFragmentStyleTablesCoverRegistry 三张表互相闭合。
func TestFragmentStyleTablesCoverRegistry(t *testing.T) {
	styled := map[string]bool{}
	for _, c := range builder.FragmentStyleCapabilities() {
		styled[strings.ToLower(c)] = true
	}
	unstyled := map[string]bool{}
	for c := range builder.FragmentUnstyledCapabilities() {
		unstyled[strings.ToLower(c)] = true
	}
	registered := map[string]bool{}
	for _, typ := range Types() {
		registered[strings.ToLower(typ)] = true
	}
	builtin := map[string]bool{}
	for _, typ := range builtinFragmentCapabilities {
		builtin[strings.ToLower(typ)] = true
	}
	if len(builtin) < 20 {
		t.Fatalf("内置能力对照集只有 %d 项（预期 ≥20）：对表断言可能在空转", len(builtin))
	}

	// ① 每个内置能力都要有样式归属（有族 / 无样式清单）。
	var uncovered []string
	for typ := range builtin {
		if !styled[typ] && !unstyled[typ] {
			uncovered = append(uncovered, typ)
		}
	}
	// ② 注册表里的能力要么是内置的、要么是登记过的非产品能力。
	var unlisted []string
	for typ := range registered {
		if builtin[typ] {
			continue
		}
		if _, ok := nonProductFragmentCapabilities[typ]; ok {
			continue
		}
		unlisted = append(unlisted, typ)
	}
	// ③ 样式表里的能力必须真的注册过（能力删了要同步清理表）。
	var stale []string
	for typ := range styled {
		if !registered[typ] {
			stale = append(stale, "有样式族:"+typ)
		}
	}
	for typ := range unstyled {
		if !registered[typ] {
			stale = append(stale, "无样式清单:"+typ)
		}
	}
	// ④ 对照集里的能力必须真的注册过（对照集不能凭空多写）。
	var phantom []string
	for typ := range builtin {
		if !registered[typ] {
			phantom = append(phantom, typ)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(unlisted)
	sort.Strings(stale)
	sort.Strings(phantom)

	if len(uncovered) > 0 {
		t.Errorf("这些片段能力没有登记样式归属 %v：\n"+
			"要么归某个片段样式族（internal/builder/fragment_base.go 的 caps），"+
			"要么进无样式清单并写清现状 —— 否则「新片段有没有样式」又变成没人能回答的问题", uncovered)
	}
	if len(unlisted) > 0 {
		t.Errorf("注册表里有对照集不认识的能力 %v：新增能力请同步 builtinFragmentCapabilities，"+
			"运行期注册的测试能力请登记进 nonProductFragmentCapabilities（附理由）", unlisted)
	}
	if len(stale) > 0 {
		t.Errorf("片段样式表里登记了注册表不存在的能力 %v：能力删了要同步清理表", stale)
	}
	if len(phantom) > 0 {
		t.Errorf("对照集里列了注册表不存在的能力 %v：对照集与现实脱节，请同步", phantom)
	}
}
