package adminservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) RuleDetail(ctx context.Context, req *admindto.RuleDetailReq) (res *admindto.RuleDetailResp, err error) {

	entity, err := s.drm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, adminenums.Err(adminenums.ErrRuleNotFound)
	}

	remark := ""
	if entity.Remark != nil {
		remark = *entity.Remark
	}
	createTime := ""
	if entity.CreateTime != nil {
		createTime = entity.CreateTime.Format("2006-01-02 15:04:05")
	}
	updateTime := ""
	if entity.UpdateTime != nil {
		updateTime = entity.UpdateTime.Format("2006-01-02 15:04:05")
	}

	config, err := decodeRuleConfig(entity.Config)
	if err != nil {
		return nil, err
	}

	return &admindto.RuleDetailResp{
		ID:         entity.ID,
		RuleName:   entity.RuleName,
		Domain:     entity.Domain,
		Config:     config,
		Status:     entity.Status,
		Remark:     remark,
		CreateBy:   entity.CreateBy,
		CreateTime: createTime,
		UpdateBy:   entity.UpdateBy,
		UpdateTime: updateTime,
	}, nil
}

func (s *Service) RuleCreate(ctx context.Context, req *admindto.RuleCreateReq) error {

	validated, err := validateRuleConfig(req.Domain, req.Config.ToRuleConfig())
	if err != nil {
		return err
	}
	config, err := encodeRuleConfig(validated)
	if err != nil {
		return err
	}

	entity := &adminmodel.SysRuleEntity{
		RuleName: req.RuleName,
		Domain:   req.Domain,
		Config:   config,
		Status:   req.Status,
	}

	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err := s.drm.Create(ctx, entity); err != nil {
		return err
	}

	s.reloadDataRuleSnapshotAfterWrite("规则新建")
	return nil
}

func (s *Service) RuleUpdate(ctx context.Context, req *admindto.RuleUpdateReq) error {

	entity, err := s.drm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return adminenums.Err(adminenums.ErrRuleNotFound)
	}

	validated, err := validateRuleConfig(req.Domain, req.Config.ToRuleConfig())
	if err != nil {
		return err
	}
	config, err := encodeRuleConfig(validated)
	if err != nil {
		return err
	}

	entity.RuleName = req.RuleName
	entity.Domain = req.Domain
	entity.Config = config
	entity.Status = req.Status

	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	if err := s.drm.Update(ctx, entity); err != nil {
		return err
	}

	s.reloadDataRuleSnapshotAfterWrite("规则更新")
	return nil
}

