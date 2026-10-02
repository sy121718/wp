package sysconfigservice

// sysconfig_group.go — 读路径：单组读取与分组清单。

import (
	"context"
	"errors"
	"strings"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
)

// GetGroup 取一组配置；分组不存在返回 ErrGroupNotFound。
//
// 「不存在」不返回空对象：空对象会让消费方分不清「这一组没配」与「这一组配了但全空」，
// 而这两种情形在默认值链上的处理不同（前者回退代码常量、后者按配置为空处理）。
func (s *Service) GetGroup(ctx context.Context, groupKey string) (res *sysconfigdto.Group, err error) {
	key := strings.TrimSpace(groupKey)
	if key == "" {
		return nil, errors.New(sysconfigenums.ErrInvalidParam)
	}
	e, err := s.m.FindByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, sysconfigcontract.ErrGroupNotFound
	}
	return groupOf(e), nil
}

// ListGroups 列出**启用**的分组（按分组键排序，输出稳定）。
//
// 只列启用组：禁用组是「后台保留但当前不生效」的配置，让消费方读到它等于把
// status 列的语义抹掉（读的人还得自己判一次）。
func (s *Service) ListGroups(ctx context.Context) (res []sysconfigdto.Group, err error) {
	rows, err := s.m.ListByStatus(ctx, sysconfigmodel.StatusEnabled)
	if err != nil {
		return nil, err
	}
	res = make([]sysconfigdto.Group, 0, len(rows))
	for i := range rows {
		res = append(res, *groupOf(&rows[i]))
	}
	return res, nil
}
