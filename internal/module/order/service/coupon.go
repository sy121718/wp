package orderservice

// 写入口只有两个（建 / 改），删除只允许在**没有核销记录**时进行：
// 有核销记录的券一旦删掉，那些记录就指向一张查不到的券，对账时分不清是数据坏了还是券被删了。
// 要停用请用 status=0（停用不删）。

// 「能不能用」与「能减多少」两件事都收在这一个文件里：试算（结算页）与核销（下单事务）
// 必须用同一份逻辑，否则会出现「结算页说能减 20、下单却只减 10」这种只能靠客诉发现的问题。

// 核销的口径只有一条：**券的每一次使用都对应一张真实订单**。
// 因此它没有独立入口，只能在建单事务里发生（见 CreateOrderReq.CouponCode）：
//
//   · 券用尽 → 整个事务回滚，订单也不会落库。访客看到的是「券用完了」，
//     而不是「下单成功但优惠没生效」—— 后者要靠客诉才能发现；
//   · 同一单重复核销 → 命中唯一键 (coupon_id, order_id)，幂等返回且**不重复计数**。
//     重试与并发重放都会走到这条路径上，把重复当错误会让重试永远失败。

// 为什么需要一个对账入口：coupons.used_count 是为并发守卫而存在的**投影**
// （核销走「UPDATE ... WHERE used_count < max_uses」的原子守卫，不能改成每次 COUNT），
// 真源是 coupon_redemptions 明细。两者没有数据库层约束，偏差会以两种方式显形：
//   - 计数偏大：券提前用尽（用户看到「已抢完」而实际还有额度）
//   - 计数偏小：可超出 max_uses 继续核销
//
// 两种都只能靠对账发现 —— 但**不自动修正**：偏差原因决定了该往哪边改，
// 自动改可能把真源也改错，先查清原因再说。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// CreateCoupon 新建优惠码。
func (s *Service) CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error) {
	if req == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	startsAt, serr := parseCouponTime(req.StartsAt)
	if serr != nil {
		return nil, serr
	}
	endsAt, eerr := parseCouponTime(req.EndsAt)
	if eerr != nil {
		return nil, eerr
	}
	now := time.Now()
	e := &ordermodel.CouponEntity{
		ProjectID:     strings.TrimSpace(req.ProjectID),
		Code:          normalizeCouponCode(req.Code),
		Name:          strings.TrimSpace(req.Name),
		DiscountType:  strings.TrimSpace(req.DiscountType),
		DiscountValue: req.DiscountValue,
		MinSubtotal:   req.MinSubtotal,
		MaxUses:       req.MaxUses,
		PerUserLimit:  req.PerUserLimit,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		Status:        req.Status,
		Remark:        strings.TrimSpace(req.Remark),
		CreateBy:      req.OperatorID,
		UpdateBy:      req.OperatorID,
		CreateTime:    now,
		UpdateTime:    now,
	}
	if err = validateCouponRule(e); err != nil {
		return nil, err
	}
	existing, gerr := s.coupons.GetByCode(ctx, e.ProjectID, e.Code)
	if gerr != nil {
		return nil, gerr
	}
	if existing != nil {
		return nil, errors.New(orderenums.ErrCouponCodeTaken)
	}
	if cerr := s.coupons.Create(ctx, e); cerr != nil {
		// 并发下两个请求同时通过上面的存在性检查时，唯一索引会挡住第二个 ——
		// 对外文案与显式检查一致，不把数据库错误原文抛给运营。
		if database.IsUniqueViolation(cerr) {
			return nil, errors.New(orderenums.ErrCouponCodeTaken)
		}
		return nil, cerr
	}
	return toCouponResp(e, now), nil
}

