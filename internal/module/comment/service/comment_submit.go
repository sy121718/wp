package commentservice

// comment_submit.go — 提交用例（状态机入口 + 限流 + 防刷 + 差异化规则）。
//
// 顺序是**刻意**的，每一步都在挡一类不同的输入：
//
//	① 登录        —— 产品口径：匿名不可评（硬约束，不看任何配置）；
//	② 限流        —— 第一道资源闸，**先于昂贵的校验**：每次请求都消耗额度，
//	                 否则「发一万条空内容」可以不花任何额度就把服务打满；
//	③ 目标三元组  —— 工程 / 实体类型（白名单）/ 实体 id（形状）；
//	④ 正文        —— trim + 长度 + 控制字符清洗（不信任客户端）；
//	⑤ 回复归一    —— 父评论必须存在且在同一实体下；多级回复归一到顶层；
//	⑥ 差异化规则  —— 消费方的 policy 端口（未注入即放行，见 contract.EntityPolicy）；
//	⑦ 落库        —— 状态恒为 pending（先审后发），事务边界在这一层。
//
// 写失败**不静默**：原文进结构化日志，调用方拿到可展示文案（片段层据此渲染一句人话）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	"go_wp/pkg/logger"
)

// Submit 提交一条评论（落 pending，等审核）。
func (s *Service) Submit(ctx context.Context, req *commentdto.SubmitReq) (res *commentdto.SubmitResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	// ① 登录：产品口径「匿名不可评」。调用方（片段层）也已判定过一次，这里再判一次 ——
	// service 不押注在调用方一定做对了（漏判的后果是**任何人都能匿名灌库**）。
	if req.UserID == 0 {
		return nil, errParam(commentenums.ErrLoginRequired)
	}
	// ② 限流：身份额度 + 来源额度（见 comment_ratelimit.go）。
	if err = s.checkSubmitRate(req.UserID, req.IPHash); err != nil {
		logger.Scene(errScene).
			With("user_id", req.UserID).
			With("entity_type", req.EntityType).
			Warn("评论提交被限流拒绝")
		return nil, err
	}
	// ③ 目标
	if err = s.validateTarget(req.ProjectID, req.EntityType, req.EntityID); err != nil {
		return nil, err
	}
	// ④ 正文
	body, err := normalizeBody(req.Body)
	if err != nil {
		return nil, err
	}
	// ⑤ 回复归一
	parentID, err := s.resolveParent(ctx, req)
	if err != nil {
		return nil, err
	}
	// ⑥ 差异化规则（可缺端口）
	if err = s.checkPolicy(ctx, req); err != nil {
		return nil, err
	}
	// ⑦ 落库：事务边界由 service 决定（model 提供 TransactionScoped + InsertTx）。
	now := s.now()
	row := &commentmodel.Entity{
		ProjectID:    strings.TrimSpace(req.ProjectID),
		EntityType:   strings.TrimSpace(req.EntityType),
		EntityID:     strings.TrimSpace(req.EntityID),
		UserID:       req.UserID,
		ParentID:     parentID,
		Body:         body,
		Status:       commentenums.StatusPending,
		AuthorIPHash: strings.TrimSpace(req.IPHash),
		CreateTime:   now,
		UpdateTime:   now,
	}
	if err = s.model.TransactionScoped(ctx, row.ProjectID, func(tx *gorm.DB) error {
		return s.model.InsertTx(ctx, tx, row)
	}); err != nil {
		// 写失败不静默：原文与定位信息进日志，调用方只需渲染一句可展示文案。
		logger.Scene(errScene).
			With("user_id", req.UserID).
			With("project_id", row.ProjectID).
			With("entity_type", row.EntityType).
			With("entity_id", row.EntityID).
			Error(err, "评论写入失败")
		return nil, err
	}
	logger.Scene(errScene).
		With("comment_id", row.ID).
		With("user_id", row.UserID).
		With("entity_type", row.EntityType).
		Info("评论已提交，待审核")
	return &commentdto.SubmitResp{
		ID:      row.ID,
		Status:  row.Status,
		Message: commentenums.MsgSubmitPending,
	}, nil
}