func (s *Service) RuleDelete(ctx context.Context, req *admindto.RuleDeleteReq) error {
	ids := uniqueRuleIDs(req.IDs)
	if len(ids) == 0 {
		return nil
	}
	if err := s.drm.Transaction(ctx, func(tx *gorm.DB) error {
		locked, err := s.drm.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if len(locked) != len(ids) {
			return adminenums.Err(adminenums.ErrRuleNotFound)
		}

		for _, id := range ids {
			if err = s.dram.DeleteByRuleIDTx(ctx, tx, id); err != nil {
				return err
			}
		}
		if _, err = s.drm.DeleteByIDsTx(ctx, tx, ids); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	s.reloadDataRuleSnapshotAfterWrite("规则删除")
	return nil
}

func uniqueRuleIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func validateRuleConfig(domain string, cfg datarule.RuleConfig) (datarule.RuleConfig, error) {
	declared, ok := datarule.GetDomain(domain)
	if !ok {
		return datarule.RuleConfig{}, adminenums.Err(adminenums.ErrInvalidDomain)
	}

	allowed := make(map[string]datarule.FieldDef, len(declared.WhiteList))
	for _, field := range declared.WhiteList {
		allowed[field.Field] = field
	}

	omitFields := make([]string, 0, len(cfg.OmitFields))
	for _, field := range cfg.OmitFields {
		if _, ok := allowed[field]; !ok {
			return datarule.RuleConfig{}, fmt.Errorf("%s: %s", adminenums.ErrRuleFieldNotAllowed, field)
		}
		omitFields = append(omitFields, field)
	}

	groups := make([]datarule.ConditionGroup, 0, len(cfg.ConditionGroups))
	for _, group := range cfg.ConditionGroups {

		logic := strings.TrimSpace(group.Logic)
		if logic != datarule.LogicAnd && logic != datarule.LogicOr {
			return datarule.RuleConfig{}, fmt.Errorf("%s: %s", adminenums.ErrRuleLogicNotAllowed, group.Logic)
		}

		conditions := make([]datarule.Condition, 0, len(group.Conditions))
		for _, condition := range group.Conditions {
			field, ok := allowed[condition.Field]
			if !ok {
				return datarule.RuleConfig{}, fmt.Errorf("%s: %s", adminenums.ErrRuleFieldNotAllowed, condition.Field)
			}

			op := strings.TrimSpace(condition.Op)
			if strings.ToUpper(op) != op || !containsOperator(field.Operators, op) {
				return datarule.RuleConfig{}, fmt.Errorf("%s: %s.%s", adminenums.ErrRuleOpNotAllowed, field.Field, condition.Op)
			}
			conditions = append(conditions, datarule.Condition{
				Field: field.Field,
				Op:    op,
				Value: condition.Value,
			})
		}
		groups = append(groups, datarule.ConditionGroup{Logic: logic, Conditions: conditions})
	}

	return datarule.RuleConfig{OmitFields: omitFields, ConditionGroups: groups}, nil
}

func containsOperator(operators []string, op string) bool {
	for _, item := range operators {
		if strings.EqualFold(strings.TrimSpace(item), op) {
			return true
		}
	}
	return false
}

func encodeRuleConfig(config datarule.RuleConfig) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeRuleConfig(config string) (admindto.RuleConfigDTO, error) {
	var result admindto.RuleConfigDTO
	if err := json.Unmarshal([]byte(config), &result); err != nil {
		return admindto.RuleConfigDTO{}, err
	}
	return result, nil
}

func (s *Service) RuleList(ctx context.Context, req *admindto.RuleListReq) (res *admindto.RuleListResp, err error) {

	query := s.drm.DB(ctx)

	if req.Domain != "" {
		query = query.Where("domain = ?", req.Domain)
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []admindto.RuleItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	if items == nil {
		items = []admindto.RuleItem{}
	}

	return &admindto.RuleListResp{Total: total, List: items}, nil
}

func (s *Service) RuleSchemaList(ctx context.Context) (res []admindto.RuleDomainItem, err error) {

	domains := datarule.GetRegisteredDomains()
	res = make([]admindto.RuleDomainItem, 0, len(domains))
	for _, d := range domains {
		res = append(res, admindto.RuleDomainItem{
			Domain:      d.Domain,
			DomainLabel: d.DomainLabel,
			TableName:   d.TableName,
		})
	}
	return res, nil
}

func (s *Service) RuleSchemaDetail(ctx context.Context, req *admindto.RuleSchemaDetailReq) (res *admindto.RuleDomainDetail, err error) {

	domains := datarule.GetRegisteredDomains()
	for _, d := range domains {
		if d.Domain == req.Domain {

			fields := make([]admindto.RuleFieldDef, 0, len(d.WhiteList))
			for _, f := range d.WhiteList {
				fields = append(fields, admindto.RuleFieldDef{
					Field:     f.Field,
					Label:     f.Label,
					Operators: f.Operators,
				})
			}
			return &admindto.RuleDomainDetail{
				Domain:      d.Domain,
				DomainLabel: d.DomainLabel,
				TableName:   d.TableName,
				Fields:      fields,
			}, nil
		}
	}

	return nil, nil
}

func (s *Service) GetRules(ctx context.Context, user *datarule.UserContext, domain string) ([]datarule.RuleConfig, error) {
	if user == nil || user.UserID == 0 {
		return nil, nil
	}

	snap := s.currentDataRuleSnapshot()
	if snap == nil {
		loaded, err := s.ensureDataRuleSnapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("加载数据权限规则快照失败: %w", err)
		}
		snap = loaded
	}

	domainRules, ok := snap.domains[domain]
	if !ok || domainRules == nil {
		return nil, nil
	}

	roleIDs := make([]uint64, 0, len(user.Roles))
	for _, code := range user.Roles {
		if id, ok := snap.roleIDs[code]; ok {
			roleIDs = append(roleIDs, id)
		}
	}

	ancestorDeptIDs := snap.deptAncestors[user.DeptID]

	matchedRuleIDs := matchAssignmentRuleIDs(
		domainRules.assignments, user.UserID, user.DeptID, roleIDs, ancestorDeptIDs,
	)
	if len(matchedRuleIDs) == 0 {
		return nil, nil
	}

	configByID := make(map[uint64]string, len(domainRules.rules))
	for _, rule := range domainRules.rules {
		configByID[rule.id] = rule.config
	}

	result := make([]datarule.RuleConfig, 0, len(matchedRuleIDs))
	for _, ruleID := range matchedRuleIDs {
		configStr, ok := configByID[ruleID]
		if !ok {
			continue
		}
		var config datarule.RuleConfig
		if err := json.Unmarshal([]byte(configStr), &config); err != nil {
			return nil, fmt.Errorf("解析数据规则 %d 配置失败: %w", ruleID, err)
		}
		result = append(result, config)
	}

	return result, nil
}

func (s *Service) RuleAssignmentList(ctx context.Context, req *admindto.RuleAssignmentListReq) (res *admindto.RuleAssignmentListResp, err error) {

	entities, err := s.dram.ListByRuleID(ctx, req.RuleID)
	if err != nil {
		return nil, err
	}

	list := make([]admindto.RuleAssignmentResp, 0, len(entities))
	for _, e := range entities {
		createTime := ""
		if e.CreateTime != nil {
			createTime = e.CreateTime.Format("2006-01-02 15:04:05")
		}
		list = append(list, admindto.RuleAssignmentResp{
			ID:          e.ID,
			RuleID:      e.RuleID,
			TargetType:  e.TargetType,
			TargetID:    e.TargetID,
			TargetScope: e.TargetScope,
			CreateTime:  createTime,
		})
	}

	return &admindto.RuleAssignmentListResp{List: list}, nil
}

func (s *Service) RuleAssignmentSave(ctx context.Context, req *admindto.RuleAssignmentSaveReq) error {

	if _, err := s.drm.GetByID(ctx, req.RuleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return adminenums.Err(adminenums.ErrRuleNotFound)
		}
		return err
	}
	seen := make(map[[2]uint64]struct{}, len(req.Assignments))
	now := time.Now()
	entities := make([]adminmodel.SysRuleAssignmentEntity, 0, len(req.Assignments))
	for _, a := range req.Assignments {
		if a.TargetID == 0 {
			return adminenums.Err(adminenums.ErrInvalidAssignment)
		}

		targetScope := adminmodel.AssignmentTargetScopeNone
		switch a.TargetType {
		case adminmodel.AssignmentTargetTypeRole, adminmodel.AssignmentTargetTypeUser:
		case adminmodel.AssignmentTargetTypeDept:
			if a.TargetScope != adminmodel.AssignmentTargetScopeSelf &&
				a.TargetScope != adminmodel.AssignmentTargetScopeSelfAndChildren {
				return adminenums.Err(adminenums.ErrInvalidAssignment)
			}
			targetScope = a.TargetScope
		default:
			return adminenums.Err(adminenums.ErrInvalidAssignment)
		}

		key := [2]uint64{uint64(a.TargetType), a.TargetID}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		entities = append(entities, adminmodel.SysRuleAssignmentEntity{
			RuleID:      req.RuleID,
			TargetType:  a.TargetType,
			TargetID:    a.TargetID,
			TargetScope: targetScope,
			CreateTime:  &now,
		})
	}

	if err := s.dram.ReplaceByRuleID(ctx, req.RuleID, entities); err != nil {
		return err
	}

	s.reloadDataRuleSnapshotAfterWrite("规则分配保存")
	return nil
}

const (
	DataRuleSnapshotRefreshInterval = 5 * time.Minute

	dataruleSnapshotLoadTimeout = 30 * time.Second
)

type dataruleRule struct {
	id     uint64
	config string
}

type dataruleAssignment struct {
	ruleID      uint64
	targetType  int
	targetID    uint64
	targetScope int
}

type dataruleDomain struct {
	rules       []dataruleRule
	assignments []dataruleAssignment
}

type dataruleSnapshot struct {
	domains map[string]*dataruleDomain

	roleIDs map[string]uint64

	deptAncestors map[uint64][]uint64

	deptSubtrees map[uint64][]uint64

	loadedAt time.Time
}

type dataRuleSnapshotLoader func(ctx context.Context) (*dataruleSnapshot, error)

type dataruleSnapshotStore struct {
	current atomic.Pointer[dataruleSnapshot]
	mu      sync.Mutex
}

func (s *Service) currentDataRuleSnapshot() *dataruleSnapshot {
	if s == nil || s.ruleSnapshot == nil {
		return nil
	}
	return s.ruleSnapshot.current.Load()
}

func (s *Service) DeptSubtreeIDsFromSnapshot(deptID uint64) []uint64 {
	snap := s.currentDataRuleSnapshot()
	if snap == nil || deptID == 0 {
		return nil
	}
	return snap.deptSubtrees[deptID]
}

func matchAssignmentRuleIDs(
	assignments []dataruleAssignment,
	userID, deptID uint64,
	roleIDs, ancestorDeptIDs []uint64,
) []uint64 {
	if len(assignments) == 0 {
		return nil
	}
	seen := make(map[uint64]struct{}, len(assignments))
	result := make([]uint64, 0, len(assignments))
	for _, a := range assignments {
		if !assignmentMatches(a, userID, deptID, roleIDs, ancestorDeptIDs) {
			continue
		}
		if _, ok := seen[a.ruleID]; ok {
			continue
		}
		seen[a.ruleID] = struct{}{}
		result = append(result, a.ruleID)
	}
	return result
}

func assignmentMatches(a dataruleAssignment, userID, deptID uint64, roleIDs, ancestorDeptIDs []uint64) bool {
	switch a.targetType {
	case adminmodel.AssignmentTargetTypeUser:
		return a.targetID == userID
	case adminmodel.AssignmentTargetTypeRole:
		return len(roleIDs) > 0 && containsDeptOrRoleID(roleIDs, a.targetID)
	case adminmodel.AssignmentTargetTypeDept:
		if deptID != 0 && a.targetID == deptID {
			return true
		}
		return len(ancestorDeptIDs) > 0 &&
			a.targetScope == adminmodel.AssignmentTargetScopeSelfAndChildren &&
			containsDeptOrRoleID(ancestorDeptIDs, a.targetID)
	default:
		return false
	}
}

func containsDeptOrRoleID(ids []uint64, id uint64) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

func (s *Service) ensureDataRuleSnapshot(ctx context.Context) (*dataruleSnapshot, error) {
	if s == nil || s.ruleSnapshot == nil {
		return nil, errors.New("数据权限规则快照存储未初始化")
	}
	s.ruleSnapshot.mu.Lock()
	defer s.ruleSnapshot.mu.Unlock()

	if snap := s.ruleSnapshot.current.Load(); snap != nil {
		return snap, nil
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctxOrDefault(ctx)), dataruleSnapshotLoadTimeout)
	defer cancel()
	snap, err := s.buildAndStoreDataRuleSnapshot(loadCtx)
	if err != nil {
		logger.Scene("admin").
			With("err", err).
			Error(err, "数据权限快照首次加载失败：本次查询已拒绝（fail-closed），下次命中请求会重试")
		return nil, err
	}
	return snap, nil
}

