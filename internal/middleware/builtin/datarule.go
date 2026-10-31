package builtin

import (
	"context"
	"strconv"
	"sync/atomic"

	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/datarule"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

type DataRuleDeptResolver func(deptID uint64) []uint64

var dataRuleSubtreeResolver atomic.Pointer[DataRuleDeptResolver]

func SetDataRuleDeptResolver(subtrees DataRuleDeptResolver) {
	if subtrees != nil {
		dataRuleSubtreeResolver.Store(&subtrees)
	}
}

func resolveDeptIDs(slot *atomic.Pointer[DataRuleDeptResolver], deptID uint64) []uint64 {
	if deptID == 0 {
		return nil
	}
	resolver := slot.Load()
	if resolver == nil {
		return nil
	}
	return (*resolver)(deptID)
}

func DataRuleContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("user_id")
		if !exists {
			c.Next()
			return
		}
		userID := uint64(userIDVal.(int64))
		if userID == 0 {
			c.Next()
			return
		}

		session, err := auth.GetUserSession(c.Request.Context(), userID)
		if err != nil || session == nil {
			logger.Scene("middleware").With("user_id", userID).With("err", err).Warn("会话读取失败，数据规则降级")
			c.Next()
			return
		}

		roleCodes, roleErr := casbin.GetRoleCodesByUserID(strconv.FormatUint(userID, 10))
		if roleErr != nil {
			logger.Scene("middleware").With("user_id", userID).With("err", roleErr).Warn("角色读取失败，数据规则降级")
		}

		uc := &datarule.UserContext{
			UserID:         userID,
			DeptID:         session.DeptID,
			IsAdmin:        session.IsAdmin,
			Roles:          roleCodes,
			DeptSubtreeIDs: resolveDeptIDs(&dataRuleSubtreeResolver, session.DeptID),
		}

		ctx := context.WithValue(c.Request.Context(), datarule.UserContextKey{}, uc)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
