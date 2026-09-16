package orderservice

// order_scope.go — 只带 id 的入口如何拿到工程作用域（DB-009 第四批）。
//
// orders / order_returns / coupons 在迁移 215 里都带 FORCE 策略：不带 app.project_id 的
// 读写在非超级角色下**静默落空**（读到 nil、更新 0 行、锁不住行），而这些入口的请求里
// 只有 id —— 后台订单管理页（取消 / 改状态 / 退款 / 备注）、支付回调、访客撤销退货申请、
// 超时取消扫描都属此类。
//
// 做法与 page 侧同形：**先逐工程独立作用域探测出归属**（id 是主键，跨工程不会重复命中），
// 拿到实体自带的 project_id 之后再进事务 —— 事务内的作用域用它，绝不退回「不限工程」。
// 方向也是安全的：探测本身受策略约束，拿不到别的工程的行。
//
// 为什么探测放在事务**外**：rls.InProjectScope 会新开事务、另取连接，放进已开的事务里
// 会让外层未提交的数据不可见、同表写入还可能自锁（见 pkg/rls.ScopeTx 的说明）。
// 工程归属是稳定属性（订单不会换工程），所以事务外取到的作用域在事务内依然成立。

import (
	"context"
	"errors"
	"strings"

	orderenums "go_wp/internal/module/order/enums"

	projectcontract "go_wp/internal/module/project/contract"
)

// SetProjects 注入工程契约（装配期调用）。
//
// 注入口径：契约只用来「列出工程 id」。未注入时 projectIDs 回退到
// OrderModel.ListAllProjectIDs（直接读 projects 表的兜底路径）——
// 兜底能工作，但它是本模块唯一一处跨模块表读取，正确做法是装配点注入（落点见报告）。
func (s *Service) SetProjects(projects projectcontract.ProjectService) {
	if s == nil {
		return
	}
	s.projects = projects
}

// projectIDs 定位与扇出用的工程清单。
//
// 工程表为空时显式失败：静默返回空清单会把「读不到工程表」伪装成「没有超时订单 /
// 订单不存在」，那正是本批要消灭的 fail-silent。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.orders == nil {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	if s.projects != nil {
		list, err := s.projects.List(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(list))
		for i := range list {
			if id := strings.TrimSpace(list[i].ID); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, errors.New(orderenums.ErrProjectRequired)
		}
		return ids, nil
	}
	// 契约未注入的兜底：见 SetProjects 与 OrderModel.ListAllProjectIDs 的注释。
	ids, err := s.orders.ListAllProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	return ids, nil
}

// locateOrderProject 逐工程探测订单归属工程；找不到返回 ErrOrderNotFound。
//
// GetByID(ctx, id, projectID) 在不存在时返回 (nil, nil)，所以「命中」的判据是实体非 nil。
func (s *Service) locateOrderProject(ctx context.Context, orderID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.orders.GetByID(ctx, orderID, pid)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", errors.New(orderenums.ErrOrderNotFound)
}

// resolveOrderProject 优先用调用方显式传入的工程；否则逐工程探测（DB-009 第四批）。
//
// 显式优先的意义：已经知道工程的调用方（超时取消扫描逐工程调用）不必再多一轮探测。
func (s *Service) resolveOrderProject(ctx context.Context, orderID uint64, explicit string) (string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return pid, nil
	}
	return s.locateOrderProject(ctx, orderID)
}

// locateReturnProject 逐工程探测退货单归属工程；找不到返回 ErrReturnNotFound。
func (s *Service) locateReturnProject(ctx context.Context, returnID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.returns.GetByID(ctx, pid, returnID)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", errors.New(orderenums.ErrReturnNotFound)
}

// locateCouponProject 逐工程探测优惠码归属工程；找不到返回 ErrCouponNotFound。
func (s *Service) locateCouponProject(ctx context.Context, couponID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.coupons.GetByID(ctx, pid, couponID)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", errors.New(orderenums.ErrCouponNotFound)
}
