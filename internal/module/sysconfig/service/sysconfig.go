package sysconfigservice

// Package sysconfigservice 实现 sysconfig 模块业务用例（GetGroup / ListGroups / SetGroup）。
//
// 本模块的写路径只有「整组替换」一种，且**必须**带乐观锁版本号：整组读-改-写若不带
// version 条件，两个管理员改同一组的不同键时后写者会静默覆盖前者（迁移 484 文件头）。
// 冲突一律返回明确错误打回给人，不自动合并、不重试、不静默胜出。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/module/sysconfig/dto"
	"go_wp/internal/module/sysconfig/enums"
	"go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// Service sysconfig 模块业务实现。
type Service struct {
	m *sysconfigmodel.Model
	// onChanged 保存成功后的主动刷新回调（装配层注入；本批注入的是 pkg/i18n 的
	// Invalidate）。未注入 = 不刷新（测试装配），保存照样成功，消费方会在下一次
	// 定时刷新时看到新值 —— 刷新失败/缺失不改变「配置已落库」这个事实。
	onChanged func()
	// countryLabels 国家/地区「码 → 当前语言显示名」的进程内索引（键 = 归一语言）。
	//
	// 为什么在这里缓存而不是让每个消费方各建一层：sys_area 是参考数据字典，而订单详情
	// 一次渲染可能解析多个地址、列表页一屏几十行 —— 每个消费方各缓存一份，等于同一份
	// 数据在进程里存 N 份、各查一次库。Service 是装配期构造的单实例，缓存挂在它上面天然
	// 只有一份。
	//
	// 值是 countryLabelEntry（索引 + 过期时刻），按 TTL 失效；用 sync.Map 而不是
	// map + 互斥：读远多于写（每种语言每 TTL 才写一次），且条目之间互不影响。
	countryLabels sync.Map
}

// countryLabelEntry 一种语言的「码 → 显示名」索引及其过期时刻。
type countryLabelEntry struct {
	index   map[string]string
	expires time.Time
}

// countryLabelTTL 国家名索引的存活时长。
//
// 为什么要过期、而不是一直缓存到进程结束：sys_area 眼下是迁移 seed 的静态字典，但
// **字典一旦有了编辑入口**（后台字典 CRUD 尚在待办），永久缓存就意味着「改完必须重启
// 进程才生效」—— 那是最难查的一类问题（页面看着正常、就是不改）。TTL 把最坏延迟变成
// 有界值，且不必现在就给字典造一条失效广播链（编辑入口还不存在，广播无处触发）。
//
// 取值依据：参考数据低频变更，5 分钟对运营足够快；对数据库则意味着**每种语言每 5 分钟
// 最多一次全表读**（247 行）。
//
// 将来加字典编辑页时：在保存路径清掉 countryLabels（Range + Delete）即可即时生效，
// 本 TTL 退化为兜底 —— 那时它也仍应保留。
const countryLabelTTL = 5 * time.Minute

// 编译期断言：本模块实现对外两条契约（宽的服务面 + 只读窄口）。
var (
	_ sysconfigcontract.Service      = (*Service)(nil)
	_ sysconfigcontract.ConfigReader = (*Service)(nil)
	// 字典只读口（后台页面下拉供数）：形状对不上时在这里编译错，
	// 而不是等到页面渲染出一片空下拉才发现。
	_ sysconfigcontract.DictReader = (*Service)(nil)
)

// NewService 构造（model 与刷新回调注入，不持有 *gorm.DB）。
func NewService(m *sysconfigmodel.Model, onChanged func()) *Service {
	return &Service{m: m, onChanged: onChanged}
}

// groupOf 实体 → 对外形状。
func groupOf(e *sysconfigmodel.SysConfigEntity) *sysconfigdto.Group {
	if e == nil {
		return nil
	}
	data := map[string]any(nil)
	if len(e.ConfigData) > 0 {
		data = make(map[string]any, len(e.ConfigData))
		for k, v := range e.ConfigData {
			data[k] = v
		}
	}
	return &sysconfigdto.Group{
		Key:        e.GroupKey,
		Name:       e.GroupName,
		Data:       data,
		Remark:     e.Remark,
		Status:     e.Status,
		Version:    e.Version,
		UpdateTime: utils.NewJSONTime(e.UpdateTime),
	}
}

