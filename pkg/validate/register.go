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

var customRuleRegistrars = []func(v *validator.Validate) error{
	validateprovider.RegisterEmailRules,
}

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
