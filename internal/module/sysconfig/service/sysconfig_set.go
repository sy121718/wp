package sysconfigservice

// sysconfig_set.go — 写路径：整组保存（乐观锁）。

import (
	"context"
	"errors"
	"strings"
	"time"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

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
