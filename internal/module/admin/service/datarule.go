package adminservice

// 为什么需要它：pkg/datarule 的 GORM 插件挂在 gorm:query 的 Before 回调上，
// **每一次** GORM 查询都会调用一次 RuleProvider.GetRules。改造前 GetRules 每次都读 4 次库
//（该域的启用规则 / 启用角色 id / 部门祖先 / 规则分配表），于是一次列表查询被放大成
// 1 + 4 次往返，分页越大越明显。
//
// 改造后的形状（与 pkg/casbin 的 urlCodeMap「重建后 Store、读端 Load」、
// pkg/i18n 的 LoadCache + StartAutoRefresh + 写路径主动刷新 是同一套）：
//   - 读路径 GetRules 先无锁 Load 一份 atomic.Pointer 快照：命中即零查询；
//   - 快照为空（从未加载）时**懒加载**：加锁后二次检查（double-check，防并发首次请求各加载
//     一遍）再重建，重建由一把 sync.Mutex 串行化 —— 同一时刻只有一个加载在跑，读端不参与；
//   - 规则 / 分配 / 部门 / 角色的写路径在提交后**同步**重载一次，写完立即生效；
//   - 另有 5 分钟定时兜底刷新（写路径重载失败、或多实例部署时收敛）。
//
// 为什么不在启动期预加载：快照只有被查询用到才有价值，而「用到才加载」不需要在装配期插入
// 一次可能失败的同步调用（那会让数据库抖动直接变成启动失败）。代价是首次命中请求多付一次
// 加载（3 次小查询，实测毫秒级，见 datarule_snapshot_db_test.go 的记录）。
//
// 安全语义（改这里之前必读）：
//   - 快照从未加载成功时 GetRules 返回 error，**绝不返回空规则集合** —— 空集合在引擎里
//     意味着「没有任何限制」，那是把数据权限静默关掉，比查询失败危险得多（改造前 provider
//     出错走 db.AddError(err)，同样是 fail-closed，这里保持同一语义）；
//   - 加载失败**不用空快照覆盖已有的成功快照**：保留旧快照 + 记日志（引擎继续按旧规则拦）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/admin/dto"
	"go_wp/internal/module/admin/enums"
	"go_wp/internal/module/admin/model"
	"go_wp/pkg/datarule"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// RuleDetail 数据规则详情。
func (s *Service) RuleDetail(ctx context.Context, req *admindto.RuleDetailReq) (res *admindto.RuleDetailResp, err error) {
	// 根据 ID 查询规则实体
	entity, err := s.drm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrRuleNotFound)
	}

	// 处理可选字段和格式化时间
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

// RuleCreate 新建数据规则。
func (s *Service) RuleCreate(ctx context.Context, req *admindto.RuleCreateReq) error {
	// domain 必须已注册（pkg/datarule 注册中心），且配置只能引用该域声明过的字段与操作符；
	// 白名单不在这里把关的话，规则会「落库成功但运行时被引擎静默丢弃」。
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
	// 可选备注
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err := s.drm.Create(ctx, entity); err != nil {
		return err
	}
	// 规则已落库：同步重载快照，新规则对下一次查询立即生效（不等 5 分钟兜底刷新）。
	s.reloadDataRuleSnapshotAfterWrite("规则新建")
	return nil
}

// RuleUpdate 更新数据规则。
func (s *Service) RuleUpdate(ctx context.Context, req *admindto.RuleUpdateReq) error {
	// 校验规则是否存在
	entity, err := s.drm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrRuleNotFound)
	}
	// 先查规则存在性（「规则不存在」语义优先），再校验 domain 与配置。
	validated, err := validateRuleConfig(req.Domain, req.Config.ToRuleConfig())
	if err != nil {
		return err
	}
	config, err := encodeRuleConfig(validated)
	if err != nil {
		return err
	}

	// 更新字段
	entity.RuleName = req.RuleName
	entity.Domain = req.Domain
	entity.Config = config
	entity.Status = req.Status
	// 若 remark 为空则置 nil，否则更新
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	if err := s.drm.Update(ctx, entity); err != nil {
		return err
	}
	// 规则已更新：同步重载快照（改配置 / 启停都立即生效）。
	s.reloadDataRuleSnapshotAfterWrite("规则更新")
	return nil
}

