package commentservice

// 两条列表的**状态口径完全不同**，这是本文件最要紧的一件事：
//
//	· ListApproved（公开面）—— 只出 approved，且状态条件写在 SQL 里、不可由调用方配置；
//	· AdminList（控制面）—— 可筛任意状态（默认全部），因为审核员要看到待审与已驳回。
//
// 把这两件事合成一个方法、用一个 `status` 参数区分，等于把「公开列表只看已通过」
// 变成调用方记得传对参数的事 —— 漏传的后果是**未审核内容直接出现在线上页面**。

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

// 为什么批量用**一条 UPDATE** 而不是「逐条走单条路径」：
//
//   - 审核是**幂等**的状态设置（把一批 id 设成同一个状态），不是「每条都有不同的
//     附加动作」—— 逐条执行只会把 N 次往返摊在请求里，且需要 N 个事务；
//   - 单条 SQL 天然原子：要么这一批都改了，要么一条都没改（不存在「改了一半」的中间态）；
//   - 受影响行数（RowsAffected）如实回带「成功 N 条」—— 与请求条数不等时说明：
//     并发的另一次审核已经把它改成了目标状态（id 仍在、条件仍匹配，但值没变），
//     或者 id 不属于这个工程（RLS 与 WHERE 双重过滤掉了）。
//
// 权限不在这里判：审核是管理面写操作，权限点在路由注册处声明（comment:review），
// 由 Casbin 在进入 handler 之前拦掉（AGENTS.md §认证与鉴权）。service 只判**业务合法性**
// （状态是不是审核动作能设的那个、id 集合是不是空的）。

// 为什么存哈希而不是明文 IP（同 analytics 模块的既有口径）：
//
//   - 目的是**防刷取证**（「同一个来源短期灌了多少条」），不是「知道他是谁」；
//   - 裸哈希在 IPv4 空间（2^32）里等于把明文换个写法存下来 —— 彩虹表一分钟就能反查，
//     所以必须带盐，且盐是**部署级密钥**（不是硬编码常量，否则等于没加盐）；
//   - 明文 IP 一旦落库就会跟着备份、导出、日志走，而它属于个人数据。
//
// 盐由装配层从配置取（`comment.ip_pepper`，未配置时回退会话密钥并告警），
// 与 analytics 的 pepper、cart 的 cookie 密钥同一条「按用途分离密钥」的形态
// （见 routers/assembly.go 的 resolvePurposeSecret）。
//
// 哈希口径放在 service 而不是 inbound：片段层与后台都要用同一个口径，
// 两边各写一遍 sha256 迟早会分叉（表现是「同一台机器在两个入口算出两个来源」，
// 而这件事不会有任何报错）。

// 与 internal/middleware/builtin 的 IP 限流的分工：
//
//   · 中间件那层是**通用**保护（/api/* 与页面的每分钟 N 次），它按 IP 计数、
//     不知道「评论」这件事，也无法按**身份**计数；
//   · 这一层是**能力级**保护：同一账号每分钟最多几条、同一来源每分钟最多几条。
//
// 为什么必须按身份而不是只按 IP：一个脚本用一百个账号刷，按 IP 计数会把它当成
// 「一个正常用户」（每个账号都远未触顶），而按账号计数能在第一条之后就压住它。
// 反过来，只按账号也不行：注册是免费的，换账号的成本是零 —— 两条一起才算防刷。
//
// 为什么是**进程内**令牌桶（而不是 Redis 计数）：与既有 IP 限流同一条口径
// （rate_limit.go 已写明「限流状态为进程内令牌桶，重启即重置；多实例部署需外置存储」）。
// 这里刻意与它保持一致，而不是给评论单独发明一套跨实例计数 —— 两套不同口径的限流
// 出现在同一个请求链上，运维很难解释「为什么这里限得住、那里限不住」。
//
// 口径与代价：**漏过**的窗口是「多实例部署下每个实例各算一份额度」，
// 方向是宽松（不是误伤正常用户）；而它与审核队列一起构成两道闸（先审后发），
// 所以宽松的偏差可接受。真正需要严格全局配额时，把这里换成 pkg/cache 的
// Redis 计数器即可（service 内部唯一改动点）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"gorm.io/gorm"

	"go_wp/internal/module/comment/contract"
	"go_wp/internal/module/comment/dto"
	"go_wp/internal/module/comment/enums"
	"go_wp/internal/module/comment/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// ListApproved 公开列表：某实体下**已通过**的评论（顶层分页 + 一级回复）。