// GetGroup 取一组配置；分组不存在返回 ErrGroupNotFound。
//
// 「不存在」不返回空对象：空对象会让消费方分不清「这一组没配」与「这一组配了但全空」，
// 而这两种情形在默认值链上的处理不同（前者回退代码常量、后者按配置为空处理）。
func (s *Service) GetGroup(ctx context.Context, groupKey string) (res *sysconfigdto.Group, err error) {
	key := strings.TrimSpace(groupKey)
	if key == "" {
		return nil, errors.New(sysconfigenums.ErrInvalidParam)
	}
	e, err := s.m.FindByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, sysconfigcontract.ErrGroupNotFound
	}
	return groupOf(e), nil
}

// ListGroups 列出**启用**的分组（按分组键排序，输出稳定）。
//
// 只列启用组：禁用组是「后台保留但当前不生效」的配置，让消费方读到它等于把
// status 列的语义抹掉（读的人还得自己判一次）。
func (s *Service) ListGroups(ctx context.Context) (res []sysconfigdto.Group, err error) {
	rows, err := s.m.ListByStatus(ctx, sysconfigmodel.StatusEnabled)
	if err != nil {
		return nil, err
	}
	res = make([]sysconfigdto.Group, 0, len(rows))
	for i := range rows {
		res = append(res, *groupOf(&rows[i]))
	}
	return res, nil
}

// SetGroup 整组保存一组配置（乐观锁）。
//
// 并发语义：守卫写在 UPDATE 的 WHERE 里（`group_key = ? AND version = ?`），受影响 0 行
// 即拒绝 —— **不是**「先读出来比对再写回」，后者在并发下必然丢更新（两次读之间别人提交了）。
// 这里的写只有一条语句，因此不需要事务边界（AGENTS.md 的事务判据以「两处及以上持久化写入」为准）。
//
// 0 行有两种成因，必须分开报：版本不符（给人「刷新后重试」）与分组不存在（给人「这一组还没建」）。
// 归因查询发生在写失败之后、只用于措辞，不参与任何写决策。
func (s *Service) SetGroup(ctx context.Context, req *sysconfigdto.SetGroupReq) (res *sysconfigdto.Group, err error) {
	if req == nil {
		return nil, errors.New(sysconfigenums.ErrInvalidParam)
	}
	key := strings.TrimSpace(req.GroupKey)
	if key == "" || len(req.Data) == 0 {
		return nil, errors.New(sysconfigenums.ErrInvalidParam)
	}
	if req.Version <= 0 {
		// 不带版本 = 用手里这份覆盖别人的改动，直接拒绝而不是「省略即强制覆盖」。
		return nil, errors.New(sysconfigenums.ErrVersionRequired)
	}

	rows, err := s.m.UpdateDataWithVersion(ctx, key, req.Version, sysconfigmodel.JSONMap(req.Data), req.UpdateBy)
	if err != nil {
		logger.Scene("sysconfig").With("group", key).Error(err, "系统配置保存失败")
		return nil, err
	}
	if rows == 0 {
		return nil, s.reportNoRows(ctx, key, req.Version)
	}

	// 保存后的主动刷新（阶段 2 因果链）：全局默认值（默认语言 / 站点语言 URL 方案 /
	// 语言码覆盖）现在唯一来源是本表，若只能等下一次定时刷新，运维保存后看到的站点行为
	// 与配置不一致 —— 那正是「写进去了但不生效」的老毛病。刷新失败不影响本次保存结论
	// （配置已落库），消费方会在下一轮刷新对齐。
	if s.onChanged != nil {
		s.onChanged()
	}

	// 读回以带上分组名 / 状态 / 真实 update_time：这些字段本次请求没有改动，但响应是
	// 后台页面继续编辑的依据。读回失败**不**把「保存成功」翻成错误 —— 那会让操作者重复
	// 提交，而第二次提交必然因版本已推进而报冲突；此时降级为最小可用的返回并记日志。
	saved, gerr := s.GetGroup(ctx, key)
	if gerr != nil {
		logger.Scene("sysconfig").With("group", key).Error(gerr, "系统配置已保存但读回失败（返回降级值）")
		return &sysconfigdto.Group{
			Key: key, Data: req.Data, Status: sysconfigmodel.StatusEnabled,
			Version: req.Version + 1, UpdateTime: utils.NewJSONTime(time.Now()),
		}, nil
	}
	return saved, nil
}

