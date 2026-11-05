package checkoutcountries

import (
	"context"
	"strings"
	"sync"

	"go_wp/pkg/builder/core"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/logger"
)

type Source struct {
	dict sysconfigcontract.DictReader

	mu    sync.Mutex
	cache map[string][]core.CheckoutCountry
}

func New(dict sysconfigcontract.DictReader) *Source {
	return &Source{dict: dict, cache: map[string][]core.CheckoutCountry{}}
}

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
		return nil
	}
	out := make([]core.CheckoutCountry, 0, len(rows))
	for _, r := range rows {
		code := strings.TrimSpace(r.Code)
		if code == "" {
			continue
		}
		label := strings.TrimSpace(r.Label)
		if label == "" {
			label = code
		}
		out = append(out, core.CheckoutCountry{Code: code, Label: label})
	}
	s.mu.Lock()
	s.cache[key] = out
	s.mu.Unlock()
	return out
}

func normalizeLang(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return "zh"
	}
	return "en"
}