// RuleDelete 批量删除数据规则。
//
// 事务：sys_rule_assignment 的分配行与 sys_rule 的规则行是两处持久化写，必须同事务。
// 旧实现「先逐条删分配（各自提交）、再删规则」—— 第二步失败就留下「规则还在、分配全没了」：
// 该规则对任何角色/用户/部门都不再生效（数据权限静默放开），而报错只回给这一次请求，
// 列表页上完全看不出异常。现在任一步失败整体回滚：要么规则与分配一起消失，要么都保持原样。
//
// 读-改-写加行锁复核（LockByIDsTx）：从拿到 req.IDs 到真正删除之间，目标行可能已被
// 并发删掉，不锁的复核删的是「快照里的 id」。数量对不上即 ErrRuleNotFound
// （与 AdminDelete 同形：冲突与不一致打回给人，不静默跳过）。
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
			return errors.New(adminenums.ErrRuleNotFound)
		}
		// 先删分配行再删规则行（保持原顺序，与外键方向一致）；两步同事务。
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
	// 删除事务已提交：同步重载快照，被删规则立刻不再命中。
	s.reloadDataRuleSnapshotAfterWrite("规则删除")
	return nil
}

// uniqueRuleIDs 剔除 0 并去重（与 uniqueAdminIDs 同形）。
// 不去重时 LockByIDsTx 锁到的是**去重后**的行数，复核会把它误判成「有行不存在」，
// 于是一次重复提交就被整批拒绝，还报「规则不存在」。
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

