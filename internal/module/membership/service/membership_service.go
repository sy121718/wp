package membershipservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	tierNameMaxLen = 60

	remarkMaxLen = 200

	defaultPageSize = 20

	maxPageSize = 200

	errScene = "membership"
)

type Service struct {
	model *membershipmodel.Model

	projects projectcontract.ProjectService

	purchases membershipcontract.PurchaseSource

	// recalcMu / recalcStop 归实例持有日结停止句柄：
	// 重复 Start 返回同一幂等句柄，Stop 只等任务真正退出。
	recalcMu   sync.Mutex
	recalcStop func()
}

func NewService(m *membershipmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{model: m, projects: projects}
}

var (
	_ membershipcontract.Reader              = (*Service)(nil)
	_ membershipcontract.Assigner            = (*Service)(nil)
	_ membershipcontract.AssignmentAdminPort = (*Service)(nil)
	_ membershipcontract.TierAdminPort       = (*Service)(nil)
	_ membershipcontract.RecalcPort          = (*Service)(nil)
	_ membershipcontract.MembershipService   = (*Service)(nil)
)

func (s *Service) SetPurchaseSource(port membershipcontract.PurchaseSource) {
	if s == nil {
		return
	}
	s.purchases = port
}

func requireProject(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return membershipenums.Err(membershipenums.ErrProjectRequired)
	}
	return nil
}

func requireUser(userID uint64) error {
	if userID == 0 {
		return membershipenums.Err(membershipenums.ErrUserRequired)
	}
	return nil
}

