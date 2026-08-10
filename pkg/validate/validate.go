package validate

import (
	"errors"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Msg 将校验错误转为中文提示
func Msg(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		for _, e := range ve {
			f := e.Field()
			p := e.Param()
			switch e.Tag() {
			case "required":
				return f + "不能为空"
			case "numeric":
				return f + "必须为数字"
			case "alpha":
				return f + "只能含字母"
			case "alphanum":
				return f + "只能含字母和数字"
			case "email":
				return f + "格式错误"
			case "email_strict":
				return f + "邮箱格式不合法"
			case "url":
				return f + "格式错误"
			case "ip":
				return f + "格式错误"
			case "datetime":
				return f + "格式错误"
			case "min":
				return f + "最少" + p + "位"
			case "max":
				return f + "最多" + p + "位"
			case "len":
				return f + "长度须为" + p
			case "gte":
				return f + "不能小于" + p
			case "lte":
				return f + "不能大于" + p
			case "gt":
				return f + "必须大于" + p
			case "lt":
				return f + "必须小于" + p
			case "oneof":
				return f + "须为" + strings.ReplaceAll(p, " ", "/") + "之一"
			}
		}
	}
	return "参数错误"
}
