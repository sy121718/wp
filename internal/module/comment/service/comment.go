package commentservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

			out = append(out, itemOf(reply, true))
			continue
		}
		out[pos].Replies = append(out[pos].Replies, itemOf(reply, true))
	}
	return out
}

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

func (s *Service) AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}

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

func jsonTime(t time.Time) utils.JSONTime { return utils.NewJSONTime(t) }

func jsonTimePtr(t *time.Time) *utils.JSONTime { return utils.NewJSONTimePtr(t) }

func (s *Service) Submit(ctx context.Context, req *commentdto.SubmitReq) (res *commentdto.SubmitResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}

	if req.UserID == 0 {
		return nil, errParam(commentenums.ErrLoginRequired)
	}

	if err = s.checkSubmitRate(req.UserID, req.IPHash); err != nil {
		logger.Scene(errScene).
			With("user_id", req.UserID).
			With("entity_type", req.EntityType).
			Warn("评论提交被限流拒绝")
		return nil, err
	}

	if err = s.validateTarget(req.ProjectID, req.EntityType, req.EntityID); err != nil {
		return nil, err
	}

	body, err := normalizeBody(req.Body)
	if err != nil {
		return nil, err
	}

	parentID, err := s.resolveParent(ctx, req)
	if err != nil {
		return nil, err
	}

	if err = s.checkPolicy(ctx, req); err != nil {
		return nil, err
	}

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

func (s *Service) checkPolicy(ctx context.Context, req *commentdto.SubmitReq) error {
	if s.policy == nil {
		return nil
	}
	denial, err := s.policy.AllowComment(ctx, PolicyReqOf(req))
	if err != nil {

		logger.Scene(errScene).
			With("entity_type", req.EntityType).
			With("entity_id", req.EntityID).
			Error(err, "评论差异化规则判定失败（端口实现报错）")

		return fmt.Errorf("评论差异化规则判定失败: %w", err)
	}
	if denial != nil {

		key := strings.TrimSpace(denial.Message)
		fallback := strings.TrimSpace(denial.Fallback)
		if key == "" && fallback == "" {

			return errParam(commentenums.ErrNotAllowed)
		}
		return &PolicyDeniedError{Key: key, Fallback: fallback}
	}
	return nil
}

type PolicyDeniedError struct {
	Key string

	Fallback string
}

const policyDeniedFallbackKey = "comment.err.policyDenied"

func (e *PolicyDeniedError) Facing() (key, fallback string) {
	if e == nil {
		return "", ""
	}
	key, fallback = strings.TrimSpace(e.Key), strings.TrimSpace(e.Fallback)
	if key == "" {
		key = policyDeniedFallbackKey
	}
	return key, fallback
}

func (e *PolicyDeniedError) Error() string {
	if e == nil {
		return ""
	}
	if key := strings.TrimSpace(e.Key); key != "" {
		return key
	}
	return strings.TrimSpace(e.Fallback)
}

func PolicyReqOf(req *commentdto.SubmitReq) *commentcontract.PolicyReq {
	return &commentcontract.PolicyReq{
		ProjectID:  strings.TrimSpace(req.ProjectID),
		EntityType: strings.TrimSpace(req.EntityType),
		EntityID:   strings.TrimSpace(req.EntityID),
		UserID:     req.UserID,
	}
}

func asPolicyDenied(err error) (*PolicyDeniedError, bool) {
	var denied *PolicyDeniedError
	if errors.As(err, &denied) {
		return denied, true
	}
	return nil, false
}

func (s *Service) Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if len(req.IDs) == 0 {
		return nil, errParam(commentenums.ErrNothingSelected)
	}

	if len(req.IDs) > commentenums.MaxReviewIDs {
		return nil, errParam(commentenums.ErrInvalidParam)
	}

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

func HashSourceIP(salt, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(salt) + ":" + ip))
	return hex.EncodeToString(sum[:])
}

const (
	submitPerIdentity = 5

	submitPerSource = 15

	submitWindow = time.Minute

	limiterBucketTTL = 10 * time.Minute
)

type submitLimiter struct {
	lmt *limiter.Limiter
}

func newLimiter(limit int, window time.Duration) *submitLimiter {
	if limit <= 0 || window <= 0 {

		return &submitLimiter{}
	}
	lmt := tollbooth.NewLimiter(float64(limit)/window.Seconds(), &limiter.ExpirableOptions{
		DefaultExpirationTTL: limiterBucketTTL,
	})
	lmt.SetBurst(limit)
	return &submitLimiter{lmt: lmt}
}

func (l *submitLimiter) allow(key string) bool {
	if l == nil || l.lmt == nil || key == "" {
		return true
	}
	return !l.lmt.LimitReached(key)
}

func (s *Service) checkSubmitRate(userID uint64, ipHash string) error {
	if !s.identityLimiter.allow("u:" + strconv.FormatUint(userID, 10)) {
		return errParam(commentenums.ErrRateLimited)
	}
	if ipHash != "" && !s.sourceLimiter.allow("ip:"+ipHash) {
		return errParam(commentenums.ErrRateLimited)
	}
	return nil
}