// validateRuleConfig 校验规则配置只能引用该数据域白名单内的字段与该字段声明的操作符，
// 并返回可直接落库的配置（字段名、逻辑、操作符都按声明口径原样保留）。
//
// 为什么这层必须有：白名单的消费者只有引擎的「字段名合法性」与操作符白名单 ——
// 前者只过滤字符集（escapeField），后者只查全局 supportedOps。字段名写错、字段存在但
// 用错操作符、OmitFields 写了本表没有的列，引擎全都静默跳过或静默无效：规则看起来生效，
// 实际什么都没拦。所以引用完整性在这里按域声明逐项判定，不合法直接拒绝落库。
//
// 调用方（表单路径 adminConfigFromJSON）不经过 gin 的 binding 校验，因此各分支在这里兜底重判。
func validateRuleConfig(domain string, cfg datarule.RuleConfig) (datarule.RuleConfig, error) {
	declared, ok := datarule.GetDomain(domain)
	if !ok {
		return datarule.RuleConfig{}, errors.New(adminenums.ErrInvalidDomain)
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
		// 引擎对未知 Logic 会静默降级为 AND（buildConditions），这里不接受「差不多」的写法。
		// 取值口径与 dto 的 binding 声明一致（只认大写），表单路径与 JSON API 行为才不会有微妙差异。
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
			// 同上：只认大写，且必须是该字段声明过的操作符（不是「引擎全局支持」就算数）。
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

// containsOperator 报告字段声明的操作符里是否包含 op（两侧都已是大写）。
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

// RuleList 数据规则分页查询。
func (s *Service) RuleList(ctx context.Context, req *admindto.RuleListReq) (res *admindto.RuleListResp, err error) {
	// 构建基础查询，支持按 domain 和 status 筛选
	query := s.drm.DB(ctx)

	if req.Domain != "" {
		query = query.Where("domain = ?", req.Domain)
	}
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	// 查询总记录数用于分页
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, err
	}

	// 按 id 降序分页查询
	var items []admindto.RuleItem
	offset := (req.GetPage() - 1) * req.GetLimit()
	err = query.Order("id DESC").
		Offset(offset).Limit(req.GetLimit()).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	// 保证返回空切片而非 nil
	if items == nil {
		items = []admindto.RuleItem{}
	}

	return &admindto.RuleListResp{Total: total, List: items}, nil
}

// RuleSchemaList 返回所有已注册 domain。
// 数据来自 pkg/datarule 注册中心，非数据库。
func (s *Service) RuleSchemaList(ctx context.Context) (res []admindto.RuleDomainItem, err error) {
	// 从注册中心获取全部已注册 domain 列表
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

// RuleSchemaDetail 返回 domain 的字段白名单。
func (s *Service) RuleSchemaDetail(ctx context.Context, req *admindto.RuleSchemaDetailReq) (res *admindto.RuleDomainDetail, err error) {
	// 遍历注册中心查找指定 domain
	domains := datarule.GetRegisteredDomains()
	for _, d := range domains {
		if d.Domain == req.Domain {
			// 将注册中心的字段白名单转换为 DTO
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

	// domain 未注册时返回空
	return nil, nil
}

// GetRules 实现 datarule.RuleProvider 接口。
//
// **读路径零查询**：只读一份进程内快照（datarule_snapshot.go），在内存里按
// 「用户本人 / 用户角色 / 用户部门 / 上级部门 + SELF_AND_CHILDREN」过滤规则分配，
// 命中的规则按 id 去重后解析配置。快照由装配期加载一次、写路径同步重载、定时兜底刷新共同维护。
//
// 快照是**懒加载**的：快照为空（从未加载）时在这里加锁重建一次，之后一直复用，
// 直到写路径主动重载或 5 分钟兜底刷新把它换掉。
//
// fail-closed：加载失败时返回 error（让本次查询失败），绝不返回空规则集合 —— 空集合在引擎里
// 意味着「没有任何限制」，那是把数据权限静默关掉，比查询失败危险得多（改造前 provider
// 出错走 db.AddError(err)，同样是 fail-closed，这里保持同一语义）。
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
		return nil, nil // 该域没有启用规则：与快照语义一致，不是错误
	}

	// 用户角色 code → 启用角色 id（快照里只保留 status=启用 的角色）。
	roleIDs := make([]uint64, 0, len(user.Roles))
	for _, code := range user.Roles {
		if id, ok := snap.roleIDs[code]; ok {
			roleIDs = append(roleIDs, id)
		}
	}
	// 用户部门的祖先链（快照解析，替代改造前的 AncestorIDs 查库）。
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

// RuleAssignmentList 查询规则分配列表。
func (s *Service) RuleAssignmentList(ctx context.Context, req *admindto.RuleAssignmentListReq) (res *admindto.RuleAssignmentListResp, err error) {
	// 按规则 ID 查询所有分配记录
	entities, err := s.dram.ListByRuleID(ctx, req.RuleID)
	if err != nil {
		return nil, err
	}

	// 组装响应，格式化时间字段
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

// RuleAssignmentSave 批量保存规则分配（全量替换）。
func (s *Service) RuleAssignmentSave(ctx context.Context, req *admindto.RuleAssignmentSaveReq) error {
	// 校验规则存在：避免产生孤儿分配记录（GetRules 对未命中 ruleMap 静默 continue）。
	if _, err := s.drm.GetByID(ctx, req.RuleID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(adminenums.ErrRuleNotFound)
		}
		return err
	}
	seen := make(map[[2]uint64]struct{}, len(req.Assignments))
	now := time.Now()
	entities := make([]adminmodel.SysRuleAssignmentEntity, 0, len(req.Assignments))
	for _, a := range req.Assignments {
		if a.TargetID == 0 {
			return errors.New(adminenums.ErrInvalidAssignment)
		}

		targetScope := adminmodel.AssignmentTargetScopeNone
		switch a.TargetType {
		case adminmodel.AssignmentTargetTypeRole, adminmodel.AssignmentTargetTypeUser:
		case adminmodel.AssignmentTargetTypeDept:
			if a.TargetScope != adminmodel.AssignmentTargetScopeSelf &&
				a.TargetScope != adminmodel.AssignmentTargetScopeSelfAndChildren {
				return errors.New(adminenums.ErrInvalidAssignment)
			}
			targetScope = a.TargetScope
		default:
			return errors.New(adminenums.ErrInvalidAssignment)
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
	// 分配已提交：同步重载快照，让新分配对下一次查询立即生效（不等 5 分钟兜底刷新）。
	s.reloadDataRuleSnapshotAfterWrite("规则分配保存")
	return nil
}

const (
	// DataRuleSnapshotRefreshInterval 快照定时兜底刷新间隔。
	//
	// 5 分钟只兜「写路径重载失败」与「多实例下别的实例改了库」两种情况：正常的变更由写路径
	// 同步重载立即生效，读路径一次库都不查。间隔再短只会增加无谓的空转。
	DataRuleSnapshotRefreshInterval = 5 * time.Minute

	// dataruleSnapshotLoadTimeout 单次重建的超时。
	//
	// 重建是 3 次小查询（规则表极小），30s 已是极端宽松的上限；它的意义是让「卡住的加载」
	// 不至于一直占着重载锁（写路径等锁会连带把写操作拖慢）。
	dataruleSnapshotLoadTimeout = 30 * time.Second
)

// dataruleRule 快照里一条启用规则（只保留读路径需要的两列）。
type dataruleRule struct {
	id     uint64
	config string
}

// dataruleAssignment 一条规则分配，字段与改造前 datarule_provider.go 的四条目标谓词逐项对应。
type dataruleAssignment struct {
	ruleID      uint64
	targetType  int
	targetID    uint64
	targetScope int
}

// dataruleDomain 一个数据域下的启用规则与其分配。
type dataruleDomain struct {
	rules       []dataruleRule
	assignments []dataruleAssignment
}

// dataruleSnapshot 一份**不可变**的数据权限快照：装载完成后只读，任何路径都不得原地修改。
type dataruleSnapshot struct {
	// domains 域 → 该域的启用规则与分配；没有启用规则的域不出现在这里。
	domains map[string]*dataruleDomain
	// roleIDs 仅启用角色：role_code → id（改造前每次 GetRules 都要按用户角色编码查一轮库）。
	roleIDs map[string]uint64
	// deptAncestors 部门 id → 祖先 id 列表（不含自身与 0，解析口径与 Service.AncestorIDs 一致）。
	deptAncestors map[uint64][]uint64
	// deptSubtrees 部门 id → 本部门及全部子孙部门 id（含自身）。
	//
	// 与 deptAncestors 是两个方向，服务两件不同的事：读路径用祖先回答「规则分配在哪个上级部门」，
	// 而引擎的 dept.scope:SELF_AND_CHILDREN 需要的是向下展开的「本部门及子部门」集合
	//（改造前它靠 deptScopeCondition 的 sys_dept 子查询现算，正是那次全表扫描的来源）。
	deptSubtrees map[uint64][]uint64
	// loadedAt 本次装载完成时间（观测用）。
	loadedAt time.Time
}

// dataRuleSnapshotLoader 重建快照的加载器签名。生产实现走 DB（loadDataRuleSnapshotFromDB）；
// 测试可注入返回值，从而在无库条件下覆盖「加载失败保留旧快照」与并发重建语义。
type dataRuleSnapshotLoader func(ctx context.Context) (*dataruleSnapshot, error)

// dataruleSnapshotStore 快照持有者：读端原子 Load（无锁），重建端由 mu 串行化。
type dataruleSnapshotStore struct {
	current atomic.Pointer[dataruleSnapshot]
	mu      sync.Mutex
}

// ---------------------------------------------------------------------------
// 读路径
// ---------------------------------------------------------------------------

// currentDataRuleSnapshot 返回当前快照；从未加载成功过时返回 nil（由调用方 fail-closed）。
func (s *Service) currentDataRuleSnapshot() *dataruleSnapshot {
	if s == nil || s.ruleSnapshot == nil {
		return nil
	}
	return s.ruleSnapshot.current.Load()
}

// DeptSubtreeIDsFromSnapshot 从部门快照解析指定部门的**子树** id 列表（含自身与全部子孙）。
//
// 只暴露这一个方向：中间件要的是「我能看哪些部门的数据」（向下），
// 祖先链（向上：我隶属于哪些上级部门）在读路径内部消化（GetRules 用 deptAncestors 匹配规则分配），
// 没有对外暴露的需要 —— 契约里不留没人读的字段/方法。
//
// 用途见 dataruleSnapshot.deptSubtrees 的注释：引擎据此把 dept.scope:SELF_AND_CHILDREN
// 展开成 IN (...) 而不是每次现算子查询。返回的切片是快照内部数据，调用方只读、不得修改。
func (s *Service) DeptSubtreeIDsFromSnapshot(deptID uint64) []uint64 {
	snap := s.currentDataRuleSnapshot()
	if snap == nil || deptID == 0 {
		return nil
	}
	return snap.deptSubtrees[deptID]
}

// matchAssignmentRuleIDs 把分配列表按用户上下文过滤成命中的规则 id（已去重、保持快照顺序）。
//
// 语义与改造前 datarule_provider.go:61-79 的四条谓词（OR 关系）逐条对应，见 assignmentMatches。
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

// assignmentMatches 判断一条分配是否命中该用户。
//
// 与改造前 datarule_provider.go:61-79 构造的四条谓词一一对应（谓词之间是 OR）：
//
//	① target_type = USER   AND target_id = user.UserID              （:61-62，无条件参与）
//	② target_type = ROLE   AND target_id IN roleIDs                 （:63-66，roleIDs 非空时）
//	③ target_type = DEPT   AND target_id = user.DeptID              （:67-70，DeptID 非 0 时）
//	④ target_type = DEPT   AND target_id IN ancestorDeptIDs
//	                        AND target_scope = SELF_AND_CHILDREN     （:71-79，祖先非空时）
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

// containsDeptOrRoleID 报告 id 是否在给定的 id 列表里（列表都很短，线性查找足够）。
func containsDeptOrRoleID(ids []uint64, id uint64) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 重建路径
// ---------------------------------------------------------------------------

// ensureDataRuleSnapshot 读路径的懒加载入口：快照为空时加锁重建，返回一份可用快照。
//
// double-check 是这里的关键：持锁后先再读一次 atomic.Pointer —— 等锁期间可能已经被别的请求
// 加载完成，此时直接复用，并发首次请求只会有一次真正落到 DB（用计数加载器断言，见用例）。
//
// 失败一律向上返回 error（fail-closed）：调用方（GetRules）会拒绝本次查询，
// 而不是返回「空规则集合」把数据权限静默关掉。旧快照不会被清空（失败根本没走到 Store）。
func (s *Service) ensureDataRuleSnapshot(ctx context.Context) (*dataruleSnapshot, error) {
	if s == nil || s.ruleSnapshot == nil {
		return nil, errors.New("数据权限规则快照存储未初始化")
	}
	s.ruleSnapshot.mu.Lock()
	defer s.ruleSnapshot.mu.Unlock()

	if snap := s.ruleSnapshot.current.Load(); snap != nil {
		return snap, nil // double-check：等锁期间已被加载
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

// LoadDataRuleSnapshot 重建快照并原子替换当前快照；重建失败时**保留旧快照**并返回错误。
//
// 调用时机：
//   - 规则 / 分配 / 部门 / 角色的写路径提交后同步调用（写完立即生效）；
//   - 定时兜底刷新调用（失败只记日志，等下一轮）；
//   - 读路径快照为空时经 ensureDataRuleSnapshot 调用（懒加载）。
func (s *Service) LoadDataRuleSnapshot(ctx context.Context) error {
	if s == nil || s.ruleSnapshot == nil {
		return nil
	}
	// 重建串行化：同一时刻只有一个加载在跑，多个写请求并发提交时不会各查一轮库。
	s.ruleSnapshot.mu.Lock()
	defer s.ruleSnapshot.mu.Unlock()

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctxOrDefault(ctx)), dataruleSnapshotLoadTimeout)
	defer cancel()
	_, err := s.buildAndStoreDataRuleSnapshot(loadCtx)
	return err
}

// buildAndStoreDataRuleSnapshot 在**已持有 store.mu** 的前提下重建并整体替换快照。
//
// 失败时直接返回错误且**不 Store**：已有的成功快照原样保留（空快照覆盖旧快照 = 静默放开数据权限）。
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

// StartDataRuleSnapshotAutoRefresh 启动定时兜底刷新（写路径已保证即时生效，这里只兜底）。
//
// 与既有调度器同形：ticker 到期后按 interval 重建、失败只记日志；ctx 取消即退出。
// 测试进程不启动（见 utils.IsTestProcess）。
//
// **从未加载过就跳过这一轮**：快照是懒加载的，空快照意味着还没有任何查询用过数据权限，
// 此时去查库只是给没人的功能付固定成本（多实例部署下每个实例都会付一遍）。
// 第一次命中请求的懒加载会把快照拉起来，之后 ticker 才接管兜底。
func (s *Service) StartDataRuleSnapshotAutoRefresh(ctx context.Context) {
	if s == nil || s.ruleSnapshot == nil {
		return
	}
	if utils.IsTestProcess() {
		return // 测试进程不启动：加载时机必须由用例自己触发（见 utils.IsTestProcess）。
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
					continue // 从未加载过：没有人用过数据权限，跳过本轮（理由见上方注释）
				}
				s.reloadDataRuleSnapshot(ctx, "定时兜底刷新")
			}
		}
	}()
}

// reloadDataRuleSnapshot 重建一次并只记日志（写路径与定时刷新共用；返回值一律丢弃）。
//
// trigger 只进日志，用来回答「这次重载是谁触发的」。
func (s *Service) reloadDataRuleSnapshot(ctx context.Context, trigger string) {
	if err := s.LoadDataRuleSnapshot(ctx); err != nil {
		logger.Scene("admin").
			With("trigger", trigger).
			With("err", err).
			Error(err, "数据权限快照重建失败：沿用上一份快照（查询期继续按旧规则拦截，定时兜底会再试）")
	}
}

// reloadDataRuleSnapshotAfterWrite 写路径提交后的同步重载。
//
// 为什么失败**不能**让写操作失败：写已经提交了，把加载错误回报给运营只会让人以为「没保存」
// 而重复提交；快照下一轮定时刷新会再试，期间查询按旧规则拦截（fail-closed，不会放开）。
func (s *Service) reloadDataRuleSnapshotAfterWrite(trigger string) {
	if s == nil || s.ruleSnapshot == nil {
		return
	}
	s.reloadDataRuleSnapshot(context.Background(), trigger)
}

// ctxOrDefault 兜住调用方传 nil ctx 的情况（context.WithoutCancel(nil) 会 panic）。
func ctxOrDefault(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// loadDataRuleSnapshot 走加载器（测试注入）或默认的查库实现。
func (s *Service) loadDataRuleSnapshot(ctx context.Context) (*dataruleSnapshot, error) {
	if s.ruleSnapshotLoader != nil {
		return s.ruleSnapshotLoader(ctx)
	}
	return s.loadDataRuleSnapshotFromDB(ctx)
}

// loadDataRuleSnapshotFromDB 3 次查询重建整份快照。
//
// 查询 ① 用 LEFT JOIN 把「规则」与「分配」一次取回（改造前它们分别是一次查询，且分配那一步
// 还要按当前用户拼一段 OR 谓词），在内存里按 domain 归集：
//
//	① SELECT r.id, r.domain, r.config, a.target_type, a.target_id, a.target_scope
//	     FROM sys_rule r LEFT JOIN sys_rule_assignment a ON a.rule_id = r.id
//	    WHERE r.status = 启用
//	② SELECT id, role_code FROM sys_role WHERE status = 启用
//	③ SELECT id, ancestors FROM sys_dept
//
// 表名一律取实体的 TableName()，列名与实体 gorm column 标签一致（不手抄表名）。
// 注意 LEFT JOIN：没有任何分配的规则会出现一行 target_* 全 NULL 的记录，必须跳过分配部分。
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
		// LEFT JOIN 会把同一条规则重复成「分配行数」条记录（无分配时 1 条），规则只收一次。
		if _, ok := seenRules[row.Domain][row.ID]; !ok {
			seenRules[row.Domain][row.ID] = struct{}{}
			domain.rules = append(domain.rules, dataruleRule{id: row.ID, config: row.Config})
		}
		if row.TargetType == nil || row.TargetID == nil || row.TargetScope == nil {
			continue // 该规则没有任何分配（LEFT JOIN 的 NULL 行）
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
		// 子树预计算：把自己挂到自己 + 每一个祖先的子树列表上（部门表很小，一次遍历即可）。
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

// parseDeptAncestorIDs 解析 sys_dept.ancestors（逗号分隔的祖先链），跳过 0 与非法项。
// 口径与 Service.AncestorIDs 一致，保证快照里的祖先链与改造前逐次查库得到的完全相同。
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
