// inventory_reason.go — 变动原因字典（issue #16 验收 4）。
//
// 「变动原因覆盖出 / 入 / 调整各枚举，含自定义原因（引用可维护字典，不用自由文本）」：
//
//	· 内置原因：迁移 103 seed，project_id IS NULL（全工程可见，**只读**：不可改名、
//	  不可删除，只能停用 / 启用 —— 名称由系统按 code 派生 i18n key，见迁移 241）；
//	· 自定义原因：工程内 code 唯一，可改名 / 停用（停用后不再能被新变动引用，
//	  历史流水不受影响 —— 流水里存的是 code 快照）。
//
// 变动入口只接受字典里存在的 code，且原因方向必须与本次变动方向一致。
//
// 文案不算本模块的数据（2026-09 收口）：inventory_change_reasons.name 存的是 **i18n key**，
// 真文案在 sys_i18n（内容 → 文案词条，全站唯一真源）。本模块只负责「有哪些原因、方向、启停」，
// 自定义原因保存时把运营填的文案写成 sys_i18n 的一条词条 —— 运营改文案、加语言都在同一个地方，
// 而不是回到库存模块里再维护一份平行的名称表。
package inventoryservice

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// maxReasonCodeLen 原因 code 长度上限。
const maxReasonCodeLen = 32

// reasonI18nPrefix 原因词条的 key 前缀（内置：inventory.reason.<code>）。
const reasonI18nPrefix = "inventory.reason."

// builtinReasonKey 内置原因的 i18n key（形如 inventory.reason.purchase_in）。
//
// 内置原因全工程共用一条词条，因此 key 里不带工程 —— 文案改了所有工程一起变，
// 这正是内置的含义（它是产品预置的取值范围，不是某个工程的自定义项）。
func builtinReasonKey(code string) string { return reasonI18nPrefix + strings.ToLower(code) }

// customReasonKey 自定义原因的 i18n key。
//
// key 里带工程 id：sys_i18n 是全局表，两个工程各建一个同 code 的自定义原因时，
// 不带工程 id 的 key 会互相覆盖（后建的工程把前一个的文案悄悄改掉）。
func customReasonKey(projectID, code string) string {
	return reasonI18nPrefix + "custom." + strings.ToLower(strings.TrimSpace(projectID)) + "." + strings.ToLower(code)
}

// reasonTextCategory sys_i18n 里的分类（后台词条页按它筛选）。
const reasonTextCategory = "inventory"

// saveReasonText 把原因文案写进 sys_i18n（该 key 的 zh-CN 一行），并重载词条缓存。
//
// 失败**不阻断**原因本身的写入，只记一条日志：原因是业务数据（流水要引用它的 code），
// 文案是展示层；把「i18n 组件不可用」（纯库存单测 / 未初始化）升级成「原因建不了」
// 是拿一个更贵的故障换一个更便宜的。en-US 不写 —— 按项目约定，新词条不伪造译文，
// 英文界面取不到时按默认语言（zh-CN）回退。
func saveReasonText(ctx context.Context, key, text string) (err error) {
	return i18n.SaveEntry(ctx, i18n.Entry{
		Key:      key,
		Lang:     "zh-CN",
		Value:    text,
		Category: reasonTextCategory,
		Remark:   "internal/module/inventory/service/inventory_reason.go",
	})
}

// writeReasonText 写词条并吞掉失败（记日志），返回是否写入成功。
func writeReasonText(ctx context.Context, key, text string) (ok bool) {
	if err := saveReasonText(ctx, key, text); err != nil {
		logger.Scene("inventory").With("key", key).With("err", err).Warn("变动原因文案写入 sys_i18n 失败")
		return false
	}
	return true
}

// ListReasons 变动原因列表（工程自定义 + 全部内置）。
func (s *Service) ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	filter := inventorymodel.ReasonFilter{
		Keyword:         strings.TrimSpace(req.Keyword),
		IncludeDisabled: req.IncludeDisabled,
	}
	if filter.Direction, err = normalizeDirectionOrEmpty(req.Direction); err != nil {
		return nil, err
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListReasons(ctx, filter)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.ReasonResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toReasonResp(r))
	}
	return list, nil
}

