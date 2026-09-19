package adminservice

// datarule_snapshot.go — 数据权限规则的进程内快照（性能整改：查询期 0 次 DB）。
//
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
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

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
