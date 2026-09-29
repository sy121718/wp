// Package membershipservice 实现 membership 模块契约（BIZ-3 会员等级 + 权益）。
//
// 文件分工：
//
//	membership_service.go  —— Service / NewService / 编译期断言 / 纯逻辑与形状转换
//	membership_tier_crud.go —— 等级与权益的 CRUD（写操作全部同事务）
//	membership_assign.go    —— 归属的手工指定 / 解锁 / 列表
//	membership_resolve.go   —— 读路径（等级解析，**绝不写库**）
//	membership_recalc.go    —— 日结重算与它的调度器
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
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// 模块常量。
const (
	// tierNameMaxLen 等级名长度上限（与迁移 462 的 VARCHAR(60) 对齐）。
	//
	// 应用层先判一次不只是为了友好文案：超长在 PG 上会报 22001，
	// 那句原文（含表名与列名）不该出现在页面上。
	tierNameMaxLen = 60
	// remarkMaxLen 备注长度上限（VARCHAR(200)）。
	remarkMaxLen = 200
	// defaultPageSize 归属列表默认每页条数。
	defaultPageSize = 20
	// maxPageSize 每页条数上限（超过即截到上限，而不是让请求方决定查询规模）。
	maxPageSize = 200
	// errScene 结构化日志场景名（与模块内其它 logger.Scene 一致）。
	errScene = "membership"
)

// Service 会员等级与权益的业务实现。
//
// 不持有 *gorm.DB：所有表访问经 model，跨表事务经 model.TransactionScoped
// （AGENTS.md §model 层定位；`bash scripts/check-service-db-boundary.sh` 守门）。
type Service struct {
	model *membershipmodel.Model
	// projects 站点工程契约：用于校验工程存在、列工程、以及日结重算遍历工程清单。
	projects projectcontract.ProjectService
	// purchases 消费额批量只读端口（订单侧实现）。**可选**：
	// 未注入时归属重算整体不可用（调度器禁用、RecalcProject 显式失败），
	// 而不是「扫到 0 个人」——后者看起来像「大家都没消费」。
	purchases membershipcontract.PurchaseSource
	// recalcOnce 保证日结调度器只启动一次（见 StartRecalcScheduler）。
	recalcOnce sync.Once
}

// NewService 构造。
func NewService(m *membershipmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{model: m, projects: projects}
}

// 编译期断言：本模块对外契约的每一份收窄形状都由 *Service 实现。
//
// 分五条而不是一条 MembershipService：装配层按需要断言取哪一份能力，
// 少实现一条（例如 Reader 的 Resolve 漏了）要在**启动时**炸掉，
// 而不是等片段层渲染会员信息时才发现拿不到数据。
var (
	_ membershipcontract.Reader              = (*Service)(nil)
	_ membershipcontract.Assigner            = (*Service)(nil)
	_ membershipcontract.AssignmentAdminPort = (*Service)(nil)
	_ membershipcontract.TierAdminPort       = (*Service)(nil)
	_ membershipcontract.RecalcPort          = (*Service)(nil)
	_ membershipcontract.FacingTexter        = (*Service)(nil)
	_ membershipcontract.MembershipService   = (*Service)(nil)
)

// SetPurchaseSource 注入消费额批量只读端口（装配期在订单侧就绪后调用一次）。
func (s *Service) SetPurchaseSource(port membershipcontract.PurchaseSource) {
	if s == nil {
		return
	}
	s.purchases = port
}

// —— 参数校验 ——
//
// 校验只做「这一层能给出更好说法」的那些：格式 / 必填 / 长度。
// 真正的取值域（权益的 0/1 与 1..100、source 的两值）由迁移 462 的 CHECK 承载，
// 应用层先判一次是为了把约束名换成可行动提示，不是替代它。

// requireProject 校验工程 id 非空。
func requireProject(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New(membershipenums.ErrProjectRequired)
	}
	return nil
}

// requireUser 校验访客 id 有效（0 是无效 id，静默当成「没有归属」会让调用方以为解析成功）。
func requireUser(userID uint64) error {
	if userID == 0 {
		return errors.New(membershipenums.ErrUserRequired)
	}
	return nil
}

