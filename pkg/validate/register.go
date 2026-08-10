package validate

import (
	"fmt"
	"sync"

	validateprovider "go_wp/pkg/validate/provider"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

var (
	registerOnce sync.Once
	registerErr  error
)

// customRuleRegistrars 维护“规则注册函数”列表。
// 后续新增规则时，优先新建独立文件并把注册函数放进这个列表，而不是把所有规则堆进一个文件。
var customRuleRegistrars = []func(v *validator.Validate) error{
	validateprovider.RegisterEmailRules,
}

// RegisterCustomRules 注册自定义校验规则到 Gin 的默认校验器。
func RegisterCustomRules() error {
	registerOnce.Do(func() {
		engine := binding.Validator.Engine()
		v, ok := engine.(*validator.Validate)
		if !ok || v == nil {
			registerErr = fmt.Errorf("获取 Gin 默认校验器失败")
			return
		}

		for _, registerFn := range customRuleRegistrars {
			if err := registerFn(v); err != nil {
				registerErr = err
				return
			}
		}
	})
	return registerErr
}