func (s *Service) ListApproved(ctx context.Context, req *commentdto.ListReq) (res *commentdto.ListResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if err = s.validateTarget(req.ProjectID, req.EntityType, req.EntityID); err != nil {
		return nil, err
	}
	page, pageSize := normalizePaging(req.Page, req.PageSize)
	offset := (page - 1) * pageSize

	tops, err := s.model.ListTopLevel(ctx, req.ProjectID, req.EntityType, req.EntityID, pageSize, offset)
	if err != nil {
		return nil, err
	}
	total, err := s.model.CountTopLevel(ctx, req.ProjectID, req.EntityType, req.EntityID)
	if err != nil {
		return nil, err
	}
	topIDs := make([]int64, 0, len(tops))
	for _, top := range tops {
		topIDs = append(topIDs, top.ID)
	}
	replies, err := s.model.ListRepliesOf(ctx, req.ProjectID, req.EntityType, req.EntityID, topIDs)
	if err != nil {
		return nil, err
	}

	res = &commentdto.ListResp{
		Items:    assembleItems(tops, replies),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		HasMore:  int64(offset+len(tops)) < total,
	}
	return res, nil
}

// assembleItems 把「顶层 + 回复」两段查询结果拼成两级结构。
//
// 为什么要两次查询而不是一次查全部再在内存里分组：一次查全部的前提是
// 「这个实体的评论不多」—— 帖子一旦有几千条回复，单次查询会把整段拉进内存，
// 而分页语义也失去意义（回复会跟着顶层一起被切在页边界外）。
func assembleItems(tops, replies []*commentmodel.Entity) []commentdto.Item {
	out := make([]commentdto.Item, 0, len(tops))
	index := make(map[int64]int, len(tops))
	for _, top := range tops {
		index[top.ID] = len(out)
		out = append(out, itemOf(top, false))
	}
	for _, reply := range replies {
		if reply.ParentID == nil {
			continue
		}
		pos, ok := index[*reply.ParentID]
		if !ok {
			// 回复的父评论不在本页（父评论被删 / 分页边界外）：**不丢数据**，
			// 而是把它提到顶层显示 —— 少显示一条用户能看见的评论，比多显示一条更糟。
			out = append(out, itemOf(reply, true))
			continue
		}
		out[pos].Replies = append(out[pos].Replies, itemOf(reply, true))
	}
	return out
}

// itemOf 把一行转成 dto（不含作者名，理由见 dto.Item 的注释）。
func itemOf(e *commentmodel.Entity, isReply bool) commentdto.Item {
	if e == nil {
		return commentdto.Item{}
	}
	return commentdto.Item{
		ID:        e.ID,
		Body:      e.Body,
		IsReply:   isReply,
		CreatedAt: jsonTime(e.CreateTime),
	}
}

// AdminList 后台审核队列（控制面，可按状态 / 实体类型 / 关键词筛选）。
func (s *Service) AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	// 工程必填：评论按工程隔离（RLS 作用域也需要它），后台页因此必须先选工程。
	// 这里只校验工程 —— 队列本身不带实体 id（它是跨实体的审核视图）。
	if err = s.validateProject(req.ProjectID); err != nil {
		return nil, err
	}
	q := commentmodel.ReviewQuery{ProjectID: strings.TrimSpace(req.ProjectID)}
	if status := strings.TrimSpace(req.Status); status != "" {
		if !commentenums.IsValidStatus(status) {
			return nil, errParam(commentenums.ErrInvalidParam)
		}
		q.Statuses = []string{status}
	}
	q.EntityType = strings.TrimSpace(req.EntityType)
	if q.EntityType != "" && !s.IsRegisteredEntityType(q.EntityType) {
		return nil, errParam(commentenums.ErrEntityTypeUnknown)
	}
	kw := strings.TrimSpace(req.Keyword)
	if len([]rune(kw)) > commentenums.MaxKeywordLen {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	q.Keyword = kw

	page, pageSize := normalizePaging(req.Page, req.PageSize)
	q.Limit, q.Offset = pageSize, (page-1)*pageSize

	rows, total, err := s.model.ListForReview(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]commentdto.AdminItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminItemOf(row))
	}
	return &commentdto.AdminListResp{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// adminItemOf 后台行转换（状态展示名由 handler 按语言填，service 不取词）。
func adminItemOf(e *commentmodel.Entity) commentdto.AdminItem {
	if e == nil {
		return commentdto.AdminItem{}
	}
	item := commentdto.AdminItem{
		ID:         e.ID,
		Body:       e.Body,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		UserID:     e.UserID,
		Status:     e.Status,
		CreateTime: jsonTime(e.CreateTime),
		IsReply:    e.ParentID != nil,
	}
	if e.ReviewedAt != nil {
		item.ReviewedAt = jsonTimePtr(e.ReviewedAt)
	}
	item.ReviewerID = e.ReviewedBy
	return item
}

// jsonTime 转对外 JSON 时间（只到秒；形态统一由 pkg/utils 决定）。
func jsonTime(t time.Time) utils.JSONTime { return utils.NewJSONTime(t) }

// jsonTimePtr 可空时间转换（nil 进 nil 出）。
func jsonTimePtr(t *time.Time) *utils.JSONTime { return utils.NewJSONTimePtr(t) }

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

// Review 批量设置审核状态（approved / rejected）。
func (s *Service) Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if len(req.IDs) == 0 {
		return nil, errParam(commentenums.ErrNothingSelected)
	}
	// 集合上限：handler 已经经 shell.BulkIDs 去重限过量，这里再判一次 ——
	// service 是契约的最终执行者，不能押注在调用方身上（将来多一个 API 调用点就漏了）。
	if len(req.IDs) > commentenums.MaxReviewIDs {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	// 审核动作只能设 approved / rejected（把一条评论「改成待审」不是判断结果）。
	if !commentenums.IsReviewableStatus(req.Status) {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if err = s.validateProject(req.ProjectID); err != nil {
		return nil, err
	}
	affected, err := s.model.UpdateStatus(ctx, req.ProjectID, req.IDs, req.Status, req.ReviewerID)
	if err != nil {
		logger.Scene(errScene).
			With("reviewer_id", req.ReviewerID).
			With("status", req.Status).
			With("count", len(req.IDs)).
			Error(err, "批量审核写入失败")
		return nil, err
	}
	logger.Scene(errScene).
		With("reviewer_id", req.ReviewerID).
		With("status", req.Status).
		With("requested", len(req.IDs)).
		With("affected", affected).
		Info("批量审核完成")
	return &commentdto.ReviewResp{Affected: affected, Status: req.Status}, nil
}

// HashSourceIP 计算来源 IP 的带盐哈希（sha256(salt + ":" + ip) 的十六进制小写）。
//
// 输入为空时返回空串：调用方据此略过来源维度的限流与取证（而不是把全空 IP
// 哈希成同一个值 —— 那会让「所有拿不到 IP 的请求」共用一个来源额度）。
func HashSourceIP(salt, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(salt) + ":" + ip))
	return hex.EncodeToString(sum[:])
}