// requireTier 校验等级 id 有效。
func requireTier(tierID int64) error {
	if tierID <= 0 {
		return errors.New(membershipenums.ErrInvalidParam)
	}
	return nil
}

// normalizeTierName 校验并归一等级名（去首尾空白）。
func normalizeTierName(raw string) (name string, err error) {
	name = strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New(membershipenums.ErrInvalidParam)
	}
	if len([]rune(name)) > tierNameMaxLen {
		return "", errors.New(membershipenums.ErrInvalidParam)
	}
	return name, nil
}

// normalizeRemark 归一备注（超长即拒，不静默截断 —— 截断后的内容运营看不出来被改过）。
func normalizeRemark(raw string) (string, error) {
	remark := strings.TrimSpace(raw)
	if len([]rune(remark)) > remarkMaxLen {
		return "", errors.New(membershipenums.ErrInvalidParam)
	}
	return remark, nil
}

// validateThreshold 校验门槛（单位分，不允许负数）。
func validateThreshold(amount int64) error {
	if amount < 0 {
		return errors.New(membershipenums.ErrThresholdInvalid)
	}
	return nil
}

// normalizePage 归一页码与每页条数。
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

// —— 权益归一（纯逻辑，供单测）——

// normalizeEntitlements 把请求里的权益清单归一成可落库的行。
//
// 三条规则：
//  1. kind 必须是本期支持的两种（上界封闭：新增一种要同时改 enums、迁移 462 的 CHECK 与 462a 的词条）；
//  2. 取值按 kind 限域 —— 免运费 0/1；折扣 1..100（**扣减百分比**，20 = 打八折）。
//     负的百分比会把折扣算成加价、超过 100 会让应付为负，两者都不该靠调用方自觉避免；
//  3. 同 kind 重复时**后一条覆盖前一条**（而不是报错）：后台表单可能带出隐藏字段，
//     报错会让运营面对一个自己看不出来的重复项。去重后按 kind 升序，
//     与 ReplaceForTierTx 的落库顺序一致，读回来的顺序因此稳定。
func normalizeEntitlements(items []membershipdto.EntitlementReq) (rows []*membershipmodel.EntitlementEntity, err error) {
	byKind := map[string]int64{}
	for _, item := range items {
		kind := strings.TrimSpace(item.Kind)
		switch kind {
		case membershipenums.KindFreeShipping:
			if item.ValueInt != 0 && item.ValueInt != 1 {
				return nil, errors.New(membershipenums.ErrEntitlementValue)
			}
		case membershipenums.KindDiscount:
			if item.ValueInt < 1 || item.ValueInt > 100 {
				return nil, errors.New(membershipenums.ErrEntitlementValue)
			}
		default:
			return nil, errors.New(membershipenums.ErrEntitlementKind)
		}
		byKind[kind] = item.ValueInt
	}
	// 按 enums.EntitlementKinds 的顺序输出：落库顺序不依赖请求里 kind 的排列。
	rows = make([]*membershipmodel.EntitlementEntity, 0, len(byKind))
	for _, kind := range membershipenums.EntitlementKinds {
		if value, ok := byKind[kind]; ok {
			rows = append(rows, &membershipmodel.EntitlementEntity{Kind: kind, ValueInt: value})
		}
	}
	return rows, nil
}

// —— 等级解析（纯逻辑，供单测）——

// selectTierBySpend 按消费额在等级清单里选出应属的档。
//
// 口径：清单已按 sort_order **降序**（model.ListTiers 的排序），取第一个
// `spent >= threshold_amount` 的档。sort_order 是等级高低的排序真源 ——
// 它而不是 threshold_amount 决定优先级，这样运营可以把「钻石」排在「黄金」之上
// 而不必保证门槛数值单调。
//
// 都不满足时返回 nil，由调用方回退到默认等级（**不是**这里退回 sort_order 最小的那一档：
// 调用方要能区分「他达到了某个档」与「他还没达到任何档」）。
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

