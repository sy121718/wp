// Package i18nvalues 把 sysconfig 的配置分组翻译成 pkg/i18n 的运行期默认值。
//
// 为什么需要一个适配器子包（internal/module/CLAUDE.md 的 outbound 判据是「要不要翻译」）：
// 这里确实在换形状 —— 分组的 JSON（map[string]any）→ i18n.RuntimeValues 的强类型字段，
// 取值规则（缺键当未配置、类型不符忽略并留痕、语言码表逐项校验）也只在这一处实现。
// 依赖方向是 internal → pkg：pkg/i18n 不认识 sysconfig，装配层把本适配器注入它。
package i18nvalues

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// Adapter 把 sysconfig 的只读配置口适配成 i18n.ValueLoader。
type Adapter struct {
	reader sysconfigcontract.ConfigReader
}

// New 构造（reader 允许为 nil：Load 会返回明确错误，而不是静默给出一组常量）。
func New(reader sysconfigcontract.ConfigReader) Adapter { return Adapter{reader: reader} }

// 编译期断言：本适配器就是 pkg/i18n 索要的那个端口（接口形状对不上时在这里炸）。
var _ i18n.ValueLoader = Adapter{}.Load

// Load 实现 i18n.ValueLoader：读 i18n 组 → 翻译 → 返回。
//
// 分组不存在（GetGroup 返回 GroupNotFound）视为「没配」而不是错误：DB 未就绪、
// 迁移未跑、组被删都会走到这里，此时正确的行为是用代码内常量继续跑 ——
// 把它当错误会让「配置缺失」升级成「站点起不来」，而默认语言本来就有常量兜底。
func (a Adapter) Load(ctx context.Context) (i18n.RuntimeValues, error) {
	if a.reader == nil {
		return i18n.RuntimeValues{}, errors.New("sysconfig 配置读取口未注入")
	}
	group, err := a.reader.GetGroup(ctx, sysconfigcontract.GroupI18n)
	if err != nil {
		if errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
			return i18n.RuntimeValues{}, nil
		}
		return i18n.RuntimeValues{}, err
	}
	if group == nil {
		return i18n.RuntimeValues{}, nil
	}
	values, problems := RuntimeValuesOf(group.Data)
	// trade 组（全局默认国家 / 货币）：与 i18n 组同一次刷新里读出来，**不新增刷新机制** ——
	// 交易默认值只有展示与结构化数据两个读取方（见 pkg/i18n.RuntimeValues.DefaultCurrency），
	// 走的就是「装配期载入 + StartAutoRefresh 的 tick + 保存后 Invalidate」这条既有链路。
	//
	// 组不存在（还没跑迁移 497）不是错误：与 i18n 组同一口径，回退代码内常量继续跑。
	if trade, terr := a.reader.GetGroup(ctx, sysconfigcontract.GroupTrade); terr == nil && trade != nil {
		if v, ok := trade.Data["default_country"].(string); ok {
			values.DefaultCountry = strings.TrimSpace(v)
		}
		if v, ok := trade.Data["default_currency"].(string); ok {
			values.DefaultCurrency = strings.TrimSpace(v)
		}
	} else if terr != nil && !errors.Is(terr, sysconfigcontract.ErrGroupNotFound) {
		return i18n.RuntimeValues{}, terr
	}
	// 有问题的键必须留痕：类型写错的键会被当成「没配」，表现为「配置改了没生效」，
	// 而人只会怀疑自己没保存（迁移 484 的列注释：组内键由代码白名单约束、
	// 解析失败一律回退默认值 —— 回退是**可见的**，不是静默的）。
	for _, p := range problems {
		logger.Scene("i18n").With("group", sysconfigcontract.GroupI18n).Warn(p)
	}
	return values, nil
}

// 「这一组没有」用 contract 的**哨兵错误**判断（ErrGroupNotFound），不匹配文案字符串：
// 按文本匹配时文案一改判断就静默失效，表现是把「配置缺失」升级成「站点起不来」——
// 而配置缺失本来就有代码内常量兜底，不该让它变成启动失败。
//
// 本适配器因此只 import contract，不认识 enums（那是响应文案，不是错误值语义）。

// RuntimeValuesOf 把 i18n 组的 JSON 翻译成 i18n.RuntimeValues（纯函数，便于单测）。
//
// 三条规则都刻意「宁可少取，不可猜」：
//   - 字符串键只接受非空字符串，缺键 / 空串 / 非字符串一律当「没配」，由 pkg/i18n
//     回退代码内常量（常量不可配置，因此不构成第二个来源）；
//   - 语言码覆盖表只接受对象，逐项要求值为非空字符串：**非法的项丢掉而不是整表丢掉** ——
//     整表丢掉会让一次手滑把其它正确覆盖一起废掉；
//   - 所有被忽略的东西都进 problems（由调用方记日志），不静默。
func RuntimeValuesOf(data map[string]any) (vals i18n.RuntimeValues, problems []string) {
	vals.DefaultLang = stringValueOf(data, sysconfigcontract.KeyDefaultLang, &problems)
	vals.SiteLangURLMode = stringValueOf(data, sysconfigcontract.KeySiteLangURLMode, &problems)
	vals.LangURLCodes = urlCodesValueOf(data, sysconfigcontract.KeyLangURLCodes, &problems)
	sort.Strings(problems)
	return vals, problems
}

// stringValueOf 取一个字符串键；类型不符记 problem 并返回空串。
func stringValueOf(data map[string]any, key string, problems *[]string) string {
	raw, ok := data[key]
	if !ok || raw == nil {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s 取值类型不是字符串（%T），该键按未配置处理", key, raw))
		return ""
	}
	return strings.TrimSpace(s)
}

// urlCodesValueOf 取语言码覆盖表；非法项逐项丢弃。
func urlCodesValueOf(data map[string]any, key string, problems *[]string) map[string]string {
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s 取值类型不是对象（%T），该键按未配置处理", key, raw))
		return nil
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		code, ok := v.(string)
		if !ok || strings.TrimSpace(code) == "" {
			*problems = append(*problems, fmt.Sprintf("%s.%s 取值非法（%v），已忽略该项", key, k, v))
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(code)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