// UpdateCoupon 修改优惠码。
//
// 券码**不可改**：改码等于换一张券，历史核销记录里的码会变成一个查不到的值。
// 需要新码就新建一张券、把旧的停用。
func (s *Service) UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error) {
	if req == nil || req.ID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	// 定位跳（DB-009 第四批）：请求只给 id；coupons 带 FORCE 策略，不带作用域的读取
	// 在非超级角色下返回 nil（表现为「优惠码不存在」）。归属由逐工程探测确定。
	projectID, perr := s.locateCouponProject(ctx, req.ID)
	if perr != nil {
		return nil, perr
	}
	existing, err := s.coupons.GetByID(ctx, projectID, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	startsAt, serr := parseCouponTime(req.StartsAt)
	if serr != nil {
		return nil, serr
	}
	endsAt, eerr := parseCouponTime(req.EndsAt)
	if eerr != nil {
		return nil, eerr
	}
	now := time.Now()
	// 在既有行上改字段后整体校验，保证「改完的券」本身自洽（而不是逐字段各校验一次）。
	next := *existing
	next.Name = strings.TrimSpace(req.Name)
	next.DiscountType = strings.TrimSpace(req.DiscountType)
	next.DiscountValue = req.DiscountValue
	next.MinSubtotal = req.MinSubtotal
	next.MaxUses = req.MaxUses
	next.PerUserLimit = req.PerUserLimit
	next.StartsAt = startsAt
	next.EndsAt = endsAt
	next.Status = req.Status
	next.Remark = strings.TrimSpace(req.Remark)
	if verr := validateCouponRule(&next); verr != nil {
		return nil, verr
	}
	fields := map[string]any{
		"name":           next.Name,
		"discount_type":  next.DiscountType,
		"discount_value": next.DiscountValue,
		"min_subtotal":   next.MinSubtotal,
		"max_uses":       next.MaxUses,
		"per_user_limit": next.PerUserLimit,
		"starts_at":      next.StartsAt,
		"ends_at":        next.EndsAt,
		"status":         next.Status,
		"remark":         next.Remark,
		"update_by":      req.OperatorID,
		"update_time":    now,
	}
	if uerr := s.coupons.UpdateFields(ctx, next.ProjectID, next.ID, fields); uerr != nil {
		return nil, uerr
	}
	updated, err := s.coupons.GetByID(ctx, next.ProjectID, next.ID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	return toCouponResp(updated, now), nil
}

// GetCoupon 按 id 取券。
func (s *Service) GetCoupon(ctx context.Context, couponID uint64) (res *orderdto.CouponResp, err error) {
	if couponID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.locateCouponProject(ctx, couponID)
	if perr != nil {
		return nil, perr
	}
	e, err := s.coupons.GetByID(ctx, projectID, couponID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	return toCouponResp(e, time.Now()), nil
}

// ListCoupons 优惠码列表。
//
// status 这个查询参数是**展示口径**（enabled / disabled / expired / exhausted），
// 翻译成 model 认得的时间与次数条件后再下推 —— 让 model 认识「过期」这个词，
// 等于把业务语义摊进查询层。
func (s *Service) ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (res *orderdto.CouponListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	f := ordermodel.CouponFilter{
		ProjectID: req.ProjectID,
		Keyword:   req.Keyword,
		Offset:    req.Offset,
		Limit:     req.Limit,
	}
	enabled, disabled, expired, exhausted := ordermodel.CouponStatusEnabled, ordermodel.CouponStatusDisabled, true, true
	switch strings.ToLower(strings.TrimSpace(req.Status)) {
	case "":
	case "enabled":
		f.Status, f.Expired = &enabled, &expired
	case "disabled":
		f.Status = &disabled
	case "expired":
		f.Expired = &expired
	case "exhausted":
		f.Exhausted = &exhausted
	default:
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	list, total, lerr := s.coupons.List(ctx, f)
	if lerr != nil {
		return nil, lerr
	}
	now := time.Now()
	res = &orderdto.CouponListResp{List: make([]*orderdto.CouponResp, 0, len(list)), Total: total}
	for _, e := range list {
		res.List = append(res.List, toCouponResp(e, now))
	}
	return res, nil
}

// DeleteCoupon 删除优惠码（有核销记录的一律拒绝）。
func (s *Service) DeleteCoupon(ctx context.Context, couponID uint64) (err error) {
	if couponID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.locateCouponProject(ctx, couponID)
	if perr != nil {
		return perr
	}
	e, err := s.coupons.GetByID(ctx, projectID, couponID)
	if err != nil {
		return err
	}
	if e == nil {
		return errors.New(orderenums.ErrCouponNotFound)
	}
	used, cerr := s.coupons.CountRedemptions(ctx, e.ProjectID, couponID, nil)
	if cerr != nil {
		return cerr
	}
	if used > 0 {
		return errors.New(orderenums.ErrCouponInUse)
	}
	// 删除带工程作用域：越界删在换角色后会被策略拒绝，而不是删掉别的工程的券。
	return s.coupons.Delete(ctx, e.ProjectID, couponID)
}

// ListCouponRedemptions 核销记录列表。
func (s *Service) ListCouponRedemptions(ctx context.Context, req *orderdto.CouponRedemptionListReq) (res *orderdto.CouponRedemptionListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	list, total, lerr := s.coupons.ListRedemptions(ctx, ordermodel.CouponRedemptionFilter{
		ProjectID: req.ProjectID,
		CouponID:  req.CouponID,
		Code:      normalizeCouponCode(req.Code),
		OrderID:   req.OrderID,
		Offset:    req.Offset,
		Limit:     req.Limit,
	})
	if lerr != nil {
		return nil, lerr
	}
	res = &orderdto.CouponRedemptionListResp{List: make([]*orderdto.CouponRedemptionResp, 0, len(list)), Total: total}
	for _, e := range list {
		res.List = append(res.List, &orderdto.CouponRedemptionResp{
			ID: e.ID, CouponID: e.CouponID, Code: e.Code,
			OrderID: e.OrderID, OrderNo: e.OrderNo,
			DiscountAmount: e.DiscountAmount,
			DiscountLabel:  fmt.Sprintf("%s 元", centsToYuanLabel(e.DiscountAmount)),
			UserID:         e.UserID,
			CreateTime:     utils.NewJSONTime(e.CreateTime),
		})
	}
	return res, nil
}

// ValidateCoupon 试算：纯读、不占次数。
//
// 不可用时**不返回 error**，而是在 res 里给 Usable=false + Message：
// 「这张券用不了」是正常业务结论（客服要能照着这句话回答客户），
// 只有参数本身不合法（缺工程、缺码）才是调用错误。
func (s *Service) ValidateCoupon(ctx context.Context, req *orderdto.CouponValidateReq) (res *orderdto.CouponValidateResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	code := normalizeCouponCode(req.Code)
	if code == "" {
		return nil, errors.New(orderenums.ErrCouponCodeRequired)
	}
	e, err := s.coupons.GetByCode(ctx, req.ProjectID, code)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	subtotal := req.Subtotal
	if subtotal < 0 {
		subtotal = 0
	}
	now := time.Now()
	res = &orderdto.CouponValidateResp{
		CouponID: e.ID, Code: e.Code, Name: e.Name,
		DiscountType: e.DiscountType, DiscountValue: e.DiscountValue,
		Subtotal: subtotal, Total: subtotal, TotalLabel: centsToYuanLabel(subtotal),
	}
	if reason := couponRuleCheck(e, subtotal, now); reason != "" {
		res.Message = reason
		return res, nil
	}
	if e.PerUserLimit > 0 && req.UserID != 0 {
		used, cerr := s.coupons.CountRedemptions(ctx, e.ProjectID, e.ID, &req.UserID)
		if cerr != nil {
			return nil, cerr
		}
		if used >= int64(e.PerUserLimit) {
			res.Message = orderenums.ErrCouponUserLimit
			return res, nil
		}
	}
	d := couponDiscount(e, subtotal)
	res.Usable = true
	res.DiscountAmount = d
	res.DiscountLabel = couponDiscountLabel(e)
	res.Total = subtotal - d
	res.TotalLabel = centsToYuanLabel(res.Total)
	res.Message = fmt.Sprintf("优惠码可用，已抵扣 %s 元", centsToYuanLabel(d))
	return res, nil
}

// toCouponResp 实体 → 视图（展示口径的字段在这里一次性算好）。
func toCouponResp(e *ordermodel.CouponEntity, now time.Time) *orderdto.CouponResp {
	if e == nil {
		return nil
	}
	// 时间窗的解释口径随券一起返回（审计 TX-011）：界面上必须能看出「填的是哪个时区」。
	//
	// 口径状态先算一次再进结构体：它同时是展示标签与徽标分档的输入 ——
	// 算两次会在「跨过生效边界的那一瞬间」渲染出标签与颜色不一致的行。
	state := couponState(e, now)
	return &orderdto.CouponResp{
		TimeZone: couponWindowLocation.String(),
		ID:       e.ID, ProjectID: e.ProjectID,
		Code: e.Code, Name: e.Name,
		DiscountType: e.DiscountType, DiscountValue: e.DiscountValue,
		DiscountLabel:    couponDiscountLabel(e),
		MinSubtotal:      e.MinSubtotal,
		MinSubtotalLabel: fmt.Sprintf("%s 元", centsToYuanLabel(e.MinSubtotal)),
		MaxUses:          e.MaxUses,
		UsedCount:        e.UsedCount,
		PerUserLimit:     e.PerUserLimit,
		StartsAt:         utils.NewJSONTimePtr(e.StartsAt),
		EndsAt:           utils.NewJSONTimePtr(e.EndsAt),
		Status:           e.Status,
		State:            state,
		StateTone:        orderenums.CouponStateTone(state),
		StatusLabel:      couponStatusLabel(e, now),
		Remark:           e.Remark,
		CreateTime:       utils.NewJSONTime(e.CreateTime),
		UpdateTime:       utils.NewJSONTime(e.UpdateTime),
	}
}

const (
	// maxCouponCodeLen 券码长度上限（与数据库列宽一致）。
	maxCouponCodeLen = 64
	// maxCouponNameLen 优惠码名称长度上限。
	maxCouponNameLen = 120
	// maxCouponRemarkLen 备注长度上限。
	maxCouponRemarkLen = 255
)

// normalizeCouponCode 券码归一化：去首尾空白 + 转大写。
//
// 只在这一处发生。两处各转一次，迟早在某条路径上漏掉一次，
// 表现是「客服抄给客户的码用不了」—— 而客户只会认为这张券是假的。
func normalizeCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// couponTimeLayouts 后台表单与接口可能送来的时间写法。
//
// 只接受不带时区的写法（见 couponWindowLocation 对「按哪个时区解释」的说明）：
// 接受带时区的 RFC3339 会引出一个「按谁的时区算」的问题，
// 而这个问题在跨境场景下没有正确答案。
var couponTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// couponWindowLocation 券时间窗的解释时区（审计 TX-011）。
//
// 刻意**不是 time.Local**：用服务器时区解释表单输入意味着「同一张券的生效时间
// 取决于部署机器」，改一次服务器时区（或换一台机器）就会让所有券的生效时刻整体平移，
// 而界面上看到的文本一个字都没变 —— 这种偏差没有任何地方会报错。
//
// 固定为 UTC：输入 10:00 就是 UTC 10:00，与部署环境无关。展示层按同一口径呈现
// （响应里带 timeZone 字段），因此「设置的时间」与「实际生效的时间」始终是同一个。
//
// 若将来要支持「按站点运营时区」，改这里不够：站点时区必须来自**配置**
// （projects.settings）而不是服务器环境，并且解析与展示要同时改 —— 只改一处会让
// 界面与实际生效时间对不上。
var couponWindowLocation = time.UTC

// parseCouponTime 解析时间窗文本；空串 = 不限（nil）。
func parseCouponTime(raw string) (*time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	for _, layout := range couponTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, couponWindowLocation); err == nil {
			// 统一转 UTC 存储与比较：列已是 timestamptz（迁移 172），
			// 时刻不变，但代码里从此只有一种时间口径。
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, errors.New(orderenums.ErrCouponWindowInvalid)
}

// validateCouponRule 校验优惠码字段（新建与修改共用）。
//
// 逐条拒绝而不是静默纠正：把 percent=150 悄悄改成 100，运营看到的是「保存成功了」，
// 而券的力度与他填的不是一回事。
func validateCouponRule(e *ordermodel.CouponEntity) error {
	if strings.TrimSpace(e.ProjectID) == "" {
		return errors.New(orderenums.ErrProjectRequired)
	}
	if e.Code == "" {
		return errors.New(orderenums.ErrCouponCodeRequired)
	}
	if len(e.Code) > maxCouponCodeLen || len(e.Name) > maxCouponNameLen || len(e.Remark) > maxCouponRemarkLen {
		return errors.New(orderenums.ErrInvalidParam)
	}
	switch e.DiscountType {
	case ordermodel.CouponTypePercent:
		if e.DiscountValue <= 0 || e.DiscountValue > 100 {
			return errors.New(orderenums.ErrCouponValueInvalid)
		}
	case ordermodel.CouponTypeFixed:
		if e.DiscountValue <= 0 {
			return errors.New(orderenums.ErrCouponValueInvalid)
		}
	default:
		return errors.New(orderenums.ErrCouponTypeInvalid)
	}
	if e.MinSubtotal < 0 {
		return errors.New(orderenums.ErrCouponValueInvalid)
	}
	if e.MaxUses < 0 || e.PerUserLimit < 0 {
		return errors.New(orderenums.ErrCouponValueInvalid)
	}
	if e.StartsAt != nil && e.EndsAt != nil && !e.EndsAt.After(*e.StartsAt) {
		// 结束不晚于开始 = 这张券永远不可能生效，存进去等于把活动悄悄废掉。
		return errors.New(orderenums.ErrCouponWindowInvalid)
	}
	if e.Status != ordermodel.CouponStatusEnabled && e.Status != ordermodel.CouponStatusDisabled {
		return errors.New(orderenums.ErrInvalidParam)
	}
	return nil
}

// couponDiscount 折扣额（分）。纯函数：只依赖券口径与小计。
//
// percent 的语义是**折扣力度**（减去小计的百分之多少，100 = 全免），不是折后价：
// 「打 8 折」与「减 80%」差着一个数量级，这类歧义必须在注释里钉死，
// 否则下一个人照着字段名 discountValue 猜，就会猜出另一套算法。
//
// 百分比按分向下取整（不足 1 分不减免）：向上取整会让商家在每单上多让一点利。
func couponDiscount(e *ordermodel.CouponEntity, subtotal int64) int64 {
	if e == nil || subtotal <= 0 {
		return 0
	}
	var d int64
	switch e.DiscountType {
	case ordermodel.CouponTypePercent:
		d = subtotal * e.DiscountValue / 100
	case ordermodel.CouponTypeFixed:
		d = e.DiscountValue
	default:
		return 0
	}
	if d > subtotal {
		// 折扣不得超过小计：负数总额没有意义，而且会把账算乱。
		d = subtotal
	}
	if d < 0 {
		d = 0
	}
	return d
}

// couponRuleCheck 券自身状态的可用性检查，返回不可用原因（空串 = 通过）。
//
// 这里刻意**不含**每人限次：那条要查核销表，不是纯计算，
// 混进来会让「试算」这个纯读函数偷偷变成一次数据库往返的隐式依赖。
func couponRuleCheck(e *ordermodel.CouponEntity, subtotal int64, now time.Time) string {
	if e == nil {
		return orderenums.ErrCouponNotFound
	}
	switch {
	case e.Status != ordermodel.CouponStatusEnabled:
		return orderenums.ErrCouponDisabled
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return orderenums.ErrCouponNotStarted
	case e.EndsAt != nil && now.After(*e.EndsAt):
		return orderenums.ErrCouponExpired
	case e.MaxUses > 0 && e.UsedCount >= e.MaxUses:
		return orderenums.ErrCouponExhausted
	case subtotal < e.MinSubtotal:
		return orderenums.ErrCouponMinSubtotal
	}
	return ""
}

// couponDiscountLabel 券口径的展示文案（如「减 20%」「减 20.00 元」）。
func couponDiscountLabel(e *ordermodel.CouponEntity) string {
	if e == nil {
		return ""
	}
	if e.DiscountType == ordermodel.CouponTypePercent {
		return fmt.Sprintf("减 %d%%", e.DiscountValue)
	}
	return fmt.Sprintf("减 %s 元", centsToYuanLabel(e.DiscountValue))
}

// couponState 券当前的**展示口径状态**（按时间与次数算出来，不看单一列）。
//
// 状态列只表达「运营有没有手动停用」；过期、未开始、用尽都是时间的函数，
// 单独存一列必然与真实状态不同步（要靠定时任务去刷，而定时任务总有停的时候）。
//
// 返回值是枚举口径值（orderenums.CouponState*），**不是文案**：
// 展示层拿它挑徽章样式并按词条渲染文案 —— 拿中文标签反查样式表的旧实现，
// 运营在后台改一句词条就能让徽章静默失效。
func couponState(e *ordermodel.CouponEntity, now time.Time) string {
	if e == nil {
		return ""
	}
	switch {
	case e.Status != ordermodel.CouponStatusEnabled:
		return orderenums.CouponStateDisabled
	case e.EndsAt != nil && now.After(*e.EndsAt):
		return orderenums.CouponStateExpired
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return orderenums.CouponStateNotStarted
	case e.MaxUses > 0 && e.UsedCount >= e.MaxUses:
		return orderenums.CouponStateExhausted
	}
	return orderenums.CouponStateEnabled
}

// couponStatusLabel 券当前状态的展示文案（API 响应字段）。
//
// 中文取自 enums 的同一条真源（orderenums.CouponStateLabel 的兜底），
// 后台页面不再用它 —— 页面按 State 取词，多语言界面才可能正确。
func couponStatusLabel(e *ordermodel.CouponEntity, now time.Time) string {
	_, fallback := orderenums.CouponStateLabel(couponState(e, now))
	return fallback
}

// usageLabel 用次展示：「3 / 不限」「3 / 100」。
func usageLabel(used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d / 不限", used)
	}
	return fmt.Sprintf("%d / %d", used, max)
}

// redeemCouponTx 事务内核销一次券。
//
// e 为 nil（这一单没用券）时直接返回，调用方不必在事务里写 if。
func (s *Service) redeemCouponTx(ctx context.Context, tx *gorm.DB, e *ordermodel.CouponEntity, orderID uint64, orderNo string, discount int64, userID *uint64, now time.Time) (err error) {
	if e == nil {
		return nil
	}
	locked, lerr := s.coupons.LockByIDTx(ctx, tx, e.ProjectID, e.ID)
	if lerr != nil {
		return lerr
	}
	if locked == nil {
		return errors.New(orderenums.ErrCouponNotFound)
	}
	e = locked
	if e.PerUserLimit > 0 && userID != nil {
		used, cerr := s.coupons.CountRedemptionsTx(ctx, tx, e.ProjectID, e.ID, userID)
		if cerr != nil {
			return cerr
		}
		if used >= int64(e.PerUserLimit) {
			return errors.New(orderenums.ErrCouponUserLimit)
		}
	}
	inserted, ierr := s.coupons.InsertRedemptionTx(ctx, tx, &ordermodel.CouponRedemptionEntity{
		CouponID:       e.ID,
		ProjectID:      e.ProjectID,
		Code:           e.Code,
		OrderID:        orderID,
		OrderNo:        orderNo,
		DiscountAmount: discount,
		UserID:         userID,
		CreateTime:     now,
	})
	if ierr != nil {
		return ierr
	}
	if !inserted {
		// 这一单此前已经核销过这张券：幂等命中。不递增计数 ——
		// 否则重试一次就把可用次数多算掉一次，而次数是真金白银。
		return nil
	}
	ok, uerr := s.coupons.IncrementUsedTx(ctx, tx, e.ID)
	if uerr != nil {
		return uerr
	}
	if !ok {
		// 守卫没通过 = 用尽（并发抢最后一次）。返回错误让**整个事务回滚**：
		// 明细行与订单头一起消失，不留下「券记了一笔但单没下成」的半截状态。
		return errors.New(orderenums.ErrCouponExhausted)
	}
	return nil
}

// auditProjectIDs 对账要跑哪些工程：显式工程优先，留空则枚举全部工程逐工程对账。
//
// 为什么不保留「不限工程一次查完」（DB-009 第七批）：coupons 与 coupon_redemptions
// 都带 FORCE 策略、谓词读会话变量 —— 没有作用域的查询恒返回空集、计数恒 0，
// 对账于是输出一份「检查了 0 张券、0 个偏差」的**看起来正常的假报告**，
// 而那正是对账要发现问题的场景。取不到工程清单时显式失败，不退化成「不限工程」。
func (s *Service) auditProjectIDs(ctx context.Context, explicit string) ([]string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return []string{pid}, nil
	}
	return s.projectIDs(ctx)
}

// AuditCouponCounts 对账券的 used_count 与核销明细行数。
func (s *Service) AuditCouponCounts(ctx context.Context, req *orderdto.CouponCountAuditReq) (res *orderdto.CouponCountAuditResp, err error) {
	if s == nil || s.coupons == nil {
		return &orderdto.CouponCountAuditResp{Items: []orderdto.CouponCountMismatch{}}, nil
	}
	projectID := ""
	limit := 100
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	// 工程清单（DB-009 第七批）：显式工程优先，留空则逐工程独立作用域跑一遍再合并。
	projects, perr := s.auditProjectIDs(ctx, projectID)
	if perr != nil {
		return nil, perr
	}
	res = &orderdto.CouponCountAuditResp{Items: make([]orderdto.CouponCountMismatch, 0)}
	for _, pid := range projects {
		if ctx.Err() != nil {
			break
		}
		checked, cerr := s.coupons.CountCoupons(ctx, pid)
		if cerr != nil {
			return nil, cerr
		}
		// limit 是**每工程**的明细上限（全站口径下原来是全局 top-N）：会话变量是单值，
		// 多个工程不可能并进一次查询。它只决定一次返回多少行明细，
		// 不改变 Checked / Mismatched 的口径。
		rows, lerr := s.coupons.ListCountMismatches(ctx, pid, limit)
		if lerr != nil {
			return nil, lerr
		}
		res.Checked += int(checked)
		res.Mismatched += len(rows)
		for _, row := range rows {
			res.Items = append(res.Items, orderdto.CouponCountMismatch{
				CouponID: row.CouponID, ProjectID: row.ProjectID, Code: row.Code,
				UsedCount: row.UsedCount, ActualCount: row.ActualCount,
				Diff: row.UsedCount - row.ActualCount,
			})
		}
	}
	if len(res.Items) > 0 {
		// 记日志而不告警升级：偏差不一定影响可用性，但需要有人知道。
		logger.Scene("order").With("mismatched", len(res.Items)).With("checked", res.Checked).
			Warn("券计数与核销明细不一致（只报告，不自动修正）")
	}
	return res, nil
}