// reportNoRows 把「受影响 0 行」归因为分组不存在或版本冲突。
//
// 归因查询失败时按**冲突**报（保守方向）：冲突的提示是「请刷新后重试」，最坏结果是
// 让人重试一次；误报成「分组不存在」则会让人去找一个其实存在的组。
func (s *Service) reportNoRows(ctx context.Context, groupKey string, expectedVersion int64) error {
	current, found, err := s.m.VersionOf(ctx, groupKey)
	if err != nil {
		logger.Scene("sysconfig").With("group", groupKey).Error(err, "系统配置保存冲突归因失败（按版本冲突处理）")
		return errors.New(sysconfigenums.ErrVersionConflict)
	}
	if !found {
		return sysconfigcontract.ErrGroupNotFound
	}
	logger.Scene("sysconfig").
		With("group", groupKey).
		With("expectedVersion", expectedVersion).
		With("currentVersion", current).
		Warn("系统配置保存被乐观锁拒绝（库内版本已变，需重新读取后提交）")
	return errors.New(sysconfigenums.ErrVersionConflict)
}

// SetGroups 一次保存多组配置（**单事务**）。
//
// 为什么需要它而不是循环调 SetGroup：后台系统设置页一张表单同时改 i18n 与 trade
// 两组；逐组独立提交时，第二组失败会留下「默认语言改了、默认货币没改」的半截状态，
// 而界面上它们是一次提交、一个结果（AGENTS.md：两处及以上持久化写入必须同事务）。
//
// 逐组仍是**乐观锁**（守卫在 UPDATE 的 WHERE 里），任意一组 0 行即整批回滚：
// 冲突一律打回给人（「刷新后重试」），不自动合并、不静默覆盖、不重试。
func (s *Service) SetGroups(ctx context.Context, req *sysconfigdto.SetGroupsReq) (res []sysconfigdto.Group, err error) {
	if req == nil || len(req.Groups) == 0 {
		return nil, errors.New(sysconfigenums.ErrInvalidParam)
	}
	keys := make([]string, 0, len(req.Groups))
	for i := range req.Groups {
		g := &req.Groups[i]
		key := strings.TrimSpace(g.GroupKey)
		if key == "" || len(g.Data) == 0 {
			return nil, errors.New(sysconfigenums.ErrInvalidParam)
		}
		if g.Version <= 0 {
			return nil, errors.New(sysconfigenums.ErrVersionRequired)
		}
		keys = append(keys, key)
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		for i := range req.Groups {
			g := &req.Groups[i]
			key := strings.TrimSpace(g.GroupKey)
			rows, uerr := s.m.UpdateDataWithVersionTx(tx, key, g.Version, sysconfigmodel.JSONMap(g.Data), req.UpdateBy)
			if uerr != nil {
				return uerr
			}
			if rows == 0 {
				// 0 行的两种成因分开报（同一事务内读，不参与写决策）：
				// 组不存在 → 让人知道「这一组还没建」；否则按版本冲突报（保守方向，
				// 最坏是让人刷新重试一次，误报成「不存在」则会让人去找一个其实存在的组）。
				_, found, verr := s.m.VersionOfTx(tx, key)
				if verr != nil {
					logger.Scene("sysconfig").With("group", key).Error(verr, "多组保存冲突归因失败（按版本冲突处理）")
					return errors.New(sysconfigenums.ErrVersionConflict)
				}
				if !found {
					return errors.New(sysconfigenums.ErrGroupNotFound)
				}
				return errors.New(sysconfigenums.ErrVersionConflict)
			}
		}
		return nil
	})
	if err != nil {
		logger.Scene("sysconfig").With("groups", strings.Join(keys, ",")).Error(err, "系统配置多组保存失败（整批回滚）")
		return nil, err
	}

	// 保存后主动刷新：全局默认值（默认语言 / 语言 URL 方案）唯一来源是本表，
	// 刷新失败不影响保存结论（配置已落库，消费方下一轮对齐）。
	if s.onChanged != nil {
		s.onChanged()
	}

	res = make([]sysconfigdto.Group, 0, len(keys))
	for _, key := range keys {
		saved, gerr := s.GetGroup(ctx, key)
		if gerr != nil {
			logger.Scene("sysconfig").With("group", key).Error(gerr, "系统配置已保存但读回失败")
			continue
		}
		res = append(res, *saved)
	}
	return res, nil
}