func requireTier(tierID int64) error {
	if tierID <= 0 {
		return membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return nil
}

func normalizeTierName(raw string) (name string, err error) {
	name = strings.TrimSpace(raw)
	if name == "" {
		return "", membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	if len([]rune(name)) > tierNameMaxLen {
		return "", membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return name, nil
}

func normalizeRemark(raw string) (string, error) {
	remark := strings.TrimSpace(raw)
	if len([]rune(remark)) > remarkMaxLen {
		return "", membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return remark, nil
}

func validateThreshold(amount int64) error {
	if amount < 0 {
		return membershipenums.Err(membershipenums.ErrThresholdInvalid)
	}
	return nil
}

func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return page, size
}

func normalizeEntitlements(items []membershipdto.EntitlementReq) (rows []*membershipmodel.EntitlementEntity, err error) {
	byKind := map[string]int64{}
	for _, item := range items {
		kind := strings.TrimSpace(item.Kind)
		switch kind {
		case membershipenums.KindFreeShipping:
			if item.ValueInt != 0 && item.ValueInt != 1 {
				return nil, membershipenums.Err(membershipenums.ErrEntitlementValue)
			}
		case membershipenums.KindDiscount:
			if item.ValueInt < 1 || item.ValueInt > 100 {
				return nil, membershipenums.Err(membershipenums.ErrEntitlementValue)
			}
		default:
			return nil, membershipenums.Err(membershipenums.ErrEntitlementKind)
		}
		byKind[kind] = item.ValueInt
	}

	rows = make([]*membershipmodel.EntitlementEntity, 0, len(byKind))
	for _, kind := range membershipenums.EntitlementKinds {
		if value, ok := byKind[kind]; ok {
			rows = append(rows, &membershipmodel.EntitlementEntity{Kind: kind, ValueInt: value})
		}
	}
	return rows, nil
}

func selectTierBySpend(tiers []*membershipmodel.TierEntity, spent int64) *membershipmodel.TierEntity {
	for _, tier := range tiers {
		if tier == nil {
			continue
		}
		if spent >= tier.ThresholdAmount {
			return tier
		}
	}
	return nil
}

func pickDefaultTier(tiers []*membershipmodel.TierEntity) *membershipmodel.TierEntity {
	for _, tier := range tiers {
		if tier != nil && tier.IsDefault {
			return tier
		}
	}
	return nil
}

func toJSONTime(t time.Time) utils.JSONTime { return utils.JSONTime(t) }

func toTierResp(e *membershipmodel.TierEntity, ents []*membershipmodel.EntitlementEntity) *membershipdto.TierResp {
	res := &membershipdto.TierResp{
		ID:              e.ID,
		ProjectID:       e.ProjectID,
		Name:            e.Name,
		SortOrder:       e.SortOrder,
		ThresholdAmount: e.ThresholdAmount,
		IsDefault:       e.IsDefault,
		Remark:          e.Remark,
		Entitlements:    make([]membershipdto.EntitlementResp, 0, len(ents)),
		CreateTime:      toJSONTime(e.CreateTime),
		UpdateTime:      toJSONTime(e.UpdateTime),
	}
	for _, ent := range ents {
		res.Entitlements = append(res.Entitlements, membershipdto.EntitlementResp{
			Kind: ent.Kind, ValueInt: ent.ValueInt,
		})
	}
	return res
}

func toAssignmentResp(e *membershipmodel.AssignmentEntity, tierName string) *membershipdto.AssignmentResp {
	return &membershipdto.AssignmentResp{
		ID:         e.ID,
		ProjectID:  e.ProjectID,
		UserID:     e.UserID,
		TierID:     e.TierID,
		TierName:   tierName,
		Source:     e.Source,
		AssignedAt: toJSONTime(e.AssignedAt),
		UpdateTime: toJSONTime(e.UpdateTime),
	}
}

func tierNameOf(tiers []*membershipmodel.TierEntity, tierID int64) string {
	for _, tier := range tiers {
		if tier != nil && tier.ID == tierID {
			return tier.Name
		}
	}
	return ""
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func uniqueViolationOn(err error, indexNames ...string) bool {
	if err == nil || !database.IsUniqueViolation(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, name := range indexNames {
		if strings.Contains(msg, strings.ToLower(name)) {
			return true
		}
	}
	return false
}

func facingDetailSep(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return "："
	}
	return ": "
}

func (s *Service) projectExists(ctx context.Context, projectID string) error {
	if s.projects == nil {
		return nil
	}
	exists, err := s.projects.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return membershipenums.Err(membershipenums.ErrProjectRequired)
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, req *membershipdto.ResolveReq) (res *membershipdto.MembershipResp, err error) {
	if err = requireProject(req.ProjectID); err != nil {
		return nil, err
	}
	if err = requireUser(req.UserID); err != nil {
		return nil, err
	}

	var (
		tier       *membershipmodel.TierEntity
		ents       []*membershipmodel.EntitlementEntity
		source     string
		assignedAt *time.Time
		defaulted  bool
	)

	err = s.model.TransactionScoped(ctx, req.ProjectID, func(tx *gorm.DB) error {
		resolved, src, at, usedDefault, rerr := s.resolveTierTx(ctx, tx, req.ProjectID, req.UserID)
		if rerr != nil {
			return rerr
		}
		list, lerr := s.model.ListByTierTx(ctx, tx, resolved.ID)
		if lerr != nil {
			return lerr
		}
		tier, ents, source, assignedAt, defaulted = resolved, list, src, at, usedDefault
		return nil
	})
	if err != nil {
		return nil, err
	}

	res = &membershipdto.MembershipResp{
		UserID:        req.UserID,
		ProjectID:     req.ProjectID,
		TierID:        tier.ID,
		TierName:      tier.Name,
		Source:        source,
		IsDefaultTier: defaulted,
	}
	if assignedAt != nil {
		jt := toJSONTime(*assignedAt)
		res.AssignedAt = &jt
	}
	for _, ent := range ents {
		switch ent.Kind {
		case membershipenums.KindFreeShipping:
			res.FreeShipping = ent.ValueInt == 1
		case membershipenums.KindDiscount:
			res.DiscountPercent = ent.ValueInt
		}
	}
	return res, nil
}

func (s *Service) resolveTierTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) (
	tier *membershipmodel.TierEntity, source string, assignedAt *time.Time, defaulted bool, err error) {
	assign, aerr := s.model.GetAssignmentTx(ctx, tx, projectID, userID)
	switch {
	case aerr == nil:
		row, terr := s.model.GetTierTx(ctx, tx, projectID, assign.TierID)
		if terr == nil {
			at := assign.AssignedAt
			return row, assign.Source, &at, false, nil
		}
		if !isNotFound(terr) {
			return nil, "", nil, false, terr
		}

		logger.Scene(errScene).
			With("projectId", projectID).With("userId", userID).With("tierId", assign.TierID).
			Warn("会员归属指向的等级已不可见，本次回退到默认等级（数据不一致，请检查该归属行）")
	case isNotFound(aerr):

	default:
		return nil, "", nil, false, aerr
	}

	def, derr := s.model.GetDefaultTierTx(ctx, tx, projectID)
	if derr != nil {
		if isNotFound(derr) {

			return nil, "", nil, false, membershipenums.ErrDetail(
				membershipenums.ErrDefaultTierMissing, projectID)
		}
		return nil, "", nil, false, derr
	}
	return def, "", nil, true, nil
}
