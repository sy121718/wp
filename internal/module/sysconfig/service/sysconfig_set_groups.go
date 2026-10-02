package sysconfigservice

// sysconfig_set_groups.go — 写路径：多组一次保存（单事务 + 逐组乐观锁）。

import (
	"context"
	"errors"
	"strings"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

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
