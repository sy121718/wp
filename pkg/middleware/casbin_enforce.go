package middleware

import (
	"errors"
	"strconv"

	"go_wp/pkg/casbin"
)

func EnforceForUser(userID int64, obj, act string) (bool, error) {
	enforcer := casbin.GetEnforcer()
	if enforcer == nil {
		return false, errors.New("权限系统未初始化")
	}
	return enforcer.Enforce(strconv.FormatInt(userID, 10), obj, act)
}
