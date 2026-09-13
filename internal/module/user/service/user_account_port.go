package userservice

// user_account_port.go — VisitorAccountPort 的实现（访问面片段用）。
//
// 这里**不新增任何查询逻辑**，只是把 service 已有的两个读方法暴露成收窄端口：
// GetAccount / ListSessions 是账号中心页面在用的同一份实现。
// 片段层因此看到的数据与内置页面逐字节一致 —— 两套实现各自演化，
// 表现就是「页面上的昵称和片段里的昵称不一样」，而这种差异没人会主动去查。

import (
	"context"

	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
)

// 编译期断言：装配期注入的就是这个实现。
var _ usercontract.VisitorAccountPort = (*Service)(nil)

// AccountOf 读账号概览与资料 / 偏好。
func (s *Service) AccountOf(ctx context.Context, userID uint64) (res *userdto.AccountResp, err error) {
	return s.GetAccount(ctx, userID)
}

// SessionsOf 读登录设备台账（currentToken 用于标记当前设备）。
func (s *Service) SessionsOf(ctx context.Context, userID uint64, currentToken string) (items []*userdto.SessionItem, err error) {
	return s.ListSessions(ctx, userID, currentToken)
}
