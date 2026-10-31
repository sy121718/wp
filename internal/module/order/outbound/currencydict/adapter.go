package currencydict

import (
	"context"
	"strings"
	"sync"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/logger"
)

const dictTypeCurrency = "currency"

type Adapter struct {
	dict sysconfigcontract.DictReader

	mu     sync.Mutex
	cached []string
}

func New(dict sysconfigcontract.DictReader) *Adapter { return &Adapter{dict: dict} }

func (a *Adapter) ListCurrencyCodes(ctx context.Context) (codes []string, err error) {
	if a == nil || a.dict == nil {
		return nil, nil
	}
	a.mu.Lock()
	cached := a.cached
	a.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	opts, lerr := a.dict.ListDictOptions(ctx, dictTypeCurrency)
	if lerr != nil {

		logger.Scene("order").Error(lerr, "币种字典读取失败，本次建单跳过币种校验")
		return nil, lerr
	}
	out := make([]string, 0, len(opts))
	seen := make(map[string]bool, len(opts))
	for _, o := range opts {
		code := strings.ToUpper(strings.TrimSpace(o.Code))
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	a.mu.Lock()
	a.cached = out
	a.mu.Unlock()
	return out, nil
}
