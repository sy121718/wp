package productenums

import (
	"strings"
	"testing"
)

func TestErrorDetailKeysStayInProductDomain(t *testing.T) {
	seen := map[string]bool{}
	forEachStringConst(t, func(name, key string) {
		if !strings.HasPrefix(name, "Detail") {
			return
		}
		if seen[key] {
			t.Errorf("明细常量共用键: %s", key)
		}
		seen[key] = true
		if !flatTranslationKey.MatchString(key) {
			t.Errorf("业务明细必须使用完整短 key: %s=%s", name, key)
		}
		if strings.TrimSpace(ErrDetailFallbacks[key]) == "" {
			t.Errorf("业务明细缺少声明处兜底: %s", name)
		}
	})
	if len(seen) != len(ErrDetailFallbacks) {
		t.Errorf("明细声明与兜底未一一对应: %d/%d", len(seen), len(ErrDetailFallbacks))
	}
}
