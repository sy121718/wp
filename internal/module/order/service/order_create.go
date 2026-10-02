package orderservice

// order_create.go — 建单（BIZ-1 销售侧）：**编排**。
//
// 事务边界（2026-09-19 收口）：订单头 + 订单项 + 流转流水 + 券核销 + **扣库存**
// 全部落在**同一个事务**里（见 order_create_persist.go）。跨模块只传 *gorm.DB 句柄
// （stock.DeductStockTx），库存侧不再自己开事务。
//
// 为什么不再「先写单、提交、再扣库存、失败补偿成已取消」：
//   补偿本身也会失败（旧代码还把它 `_ =` 吞掉），失败时库里会同时留下
//   「有订单」「库存没动」两处事实，谁也没法从数据上判断到底发没发货。同库跨模块
//   必须事务透传，任一步失败整体回滚 —— 失败的单干脆不存在，调用方拿到明确错误。
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

	"gorm.io/gorm"

	"go_wp/pkg/database"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	usercontract "go_wp/internal/module/user/contract"
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

// CreateOrder 访客结算建单，来源固定为 checkout。
func (s *Service) CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaCheckout)
}

// CreateAPIOrder 站点 API 建单，来源固定为 api。
func (s *Service) CreateAPIOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaAPI)
}

// CreateAdminOrder 后台代客建单，来源固定为 admin。
func (s *Service) CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaAdmin)
}

// createOrder 共用建单流水：校验 → 幂等 → 备料 → 落库 → 扣库存。
func (s *Service) createOrder(ctx context.Context, req *orderdto.CreateOrderReq, createdVia string) (res *orderdto.CreateOrderResp, err error) {
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

	// 备料：商品事实、金额、优惠码、归因 —— 全部在事务之外算好。
	// （访客开号**不在这里**：BIZ-11 把它移进了下面的事务。）
	draft, err := s.buildOrderDraft(ctx, req, projectID, createdVia)
	if err != nil {
		return nil, err
	}

	// 订单（头 + 项 + 流水 + 券核销 + 扣库存）与**访客开号**同一个事务：
	// 任一步失败整单不存在，账号也随之不存在 —— 不再有「建号成功、订单回滚」
	// 留下的孤儿账号（客户能登录、却没有任何订单，还已经收到了初始密码邮件）。
	//
	// 初始密码邮件**不在这里发**（事务还没提交，发了回滚收不回）：载荷带出来，
	// 提交成功之后再发；发信失败只影响响应里的 AccountMailed，不回滚任何东西。
	var pendingMail *usercontract.GuestAccountMail
	if err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		userID, mail, perr := s.provisionGuestAccountTx(ctx, tx, req)
		if perr != nil {
			return perr
		}
		if !sameUserID(userID, draft.head.UserID) {
			// 会员身份与事务前预解析的不一致（通常是事务内刚建出了账号）：
			// 用真实身份重算会员折扣、逐行分摊与总额，再落库。
			draft.head.UserID = userID
			s.applyLineMoney(ctx, draft, userID)
		}
		pendingMail = mail
		return s.persistOrderTx(ctx, tx, draft)
	}); err != nil {
		// 并发同键撞唯一索引：另一个请求已经把单建出来了，回读它原样返回。
		if database.IsUniqueViolation(err) {
			if dup, derr := s.orderByRequestID(ctx, projectID, req.RequestID); derr == nil && dup != nil {
				return dup, nil
			}
		}
		return nil, err
	}
	// 事务已提交：这时才发初始密码邮件。发不出去不是下单失败 —— 账号已经在了，
	// 客户可以走「忘记密码」自己重置；这里只影响响应里的 AccountMailed。
	if pendingMail != nil && s.guest != nil && s.guest.SendGuestAccountMail(ctx, pendingMail) == nil {
		draft.accountMailed = true
	}

	return draft.response(), nil
}

// sameUserID 比较两个可空会员身份（指针本身不参与比较，比的是它指向的 id）。
func sameUserID(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
