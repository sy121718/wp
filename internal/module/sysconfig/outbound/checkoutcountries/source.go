// Package checkoutcountries 把 sysconfig 的国家字典（sys_area）适配成构建期需要的形状。
//
// 适配谁：结算表单组件（core.checkoutForm）在构建期要一份「码 + 当前语言展示名」的
// 国家选项。这件事有两个非平凡点，所以独立成 outbound 子包而不是就地几行：
//
//   - **形状翻译**：sysconfig 的 CountryOption{Code, Label} → core.CheckoutCountry。
//     消费侧（组件）不认识 sysconfig 的 dto，字典侧也不该认识 builder 的渲染输入。
//   - **语言归一**：ListCountryOptions 只区分「以 zh 开头与否」（字典表当前只有
//     name_zh / name_en 两列），而构建语言是任意 BCP-47 码。不归一的话，
//     每种语言各查一次库、各缓存一份完全相同的清单。
//
// 缓存：sys_area 只有两百多行且是迁移 seed 的静态数据，进程内按（归一后）语言缓存即可。
// 读取失败**不进缓存**（下次构建重试），并记日志 —— 返回空清单会让「表单里有国家字段」
// 的页面构建失败，那条错误本身说的是「未注入国家清单」，真因在日志里。
package checkoutcountries

import (
	"context"
	"strings"
	"sync"

	"go_wp/internal/builder/core"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/logger"
)

// Source 国家选项来源（装配期构造一次，构建期并发读）。
type Source struct {
	dict sysconfigcontract.DictReader

	mu    sync.Mutex
	cache map[string][]core.CheckoutCountry
}

// New 构造来源；dict 为 sysconfig 的**只读窄口**（DictReader）。
func New(dict sysconfigcontract.DictReader) *Source {
	return &Source{dict: dict, cache: map[string][]core.CheckoutCountry{}}
}

// Countries 返回给定构建语言下的国家选项（顺序由字典的 sort_order / code 决定）。
//
// 返回的切片是**缓存内的同一份**，调用方只读、不得修改（它直接进 RenderContext）。
// 未接入（dict 为 nil）或读取失败时返回 nil：调用方按「未注入」处理
// （表单里出现国家字段时构建失败），而不是在这里静默造一个空数据源。
func (s *Source) Countries(ctx context.Context, lang string) []core.CheckoutCountry {
	if s == nil || s.dict == nil {
		return nil
	}
	key := normalizeLang(lang)
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok {
		return cached
	}
	rows, err := s.dict.ListCountryOptions(ctx, key)
	if err != nil {
		logger.Scene("build").Error(err, "结算表单国家清单读取失败")
		return nil // 不缓存：下一次构建重试
	}
	out := make([]core.CheckoutCountry, 0, len(rows))
	for _, r := range rows {
		code := strings.TrimSpace(r.Code)
		if code == "" {
			continue
		}
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = code // 字典两列都缺时用 code：宁可显示 AO，也不要空选项
		}
		out = append(out, core.CheckoutCountry{Code: code, Label: label})
	}
	s.mu.Lock()
	s.cache[key] = out
	s.mu.Unlock()
	return out
}

// normalizeLang 把构建语言归一到列表参数需要的两档（zh / en），见包注释。
func normalizeLang(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return "zh"
	}
	return "en"
}