// resolveParent 校验并归一回复目标。
//
// 归一规则（只支持一级回复）：若父评论本身是一条回复，则把本次回复挂到
// 父评论的顶层父节点上 —— **不做多级嵌套**（见 enums 的模块口径第 3 条）。
// 不归一的话，同一段展示逻辑要么递归渲染（每条回复都要相对自己缩进），
// 要么把深层回复渲成「顶层评论」（看起来像另起一条，语义错）。
//
// 父评论必须存在、未被删、且**属于同一个实体**：跨实体挂回复会让评论在
// 另一个页面上凭空出现（父评论不在那一页，它会被提到顶层显示）。
func (s *Service) resolveParent(ctx context.Context, req *commentdto.SubmitReq) (parent *int64, err error) {
	if req.ParentID <= 0 {
		return nil, nil
	}
	parentRow, err := s.model.GetByID(ctx, strings.TrimSpace(req.ProjectID), req.ParentID)
	if err != nil {
		return nil, err
	}
	if parentRow == nil {
		return nil, errParam(commentenums.ErrParentInvalid)
	}
	if parentRow.EntityType != strings.TrimSpace(req.EntityType) ||
		parentRow.EntityID != strings.TrimSpace(req.EntityID) {
		return nil, errParam(commentenums.ErrParentInvalid)
	}
	if parentRow.ParentID != nil {
		return parentRow.ParentID, nil
	}
	id := parentRow.ID
	return &id, nil
}

// checkPolicy 问消费方的差异化规则端口（未注入即放行，理由见 contract.EntityPolicy）。
func (s *Service) checkPolicy(ctx context.Context, req *commentdto.SubmitReq) error {
	if s.policy == nil {
		return nil
	}
	denial, err := s.policy.AllowComment(ctx, PolicyReqOf(req))
	if err != nil {
		// 判定本身失败是**故障**（不是「不允许」）：原文进日志，对外用归口文案 ——
		// 不能让消费方的数据库原文顺着访客页面漏出去。
		logger.Scene(errScene).
			With("entity_type", req.EntityType).
			With("entity_id", req.EntityID).
			Error(err, "评论差异化规则判定失败（端口实现报错）")
		return errParam(commentenums.ErrInternal)
	}
	if denial != nil {
		// 拒绝文案由**消费方**给出（它的实体、它的措辞）：这里只搬运，
		// 不撞本模块白名单、不翻译 —— 取词发生在 FacingText（那里才知道请求语言）。
		key := strings.TrimSpace(denial.Message)
		fallback := strings.TrimSpace(denial.Fallback)
		if key == "" && fallback == "" {
			// 消费方什么都没给：回落本模块的归口业务文案（而不是 ErrInternal ——
			// 那会把「这条内容不满足评论条件」这个**可行动**的原因吞掉）。
			return errParam(commentenums.ErrNotAllowed)
		}
		return &PolicyDeniedError{Key: key, Fallback: fallback}
	}
	return nil
}

// PolicyDeniedError 消费方的差异化规则给出的拒绝（i18n key + 中文兜底）。
//
// 它**不是**本模块的白名单文案（key 属于别的模块），所以 FacingText 单独识别它，
// 走 i18n.Translate(key, fallback, lang) 取词 —— 而不是拿去撞本模块白名单：
// 撞不上就会被归口成「操作失败」，把「买过才能评」这类**可行动**的原因吞掉。
type PolicyDeniedError struct {
	// Key 消费方的 i18n key（可能为空 —— 那就直接用 Fallback）。
	Key string
	// Fallback 中文原文（Key 未命中词条时的回落）。
	Fallback string
}

// Error 实现 error（日志与 tests 用；值是 key，缺失时用兜底原文）。
//
// 注意它不是**可展示文案**：可展示文案由 FacingText 按请求语言取词后给出。
func (e *PolicyDeniedError) Error() string {
	if e == nil {
		return ""
	}
	if key := strings.TrimSpace(e.Key); key != "" {
		return key
	}
	return strings.TrimSpace(e.Fallback)
}

// PolicyReqOf 把提交请求转成差异化规则的判定入参。
//
// 做成函数（而不是让调用点直接构造结构体字面量）的理由：
// 将来判定需要的字段变多（如「已购买订单号」）时，只有这里要改。
func PolicyReqOf(req *commentdto.SubmitReq) *commentcontract.PolicyReq {
	return &commentcontract.PolicyReq{
		ProjectID:  strings.TrimSpace(req.ProjectID),
		EntityType: strings.TrimSpace(req.EntityType),
		EntityID:   strings.TrimSpace(req.EntityID),
		UserID:     req.UserID,
	}
}

// asPolicyDenied 判定一个错误是不是差异化规则的拒绝（FacingText 用）。
func asPolicyDenied(err error) (*PolicyDeniedError, bool) {
	var denied *PolicyDeniedError
	if errors.As(err, &denied) {
		return denied, true
	}
	return nil, false
}