// pickDefaultTier 在等级清单里找默认等级（内存兜底路径用）。
//
// 只是「从已取到的清单里挑」，不是「查不到就选最小的」：清单一定来自
// model.GetDefaultTier（按 is_default 查出来的那一条），这里再做一次匹配是为了
// 让调用点在同一个事务里只查一次等级表。
func pickDefaultTier(tiers []*membershipmodel.TierEntity) *membershipmodel.TierEntity {
	for _, tier := range tiers {
		if tier != nil && tier.IsDefault {
			return tier
		}
	}
	return nil
}

// —— 形状转换 ——

// toJSONTime 时间 → 对外 JSON 时间（只到秒，AGENTS.md §数据库）。
func toJSONTime(t time.Time) utils.JSONTime { return utils.JSONTime(t) }

// toTierResp 等级实体 + 权益行 → 对外响应。
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

// toAssignmentResp 归属实体 + 等级名 → 对外响应。
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

// tierNameOf 在清单里按 id 取等级名（列表页避免每行再查一次）。
func tierNameOf(tiers []*membershipmodel.TierEntity, tierID int64) string {
	for _, tier := range tiers {
		if tier != nil && tier.ID == tierID {
			return tier.Name
		}
	}
	return ""
}

// —— 错误判定 ——

// isNotFound gorm 的记录不存在判据。
func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// uniqueViolationOn 判定错误是否是「命中指定唯一索引 / 约束名」的 23505。
//
// 为什么要按名字分辨：membership_tiers 上有三条部分唯一索引（name / threshold / default），
// 它们**都报 23505** —— 只判「是唯一冲突」会让运营对着一个约束名猜自己撞了哪一条。
//
// 两条判据同时用（与 product_crud 的 mapContainerSKUConflict 同一手法）：
// SQLSTATE 在真实 PG 上一定在；索引名则覆盖「测试 fixture 拿不到原生错误结构、只有文本」的场合。
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

// —— 契约出口：文案 ——

// FacingText 把本模块的错误转成指定语言下可直接展示的一句话（消费方唯一的文案出口）。
//
// 三种形态（与 enums 的判据一一对应）：
//
//   - 命中白名单 → 按语言取词（key|param 形态交给 i18n.FillTranslate 之外的取词层，
//     本模块的带参形态是 `key：<定位>`，见下一条）；
//   - `key：<定位>` → 翻译 key 再拼回定位信息（定位信息是**数据**：
//     哪个 project_id、还剩几条归属；不是内部错误原文）；
//   - 未命中 → 记结构化日志（原文只进日志）并返回归口文案。
//
// 实现与 inbound/http 的 membershipErrText **同一份判据**（enums.HitFacingMessage）：
// 页面出口与契约出口对「什么能对外说」不会产生分歧。
func (s *Service) FacingText(lang string, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if key, detail, ok := membershipenums.SplitFacingDetail(msg); ok {
		return i18n.Translate(key, key, lang) + facingDetailSep(lang) + detail
	}
	if _, ok := membershipenums.HitFacingMessage(msg); ok {
		return i18n.Translate(msg, msg, lang)
	}
	logger.Scene(errScene).Error(err, "membership 契约出口遇到未归类的错误（原文只进日志）")
	return i18n.Translate(membershipenums.ErrInternal, "操作失败，请稍后重试（细节只进日志）", lang)
}

// facingDetailSep 定位信息的分隔符：中文用全角冒号，其余语言用「: 」。
//
// 读侧（shell.FacingNotice 的形态 3）两种都认，所以写读两侧不会因语言切换而失配。
func facingDetailSep(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return membershipenums.FacingDetailSep
	}
	return ": "
}

// —— 上下文小工具 ——

// projectExists 校验工程存在（projects 契约未注入时跳过 —— 它只是加固，不是判据主体）。
//
// 为什么仍要做这一层：tiers.project_id 没有外键，一个笔误的 uuid 会建出一整套
// 永远查不到的等级（它们只有在「用那个 uuid 去查」时才可见）。存在性检查把这种
// 静默错配挡在写入之前。
func (s *Service) projectExists(ctx context.Context, projectID string) error {
	if s.projects == nil {
		return nil
	}
	exists, err := s.projects.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(membershipenums.ErrProjectRequired)
	}
	return nil
}
