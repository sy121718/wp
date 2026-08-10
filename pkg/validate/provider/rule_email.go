package provider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

var emailStrictPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

// RegisterEmailRules 注册邮箱相关校验规则。
func RegisterEmailRules(v *validator.Validate) error {
	if err := v.RegisterValidation("email_strict", validateEmailStrict); err != nil {
		return fmt.Errorf("注册 email_strict 规则失败: %w", err)
	}
	return nil
}

func validateEmailStrict(fl validator.FieldLevel) bool {
	value := strings.TrimSpace(fl.Field().String())
	if value == "" {
		return true
	}
	return emailStrictPattern.MatchString(value)
}
