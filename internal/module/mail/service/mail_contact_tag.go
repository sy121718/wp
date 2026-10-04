package mailservice

// mail_contact_tag.go — 标签的批量增删与候选集合。
//
// 标签是 text[] 列上的**精确匹配**值（筛选与投递目标都走 tags @> ...），
// 所以这里的每一处去空白 / 去重 / 大小写处理都必须与 splitTags / mergeTags / diffTags 同口径：
// 口径一旦放宽（比如把标签统一转小写），「按标签筛人群」就会开始漏人，而且不报错。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// TagContacts 批量给联系人加 / 减标签。
//
// 逐条「锁读当前标签 → 算新值 → 写回」：数组列上没有既做减法又保序去重的单语句写法，
// 而丢掉行锁就会让并发写互相覆盖（见 model.LockContactTagsTx 的说明）。
// 批量 IN 预读只能省一次读、不能省锁 —— 锁外读到的旧值随时会变，写回去就是丢更新。
//
// 牺牲是 N 次往返（N = 点选条数）。为什么可接受：点选上限由 shell.BulkIDs 封顶
// （MaxBulkIDs），不是无界输入；将来上限若放大到几百，再把这里改成「按 id 升序一次锁多行」。
//
// changed 计「已处理」而不是「标签真的变了」：全部人本来就带着该标签时，
// 回执说「已处理 N 个」比两个数字都是 0、页面没有任何反馈要好（后者会被当成点了没反应）。
// skipped 只计失败与不存在的行，逐条失败不中断整批。
func (s *Service) TagContacts(ctx context.Context, req *maildto.TagContactsReq) (changed, skipped int, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, 0, errors.New(mailenums.ErrInvalidParam)
	}
	add, remove := normalizeTagList(req.Add), normalizeTagList(req.Remove)
	if len(add) == 0 && len(remove) == 0 {
		return 0, 0, errors.New(mailenums.ErrContactTagEmpty)
	}
	for _, id := range dedupIDs(req.IDs) {
		var addedTags []string
		werr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
			cur, lerr := s.m.LockContactTagsTx(ctx, tx, id)
			if lerr != nil {
				return lerr
			}
			next, added := applyTagDelta(cur, add, remove)
			addedTags = added
			if tagListEqual(cur, next) {
				// 幂等：目标状态已经是这样，不写库。
				return nil
			}
			return s.m.UpdateContactFieldsTx(ctx, tx, id, map[string]any{
				"tags": mailmodel.StringArray(next), "update_time": time.Now(),
			})
		})
		if werr != nil {
			skipped++
			continue
		}
		changed++
		// 触发在事务外（自动化要建实例、可能入队发信）。
		for _, tag := range addedTags {
			s.OnTagsAddedBatch(ctx, tag, []uint64{id})
		}
	}
	return changed, skipped, nil
}

// ListContactTags 全库去重后的标签集合（筛选区 datalist 候选）。
//
// 只读、且只服务列表页的筛选项，权限复用 mail:contact_list，不新增权限点
// （新增权限点 = 已授权角色静默缩权，见 521 的注释）。
func (s *Service) ListContactTags(ctx context.Context) (tags []string, err error) {
	return s.m.ListContactTags(ctx)
}

// applyTagDelta 在 cur 之上加 add、去 remove，返回新标签与本次**真正新增**的标签。
//
// 口径与 splitTags / mergeTags / diffTags 一致：去空白、去重、保序、精确匹配（大小写敏感）。
// 同一条请求里既在 add 又在 remove 的标签以 **remove 为准**（「去掉」是更强的诉求：
// 运营同时勾了两个字段时，宁可少打一个标签，也不该给一个明确要去掉的人打上）。
func applyTagDelta(cur, add, remove []string) (next []string, added []string) {
	drop := make(map[string]struct{}, len(remove))
	for _, t := range remove {
		if v := strings.TrimSpace(t); v != "" {
			drop[v] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(cur)+len(add))
	next = make([]string, 0, len(cur)+len(add))
	for _, t := range cur {
		v := strings.TrimSpace(t)
		if v == "" {
			continue
		}
		if _, ok := drop[v]; ok {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		next = append(next, v)
	}
	for _, t := range add {
		v := strings.TrimSpace(t)
		if v == "" {
			continue
		}
		if _, ok := drop[v]; ok {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		next = append(next, v)
		added = append(added, v)
	}
	return next, added
}

// normalizeTagList 去空白、去空项、去重（保序）—— 表单与 CSV 两条输入共用。
func normalizeTagList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// tagListEqual 逐项比较两组标签（顺序敏感：新值由 applyTagDelta 按原顺序生成）。
func tagListEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dedupIDs 去零、去重（保序）。批量入口的 id 集合来自 shell.BulkIDs，
// 但单条路径与直接调用 service 的测试也会走这里，重复 id 会让同一条被处理两次
// （第二次数出的 changed/skipped 就是假的）。
func dedupIDs(ids []uint64) []uint64 {
	out := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
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