func (s *Service) LoadDataRuleSnapshot(ctx context.Context) error {
	if s == nil || s.ruleSnapshot == nil {
		return nil
	}

	s.ruleSnapshot.mu.Lock()
	defer s.ruleSnapshot.mu.Unlock()

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctxOrDefault(ctx)), dataruleSnapshotLoadTimeout)
	defer cancel()
	_, err := s.buildAndStoreDataRuleSnapshot(loadCtx)
	return err
}

func (s *Service) buildAndStoreDataRuleSnapshot(ctx context.Context) (*dataruleSnapshot, error) {
	start := time.Now()
	snap, err := s.loadDataRuleSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	s.ruleSnapshot.current.Store(snap)
	logger.Scene("admin").
		With("domains", len(snap.domains)).
		With("roles", len(snap.roleIDs)).
		With("depts", len(snap.deptSubtrees)).
		With("cost_ms", time.Since(start).Milliseconds()).
		Info("数据权限快照加载完成")
	return snap, nil
}

func (s *Service) StartDataRuleSnapshotAutoRefresh(ctx context.Context) {
	if s == nil || s.ruleSnapshot == nil {
		return
	}
	if utils.IsTestProcess() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		ticker := time.NewTicker(DataRuleSnapshotRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				logger.Scene("admin").Info("数据权限快照定时刷新已停止")
				return
			case <-ticker.C:
				if s.currentDataRuleSnapshot() == nil {
					continue
				}
				s.reloadDataRuleSnapshot(ctx, "定时兜底刷新")
			}
		}
	}()
}

