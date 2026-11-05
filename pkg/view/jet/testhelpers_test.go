package jet

import (
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// memSet 构造一个带显示函数的内存模板 Set，供纯函数测试使用。
func memSet(t *testing.T, files map[string]string) *jet.Set {
	t.Helper()
	loader := jet.NewInMemLoader()
	for name, src := range files {
		loader.Set(name, src)
	}
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	injectGlobals(set)
	return set
}
