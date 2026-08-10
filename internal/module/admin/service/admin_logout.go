package adminservice

import (
	"context"

	"go_wp/pkg/auth"
)

// Logout 注销当前管理员会话，使该会话签发的 JWT 立即失效。
func (s *Service) Logout(ctx context.Context, userID uint64) (err error) {
	return auth.DeleteUserSession(ctx, userID)
}