func (s *Service) reloadDataRuleSnapshot(ctx context.Context, trigger string) {
	if err := s.LoadDataRuleSnapshot(ctx); err != nil {
		logger.Scene("admin").
			With("trigger", trigger).
			With("err", err).
			Error(err, "数据权限快照重建失败：沿用上一份快照（查询期继续按旧规则拦截，定时兜底会再试）")
	}
}

func (s *Service) reloadDataRuleSnapshotAfterWrite(trigger string) {
	if s == nil || s.ruleSnapshot == nil {
		return
	}
	s.reloadDataRuleSnapshot(context.Background(), trigger)
}

func ctxOrDefault(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Service) loadDataRuleSnapshot(ctx context.Context) (*dataruleSnapshot, error) {
	if s.ruleSnapshotLoader != nil {
		return s.ruleSnapshotLoader(ctx)
	}
	return s.loadDataRuleSnapshotFromDB(ctx)
}

func (s *Service) loadDataRuleSnapshotFromDB(ctx context.Context) (*dataruleSnapshot, error) {
	ruleTable := adminmodel.SysRuleEntity{}.TableName()
	assignmentTable := adminmodel.SysRuleAssignmentEntity{}.TableName()

	var ruleRows []struct {
		ID          uint64  `gorm:"column:id"`
		Domain      string  `gorm:"column:domain"`
		Config      string  `gorm:"column:config"`
		TargetType  *int    `gorm:"column:target_type"`
		TargetID    *uint64 `gorm:"column:target_id"`
		TargetScope *int    `gorm:"column:target_scope"`
	}
	if err := s.drm.DB(ctx).
		Select(ruleTable+".id AS id, "+ruleTable+".domain AS domain, "+ruleTable+".config AS config, "+
			"a.target_type AS target_type, a.target_id AS target_id, a.target_scope AS target_scope").
		Joins("LEFT JOIN "+assignmentTable+" AS a ON a.rule_id = "+ruleTable+".id").
		Where(ruleTable+".status = ?", adminmodel.RuleStatusEnabled).
		Scan(&ruleRows).Error; err != nil {
		return nil, err
	}

	domains := make(map[string]*dataruleDomain)
	seenRules := make(map[string]map[uint64]struct{})
	for _, row := range ruleRows {
		domain, ok := domains[row.Domain]
		if !ok {
			domain = &dataruleDomain{}
			domains[row.Domain] = domain
			seenRules[row.Domain] = make(map[uint64]struct{})
		}

		if _, ok := seenRules[row.Domain][row.ID]; !ok {
			seenRules[row.Domain][row.ID] = struct{}{}
			domain.rules = append(domain.rules, dataruleRule{id: row.ID, config: row.Config})
		}
		if row.TargetType == nil || row.TargetID == nil || row.TargetScope == nil {
			continue
		}
		domain.assignments = append(domain.assignments, dataruleAssignment{
			ruleID:      row.ID,
			targetType:  *row.TargetType,
			targetID:    *row.TargetID,
			targetScope: *row.TargetScope,
		})
	}

	var roleRows []struct {
		ID       uint64 `gorm:"column:id"`
		RoleCode string `gorm:"column:role_code"`
	}
	if err := s.rm.DB(ctx).
		Select("id, role_code").
		Where("status = ?", adminmodel.RoleStatusEnabled).
		Scan(&roleRows).Error; err != nil {
		return nil, err
	}
	roleIDs := make(map[string]uint64, len(roleRows))
	for _, row := range roleRows {
		roleIDs[row.RoleCode] = row.ID
	}

	var deptRows []struct {
		ID        uint64 `gorm:"column:id"`
		Ancestors string `gorm:"column:ancestors"`
	}
	if err := s.dm.DB(ctx).
		Select("id, ancestors").
		Scan(&deptRows).Error; err != nil {
		return nil, err
	}
	deptAncestors := make(map[uint64][]uint64, len(deptRows))
	deptSubtrees := make(map[uint64][]uint64, len(deptRows))
	for _, row := range deptRows {
		ancestors := parseDeptAncestorIDs(row.Ancestors)
		if len(ancestors) > 0 {
			deptAncestors[row.ID] = ancestors
		}

		deptSubtrees[row.ID] = append(deptSubtrees[row.ID], row.ID)
		for _, ancestorID := range ancestors {
			deptSubtrees[ancestorID] = append(deptSubtrees[ancestorID], row.ID)
		}
	}

	return &dataruleSnapshot{
		domains:       domains,
		roleIDs:       roleIDs,
		deptAncestors: deptAncestors,
		deptSubtrees:  deptSubtrees,
		loadedAt:      time.Now(),
	}, nil
}

func parseDeptAncestorIDs(ancestors string) []uint64 {
	parts := strings.Split(ancestors, ",")
	ids := make([]uint64, 0, len(parts))
	for _, item := range parts {
		id, err := strconv.ParseUint(strings.TrimSpace(item), 10, 64)
		if err != nil || id == 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
