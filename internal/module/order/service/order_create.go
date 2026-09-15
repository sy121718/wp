package orderservice

// order_create.go — 建单（BIZ-1 销售侧）：**编排**。
//
// 事务边界与补偿策略照采购入库（#18）的既有先例：
//   ① 先在**一个事务**里写订单头 + 订单项 + 流转流水（订单是主记录，也是扣减的依据）；
//   ② 提交之后再动库存（跨模块，不可能共用一个事务）；
//   ③ 库存不足或库存服务不可用 → **补偿**：把订单标记为已取消并记流水。
//
// 为什么不「先扣库存再写单」：扣减的 source_ref 要用订单号，而订单号不依赖订单 id，
// 两种顺序都能做。选「先写单」是因为它让失败**留痕** —— 补偿后库里留下一条
// 「因库存不足而失败」的已取消订单，能看出发生过什么；反过来先扣库存、写单失败，
// 就只能把库存悄悄归还，事后查不出任何痕迹。
//
// 文件分工（CQ-023：此前是 278 行的单函数）：
//   · 本文件 —— 编排、入参校验、幂等查、订单号与展示辅助；
//   · order_create_draft.go —— 事务之前的一切（商品快照 / 金额 / 优惠码 / 归因 / 访客开号）；
//   · order_create_persist.go —— 事务写入 + 扣库存与失败补偿。
// 拆开的理由不是「行数好看」：这个函数里藏着本次审计发现的两个资损缺陷
//（无券时采信客户端折扣、每人限次在事务外判定），越长的函数越难发现这类问题。

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go_wp/pkg/database"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

const (
	// maxOrderItems 单笔订单的商品项上限。
	//
	// 上限不是性能问题而是**幂等与计算**问题：订单号与金额都在一次请求里算完，
	// 无上限的输入会让一次请求锁住任意多的库存行。
	maxOrderItems = 100
	// maxItemQuantity 单项数量上限（与库存的硬上限同量级）。
	maxItemQuantity = 100000
)

// CreateOrder 建单：校验 → 幂等 → 备料 → 落库 → 扣库存（失败补偿）。
func (s *Service) CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	if err = validateCreateOrderReq(req); err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(req.ProjectID)

	// 幂等：同一 request_id 命中既有单就原样返回，绝不再扣一次库存。
	existing, err := s.orderByRequestID(ctx, projectID, req.RequestID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// 备料：商品事实、金额、优惠码、归因、访客账号 —— 全部在事务之外算好。
	draft, err := s.buildOrderDraft(ctx, req, projectID)
	if err != nil {
		return nil, err
	}

	// ① 订单（头 + 项 + 流水 + 券核销）一个事务。
	if err = s.persistOrder(ctx, draft); err != nil {
		// 并发同键撞唯一索引：另一个请求已经把单建出来了，回读它原样返回。
		if database.IsUniqueViolation(err) {
			if dup, derr := s.orderByRequestID(ctx, projectID, req.RequestID); derr == nil && dup != nil {
				return dup, nil
			}
		}
		return nil, err
	}

	// ② 扣库存（跨模块，落在订单事务之外）；③ 失败补偿在同一函数内。
	if err = s.deductStockOrCompensate(ctx, draft); err != nil {
		return nil, err
	}

	return draft.response(), nil
}

// validateCreateOrderReq 入参校验：不碰数据库，纯形状检查。
func validateCreateOrderReq(req *orderdto.CreateOrderReq) error {
	if req == nil {
		return errors.New(orderenums.ErrInvalidParam)
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return errors.New(orderenums.ErrProjectRequired)
	}
	email := strings.TrimSpace(req.CustomerEmail)
	if email == "" {
		return errors.New(orderenums.ErrCustomerEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return errors.New(orderenums.ErrCustomerEmailInvalid)
	}
	if len(req.Items) == 0 {
		return errors.New(orderenums.ErrItemsRequired)
	}
	if len(req.Items) > maxOrderItems {
		return errors.New(orderenums.ErrItemLimitExceeded)
	}
	return nil
}

// orderByRequestID 幂等键命中则返回既有单的响应；无键或未命中返回 nil。
//
// 首次查（进入建单前）与唯一冲突后的回读共用它，保证两条路径返回**同一份形状**
// （Duplicated=true 的响应），不会一条带 accountMailed、另一条不带。
func (s *Service) orderByRequestID(ctx context.Context, projectID, requestID string) (*orderdto.CreateOrderResp, error) {
	reqID := strings.TrimSpace(requestID)
	if reqID == "" {
		return nil, nil
	}
	existing, err := s.orders.GetByRequestID(ctx, projectID, reqID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	return &orderdto.CreateOrderResp{
		ID: existing.ID, OrderNo: existing.OrderNo, Status: existing.Status,
		Total: existing.Total, Currency: existing.Currency, Duplicated: true,
	}, nil
}

// newOrderNo 生成订单号：时间前缀 + 随机后缀。
//
// 时间前缀便于人工识别与归档（客服报单号时能看出是哪天），随机后缀避免同秒碰撞；
// 唯一约束兜底，撞了就重试（概率极低，但重试比报错好）。
func (s *Service) newOrderNo(ctx context.Context, projectID string) (no string, err error) {
	const attempts = 5
	buf := make([]byte, 4)
	for i := 0; i < attempts; i++ {
		if _, rerr := rand.Read(buf); rerr != nil {
			return "", rerr
		}
		no = fmt.Sprintf("GWP%s%08X", time.Now().Format("20060102"), uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
		existing, gerr := s.orders.GetByNo(ctx, projectID, no)
		if gerr != nil {
			return "", gerr
		}
		if existing == nil {
			return no, nil
		}
	}
	return "", errors.New(orderenums.ErrOrderNoTaken)
}

// operatorTypeOf 下单入口 → 操作人类型。
func operatorTypeOf(createdVia string) string {
	if createdVia == ordermodel.CreatedViaAdmin {
		return ordermodel.OperatorTypeAdmin
	}
	return ordermodel.OperatorTypeCustomer
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// centsToYuanLabel 分 → 元的展示串（仅用于备注与提示，不参与计算）。
func centsToYuanLabel(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

// marshalAttribution 归因快照 → JSONB。
//
// nil 表示没有访客上下文（后台代客下单没有追踪数据），落空对象而不是 NULL ——
// 列是 NOT NULL，且「没有归因数据」与「这一列不存在」对下游是两件事。
func marshalAttribution(a *orderdto.Attribution) (json.RawMessage, error) {
	if a == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}