const (
	// submitPerIdentity 同一账号在 submitWindow 内允许的提交次数。
	//
	// 取值依据：正常访客写一条评论要几十秒（读 + 打字 + 提交），5 条/分钟已经远高于
	// 真实节奏；而对脚本来说，这个上限把「一分钟灌几千条」压到 5 条。
	submitPerIdentity = 5
	// submitPerSource 同一来源（IP 哈希）在 submitWindow 内允许的提交次数。
	//
	// 比账号额度宽 3 倍：同一个出口 IP 后面可能坐着多个人（公司网络、校园网、NAT），
	// 按账号额度卡来源会误伤「同事同时在同一个站评论」；而 15 条/分钟仍然能挡住
	// 「一个脚本换了十几个账号继续刷」。
	submitPerSource = 15
	// submitWindow 限流窗口（两条额度共用同一个窗口长度）。
	submitWindow = time.Minute
	// limiterBucketTTL 不活跃 key 的令牌桶存活时间（与 builtin.rateLimitBucketTTL 同值同理由：
	// 长期不活跃的 key 不该在内存里无限累积）。
	limiterBucketTTL = 10 * time.Minute
)

// submitLimiter 能力级令牌桶（tollbooth 的薄封装）。
//
// 为什么不让 service 直接持有 *limiter.Limiter：LimitReached 的语义（「取一次令牌，
// 返回是否已耗尽」）与「限流判定」这件事之间差一层翻译 —— 封一层之后，测试可以
// 直接构造一个很小的额度来验证第 N+1 次被拒，而不必理解 tollbooth 的 API。
type submitLimiter struct {
	lmt *limiter.Limiter
}

// newLimiter 构造一个按 key 计数的令牌桶。
//
// burst 必须显式设为 limit：tollbooth 默认 burst=0 会让**所有**请求立即被拒
// （rate_limit.go 的注释记过这个坑）。
func newLimiter(limit int, window time.Duration) *submitLimiter {
	if limit <= 0 || window <= 0 {
		// 非法参数：不构造限流器（判定恒放行）。
		return &submitLimiter{}
	}
	lmt := tollbooth.NewLimiter(float64(limit)/window.Seconds(), &limiter.ExpirableOptions{
		DefaultExpirationTTL: limiterBucketTTL,
	})
	lmt.SetBurst(limit)
	return &submitLimiter{lmt: lmt}
}

// allow 取一次令牌；返回 false 表示额度已耗尽（本次请求应被拒绝）。
func (l *submitLimiter) allow(key string) bool {
	if l == nil || l.lmt == nil || key == "" {
		return true
	}
	return !l.lmt.LimitReached(key)
}

// checkSubmitRate 提交前的限流判定（命中任一维度即拒绝）。
//
// 顺序：**身份额度先判、来源额度后判**。两个额度都被消耗时才拒绝，
// 这是刻意的：只判身份会让「多账号刷」完全绕过，只判来源会让 NAT 后面的正常
// 用户互相拖累。
//
// ipHash 为空时跳过来源维度（拿不到 IP 的请求在真实部署里不该存在，
// 但测试与反代误配下会出现 —— 此时按身份判仍然有效，不因为一个取不到的字段全放开）。
func (s *Service) checkSubmitRate(userID uint64, ipHash string) error {
	if !s.identityLimiter.allow("u:" + strconv.FormatUint(userID, 10)) {
		return errParam(commentenums.ErrRateLimited)
	}
	if ipHash != "" && !s.sourceLimiter.allow("ip:"+ipHash) {
		return errParam(commentenums.ErrRateLimited)
	}
	return nil
}