// CreateReason 新建自定义变动原因（code 工程内唯一，方向一经确定不可改）。
func (s *Service) CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(inventoryenums.ErrReasonNameRequired)
	}
	code, err := normalizeReasonCode(req.Code)
	if err != nil {
		return nil, err
	}
	direction, err := normalizeDirection(req.Direction)
	if err != nil {
		return nil, errors.New(inventoryenums.ErrReasonDirectionInvalid)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// 内置原因占用同一命名空间：自定义 code 不能与内置撞名（否则解析结果不确定）。
	if taken, cerr := s.m.ReasonCodeExists(ctx, projectID, code, 0); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, errors.New(inventoryenums.ErrReasonCodeTaken)
	}
	// 原因行只存 i18n key，运营填的文案写成 sys_i18n 的一条词条。
	// 顺序「先词条、后原因」：反过来会出现「原因已能选、页面却显示裸 key」的窗口。
	key := customReasonKey(projectID, code)
	writeReasonText(ctx, key, name)
	now := time.Now().UTC()
	pid := projectID
	e := &inventorymodel.ReasonEntity{
		ProjectID: &pid, Code: code, Name: key, Direction: direction,
		IsBuiltin: false, Status: inventoryenums.StatusActive, Sort: req.Sort,
		CreateTime: now, UpdatedAt: now,
	}
	if err = s.m.CreateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

// UpdateReason 修改自定义变动原因（内置原因一律拒绝）。
func (s *Service) UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if perr != nil {
		return nil, errors.New(inventoryenums.ErrReasonNotFound)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetReason(ctx, rid, projectID)
	if err != nil {
		return nil, mapReasonNotFound(err)
	}
	// 内置原因只读：它的名称就是系统按 code 派生的 key（inventory.reason.<code>），
	// 允许改文案等于让「内置」名不副实 —— 停用 / 排序则允许（那是使用范围，不是身份）。
	if e.IsBuiltin && req.Name != nil {
		return nil, errors.New(inventoryenums.ErrReasonBuiltin)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(inventoryenums.ErrReasonNameRequired)
		}
		// key 不变（它派生自 code 与工程），改的是词条的值。
		writeReasonText(ctx, e.Name, name)
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
			return nil, errors.New(inventoryenums.ErrReasonStatusInvalid)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

// resolveReason 解析变动原因：必须是字典里的 active 条目，且方向与本次变动一致。
func (s *Service) resolveReason(ctx context.Context, projectID, code, direction string) (r *inventorymodel.ReasonEntity, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New(inventoryenums.ErrStockReasonRequired)
	}
	e, ferr := s.m.FindReasonByCode(ctx, projectID, code)
	if ferr != nil {
		return nil, mapReasonNotFound(ferr)
	}
	if e.Direction != direction {
		return nil, errors.New(inventoryenums.ErrReasonDirectionMismatch)
	}
	return e, nil
}

// normalizeReasonCode 归一并校验原因 code：小写字母 / 数字 / 下划线，1..maxReasonCodeLen。
func normalizeReasonCode(code string) (out string, err error) {
	out = strings.ToLower(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrReasonCodeRequired)
	}
	if len(out) > maxReasonCodeLen {
		return "", errors.New(inventoryenums.ErrReasonCodeInvalid)
	}
	for _, r := range out {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return "", errors.New(inventoryenums.ErrReasonCodeInvalid)
		}
	}
	return out, nil
}

// mapReasonNotFound 行不存在 → 业务错误，其余原样透出。
func mapReasonNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrReasonNotFound)
	}
	return err
}

// toReasonResp 实体 → 响应。
func toReasonResp(e *inventorymodel.ReasonEntity) *inventorydto.ReasonResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.ReasonResp{
		ID: strconv.FormatInt(e.ID, 10), Code: e.Code, Name: e.Name, Direction: e.Direction,
		IsBuiltin: e.IsBuiltin, Status: e.Status, Sort: e.Sort,
		CreatedAt: e.CreateTime.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if e.ProjectID != nil {
		resp.ProjectID = *e.ProjectID
	}
	return resp
}
