package pubservice

// 与 Activate 内部已有的回执是同一张表、同一套状态机（pending / committed /
// rolled_back），区别只在**时机**：Activate 的回执描述「路由行写入」，
// 这里的回执描述「访问面切换」—— 后者才是不可回退的那一步。
//
// 崩溃恢复的判据（page 侧 RecoverPendingPublications 使用）：
//
//	pending 存在 + 符号链接指向回执记录的产物 → 切换成功、DB 没跟上 → 补完成
//	pending 存在 + 符号链接指向别处或不存在    → 切换没发生 → 标 rolled_back

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"gorm.io/gorm"
)

// RollbackReceipts 启动恢复：全部 pending 回执标记 rolled_back，返回处理数量。
func (s *Service) RollbackReceipts(ctx context.Context) (count int64, err error) {
	return s.model.RollbackPendingReceipts(ctx, time.Now().UTC())
}

func receiptAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return action
}

// errRouteOccupied 事务内占位冲突哨兵，外层映射为 pubenums.ErrRouteOccupied。
// 本体在 model 侧（pubmodel.ErrRouteOccupied）：路由/回执的 …Tx 具名方法在唯一键
// 冲突时归一返回它，service 这边只做「哨兵 → 用户文案」的映射，两侧共用同一哨兵，
// errors.Is 的匹配不会因为哨兵分居两包而失效。
var errRouteOccupied = pubmodel.ErrRouteOccupied

// receiptPayload 回执数据结构化序列化（替代手工拼接 JSON，避免特殊字符生成非法 jsonb）。
type receiptPayload struct {
	To string `json:"to,omitempty"`
}

// publishReceiptAction 访问面切换回执的动作名（与 Activate 的路由回执区分开：
// 恢复流程要按它筛出「切换类」回执，不能把普通路由变更一起卷进来）。
const publishReceiptAction = "switch_active"

// publishReceiptData 回执附加数据。
//
// 放进 receipt_data 是因为 publication_receipts 表没有 project_id / lang / old_path 列，
// 而这几项是「补完成」时定位页面、语言与旧路径的必要信息（补完成要写
// page_publications，改 URL 还要处置旧路径的 301 / 下线）。
type publishReceiptData struct {
	ProjectID string `json:"projectId,omitempty"`
	Lang      string `json:"lang,omitempty"`
	OldPath   string `json:"oldPath,omitempty"`
	Redirect  bool   `json:"redirect,omitempty"`
	// CapturedInputVersion 登记时调用方交来的编译捕获代次（见 BeginPublishReceiptReq）。
	//
	// 落在 receipt_data 而不是新列：回执的附加事实本来就全在这一个 jsonb 里，加列要迁移、
	// 要让旧行回填，而这一项对旧回执的语义就是「缺省 = 未知」（零值时不写该键，读侧缺键
	// 与 0 完全等价）。
	//
	// 为什么是 omitzero 而不是 omitempty：本仓 JSON 引擎是 encoding/json/v2（经
	// pkg/jsonx，见其包注释的差异表）。v2 的 omitempty 语义是「省略 JSON 空值」——
	// 只省略 null、""、[]、{}；数值 0 与布尔 false **不是** JSON 空值，照常写出。
	// 数值/布尔的零值省略在 v2 里只有 omitzero（等价全局选项 OmitZeroStructFields）。
	// 写成 omitempty 会让 capturedInputVersion:0 落库，把「未知」变成一个真实数字。
	CapturedInputVersion int64 `json:"capturedInputVersion,omitzero"`
	// OldPaths 本次改址要处置的全部旧路径（多语言）；OldPath 只是首条兼容值。
	OldPaths []string `json:"oldPaths,omitempty"`
	// SuccessfulReceipts 本次改址关联的成功回执（语言 → switch_active 回执 id 十进制串）。
	//
	// 只对 update_url 出现。它把「这次改址是否真的成功」变成可判定的持久事实：
	// 收敛侧先回读这几个 id 的最终状态，全部 committed 才允许执行旧路径处置。
	// 落在 receipt_data 而不是新列的理由同 CapturedInputVersion：附加事实都在这个 jsonb 里，
	// 旧行的语义是「缺省 = 没有关联事实」，读侧缺键即可判定。
	SuccessfulReceipts map[string]string `json:"successfulReceipts,omitempty"`
	// FromReceiptID 登记时刻账本上的前次成功身份（见 dto.BeginPublishReceiptReq.FromReceiptID）。
	// 原样持久、原样投影：恢复时绝不能回查当前值来顶替它。
	FromReceiptID string `json:"fromReceiptId,omitempty"`
	// PathArtifacts 每条待下线路径的归属事实（见 dto.BeginPublishReceiptReq.PathArtifacts）。
	// 只对 deactivate 出现。
	PathArtifacts map[string]pubdto.ReceiptPathIdentity `json:"pathArtifacts,omitempty"`
	// BusinessCommitted 业务 DB 变更是否已在业务事务里提交（deactivate 专用事实）。
	//
	// 只有 ture 才出现键（omitempty）：新登记的 deactivate 回执= false=业务未提交，
	// 读者缺键与 false 完全等价。它由 MarkDeactivationCommittedTx 与业务变更同事务写入，
	// 因此"键存在"必然意味着"那次业务提交真的发生了"。
	BusinessCommitted bool `json:"businessCommitted,omitempty"`
}

// normalizePathList 归一旧路径清单：去空白、去重、丢空值；全空返回 nil（不出该键）。
//
// 不去重会重复执行同一处置（虽然幂等，但每次多一次 FS 写与路由 upsert）；
// 不丢空值会让重放把空路径当真实目标。
func normalizePathList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeReceiptLinks 归一关联回执表：去空白、丢空值；全空返回 nil（不出该键）。
//
// 不在这里校验 id 是否存在：登记方（presentation 改 URL）刚刚创建了那些回执，
// 校验会多一轮读；而是否「真的成功」由读侧按最终状态判定 —— 写侧只负责如实搬运事实。
func normalizeReceiptLinks(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for lang, id := range in {
		v := strings.TrimSpace(id)
		if v == "" {
			continue
		}
		out[lang] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeReceiptID 归一"前次成功身份"这一个字段：只做空白裁剪，**不做语义纠正**。
//
// 为什么不拒绝非法串、也不把它们归 0："" = 未知、正十进制 = 具体身份、两者含义不同。
// 把一个乱串悄悄归成 ""（未知）或 "0"（确认无前次成功）都是**伪造身份事实** ——
// 前者让已被明确取代的 attempt 变成"证据不足"从而可以被前向补齐，后者让
// "有人成功过"变成"没人成功过"。非法取值一律原样保留，由消费方按受控错误
// （保留 pending）处理，绝不在这里替它猜。
// normalizePathIdentities 归一"每路径归属事实"表：去空 key/空结构；全空返回 nil（不出该键）。
//
// 不在这里校验产物/回执 id 是否存在：登记方刚从自己的账本读到它们，
// 校验只多一轮读；归属是否仍成立由恢复侧在锁内比对当前事实决定。
// toArtifactOptional 该动作是否允许没有目标产物（见 BeginPublishReceipt 的空 to 校验）。
func toArtifactOptional(action string) bool {
	switch strings.TrimSpace(action) {
	case pubdto.ReceiptActionUpdateURL, pubdto.ReceiptActionDeactivate:
		return true
	default:
		return false
	}
}

func normalizePathIdentities(in map[string]pubdto.ReceiptPathIdentity) map[string]pubdto.ReceiptPathIdentity {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]pubdto.ReceiptPathIdentity, len(in))
	for raw, id := range in {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		clean := pubdto.ReceiptPathIdentity{
			ArtifactID: strings.TrimSpace(id.ArtifactID),
			ReceiptID:  strings.TrimSpace(id.ReceiptID),
			Locator:    strings.TrimSpace(id.Locator),
		}
		if clean.ArtifactID == "" && clean.ReceiptID == "" && clean.Locator == "" {
			continue
		}
		out[path] = clean
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeReceiptID(raw string) string {
	return strings.TrimSpace(raw)
}

// deactivationOwnerRejected 拒绝两类不合法的下线归属（都属受控参数错误）：
//   - project 来源的 SourceID 必须是**本工程**的 UUID（与登记时同一判据），
//     否则一条回执会挂在别的工程上，而 RLS 与收敛的工程筛选都以 projectID 为准；
//   - 除 page / presentation / project 之外的来源类型一律不接受。
func deactivationOwnerRejected(owner pubdto.ReceiptOwner, projectID string) bool {
	sourceType := strings.TrimSpace(owner.SourceType)
	sourceID := strings.TrimSpace(owner.SourceID)
	switch sourceType {
	case pubdto.ReceiptSourcePage, pubdto.ReceiptSourcePresentation:
		return false
	case pubdto.ReceiptSourceProject:
		return sourceID == "" || sourceID != strings.TrimSpace(projectID)
	default:
		return true
	}
}

// receiptOwnerOf 决定回执归属：默认 page / presentation 二选一，只有**项目级下线**
// 走显式 Owner。
//
// 允许的显式归属只有一种组合，写死在这里而不是"信任调用方":
//   - Action 必须是 deactivate（其它动作总有 page 或 presentation 在动作）；
//   - Owner.SourceType 必须是 'project'（其它来源类型必须走 parseRouteOwner）;
//   - Owner.SourceID 必须等于 req.ProjectID（否则一条回执会挂在"别的工程"上，
//     而 RLS 与收敛的工程筛选都以 ProjectID 为准，两边不一致就只能靠人眼发现）；
//   - PageID / PresentationID 必须为空（同时给实体归属就有两个 owner，恢复时
//     会按两个来源各领一次）。
//
// 这不是任意 source 泛化：除 project 之外的来源一律拒绝，收敛例程按来源筛选时
// 不会因为"多了一种来源"而认领到别人的回执。
func receiptOwnerOf(req *pubdto.BeginPublishReceiptReq) (routeOwner, error) {
	owner := req.Owner
	if owner == nil || owner.IsZero() {
		return parseRouteOwner(req.PageID, req.PresentationID)
	}
	sourceType := strings.TrimSpace(owner.SourceType)
	sourceID := strings.TrimSpace(owner.SourceID)
	if sourceType != pubdto.ReceiptSourceProject {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	if receiptAction(req.Action, publishReceiptAction) != pubdto.ReceiptActionDeactivate {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	if strings.TrimSpace(req.PageID) != "" || strings.TrimSpace(req.PresentationID) != "" {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	if sourceID == "" || sourceID != strings.TrimSpace(req.ProjectID) {
		return routeOwner{}, pubenums.Err(pubenums.ErrInvalidParam)
	}
	// 回执表没有 project 归属列（source_type/source_id 就是归属全部），
	// 用 routeOwner{projectID: ...} 承载即可：pageIDPtr / presentationIDPtr 都为 nil。
	return routeOwner{projectID: sourceID}, nil
}

// BeginPublishReceipt 登记 pending 回执。**必须在访问面切换之前调用** ——
// 登记放在切换之后，崩溃窗口里就查不到「这次发布发生过」。
func (s *Service) BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return "", pubenums.Err(pubenums.ErrInvalidParam)
	}
	action := receiptAction(req.Action, publishReceiptAction)
	to := strings.TrimSpace(req.ToArtifactID)
	// 允许空 ToArtifactID 的动作只有两类：
	//   - update_url：产物按新路径现编译，登记时还没有产物行（见 DTO 字段注释）；
	//   - deactivate：下线的结果是不存在，本来就没有"目标产物"。
	// 其余动作仍要求给出目标产物，否则恢复无从判定。写成函数而不是逐处 if：
	// 以后再加动作时漏改的概率远低于漏改一串并列条件。
	if to == "" && !toArtifactOptional(action) {
		return "", pubenums.Err(pubenums.ErrInvalidParam)
	}
	owner, perr := receiptOwnerOf(req)
	if perr != nil {
		return "", perr
	}
	// 版本事实如实照抄调用方交来的值，这里不做任何「补齐」：
	// 用回执创建时刻的当前版本替空值，会把「提交时看到的版本」冒充成「编译捕获的版本」，
	// 收敛随后就会拿一个亲眼没见过的值去判「产物是否过期」。负值没有语义，归一到 0（未知）。
	captured := req.CapturedInputVersion
	if captured < 0 {
		captured = 0
	}
	data, merr := json.Marshal(publishReceiptData{
		ProjectID:            strings.TrimSpace(req.ProjectID),
		Lang:                 strings.TrimSpace(req.Lang),
		OldPath:              strings.TrimSpace(req.OldPath),
		OldPaths:             normalizePathList(req.OldPaths),
		Redirect:             req.Redirect,
		CapturedInputVersion: captured,
		SuccessfulReceipts:   normalizeReceiptLinks(req.SuccessfulReceipts),
		FromReceiptID:        normalizeReceiptID(req.FromReceiptID),
		PathArtifacts:        normalizePathIdentities(req.PathArtifacts),
	})
	if merr != nil {
		return "", merr
	}
	entity := &pubmodel.ReceiptEntity{
		SourceType:   owner.sourceType(),
		SourceID:     owner.sourceID(),
		Action:       action,
		Path:         req.Path,
		ReceiptState: pubmodel.ReceiptPending,
		ReceiptData:  data,
		CreateTime:   time.Now().UTC(),
	}
	if to != "" {
		entity.ToArtifact = &to
	}
	if id := strings.TrimSpace(req.FromArtifactID); id != "" {
		entity.FromArtifact = &id
	}
	if err = s.model.CreateReceipt(ctx, entity); err != nil {
		return "", err
	}
	return strconv.FormatInt(entity.ID, 10), nil
}

// CompletePublishReceipt 标记回执完成（数据库状态已与访问面一致）。
func (s *Service) CompletePublishReceipt(ctx context.Context, receiptID string) (err error) {
	return s.finishReceipt(ctx, receiptID, pubmodel.ReceiptCommitted)
}

// AbortPublishReceipt 标记回执已回滚（切换没发生，或明确失败且无副作用）。
func (s *Service) AbortPublishReceipt(ctx context.Context, receiptID string) (err error) {
	return s.finishReceipt(ctx, receiptID, pubmodel.ReceiptRolledBack)
}

// finishReceipt 写终态。空 id 视为无需处理（调用方可能还没走到登记那一步）；
// 只改 pending 行，重复调用（重试）是幂等的。
func (s *Service) finishReceipt(ctx context.Context, receiptID, state string) (err error) {
	id := strings.TrimSpace(receiptID)
	if id == "" {
		return nil
	}
	rid, perr := strconv.ParseInt(id, 10, 64)
	if perr != nil {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.FinishPendingReceipt(ctx, rid, state, time.Now().UTC())
}

// MarkDeactivationCommittedTx 在**调用方业务事务内**写"下线意图的业务变更已提交"事实。
//
// 直接转发 model：本模块不对状态做二次解释。receiptID 是十进制回执 id 串
// （BeginPublishReceipt 的返回值），解析失败即参数错误 —— 绝不当成"没有回执"放过。
func (s *Service) MarkDeactivationCommittedTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
	projectID, receiptID string, pathArtifacts map[string]pubdto.ReceiptPathIdentity) error {
	if tx == nil || owner.IsZero() || deactivationOwnerRejected(owner, projectID) {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(receiptID), 10, 64)
	if perr != nil || rid <= 0 {
		return pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.MarkDeactivationCommittedTx(tx, owner, projectID, rid, pathArtifacts)
}

// LoadDeactivationReceiptTx 在调用方事务内（通常已持业务实体锁）重读一条下线回执的当前事实。
func (s *Service) LoadDeactivationReceiptTx(ctx context.Context, tx *gorm.DB, owner pubdto.ReceiptOwner,
	projectID, receiptID string) (row *pubmodel.ReceiptEntity, err error) {
	if tx == nil || owner.IsZero() || deactivationOwnerRejected(owner, projectID) {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(receiptID), 10, 64)
	if perr != nil || rid <= 0 {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.LoadDeactivationReceiptTx(tx, owner, projectID, rid)
}

// ReceiptAttemptVerdictTx 事务内判定关联回执整体是否成功（contract 读端口，见接口声明）。
//
// 直接转发 model：本模块不对"状态是什么"再做解释 —— 三态的语义在 model 那一处定义，
// 两处各写一份阈值/分支是这类多态判定最常见的漂移来源。
func (s *Service) ReceiptAttemptVerdictTx(ctx context.Context, tx *gorm.DB, links map[string]string,
	ownedLangs map[string]string) (pubdto.ReceiptAttemptVerdict, error) {
	return s.model.ReceiptAttemptVerdictTx(tx, links, ownedLangs)
}

// ListPendingReceipts 列出未完成的回执（启动全量恢复的输入）。
func (s *Service) ListPendingReceipts(ctx context.Context) (list []pubdto.PendingReceiptResp, err error) {
	rows, lerr := s.model.ListPendingReceipts(ctx)
	if lerr != nil {
		return nil, lerr
	}
	return pendingReceiptResps(rows), nil
}

// ClaimPendingReceipts 领取一批待收敛的未结案回执（多实例安全，见 model.ClaimPendingReceipts）。
//
// 领取口径由调用方给（source_type + 动作名）：同一张表上叠着多套恢复职责，
// 领到不属于自己的行只会被反复领取再跳过，还会把真正待办的行挤出单批上限。
func (s *Service) ClaimPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (list []pubdto.PendingReceiptResp, err error) {
	limit := 0
	if req != nil {
		limit = req.Limit
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, cerr := s.model.ClaimPendingReceipts(ctx, req.SourceType, req.Actions, limit)
	if cerr != nil {
		return nil, cerr
	}
	return pendingReceiptResps(rows), nil
}

// CountPendingReceipts 统计仍未结案的回执数（只读观测：健康检查 / 后台）。
//
// 与 ClaimPendingReceipts 用同一套过滤条件，保证「报出来的数」与「收敛会处理的行」是同批。
func (s *Service) CountPendingReceipts(ctx context.Context, req *pubdto.ReceiptsQueryReq) (n int64, err error) {
	sourceType, actions := "", []string(nil)
	if req != nil {
		sourceType, actions = req.SourceType, req.Actions
	}
	return s.model.CountPendingReceipts(ctx, sourceType, actions)
}

// pendingReceiptResps 把回执行投影成对外视图（列表 / 领取共用同一份投影）。
func pendingReceiptResps(rows []pubmodel.ReceiptEntity) []pubdto.PendingReceiptResp {
	list := make([]pubdto.PendingReceiptResp, 0, len(rows))
	for i := range rows {
		list = append(list, pendingReceiptRespOf(rows[i]))
	}
	return list
}

// pendingReceiptRespOf 单行投影：receipt_data 里的工程 / 语言 / 旧路径是恢复时定位
// 页面与处置旧路径的唯一依据，解析失败按零值走（恢复例程会按「证据不足」保守判定）。
//
// 不导出：投影只服务本包的 ListPendingReceipts / ClaimPendingReceipts 两条读路径，
// 测试一律走真实 PG + 真实列表（fake row 直投会绕开 jsonb 落库与读回两跳）。
func pendingReceiptRespOf(row pubmodel.ReceiptEntity) pubdto.PendingReceiptResp {
	var data publishReceiptData
	if len(row.ReceiptData) > 0 {
		_ = json.Unmarshal(row.ReceiptData, &data)
	}
	item := pubdto.PendingReceiptResp{
		ID: strconv.FormatInt(row.ID, 10), SourceType: row.SourceType, SourceID: row.SourceID,
		ProjectID: data.ProjectID, Lang: data.Lang, Path: row.Path, Action: row.Action,
		OldPath: data.OldPath, OldPaths: data.OldPaths, Redirect: data.Redirect,
		// 缺键 / 解析失败 / 旧回执一律 0（未知）。下游只能把它当「没有版本事实」，
		// 不得当「版本有效」—— 有效性的唯一判据是与行锁内读到的当前代次相等。
		CapturedInputVersion: data.CapturedInputVersion,
		SuccessfulReceipts:   data.SuccessfulReceipts,
		FromReceiptID:        data.FromReceiptID,
		PathArtifacts:        data.PathArtifacts,
		BusinessCommitted:    data.BusinessCommitted,
		CreatedAt:            row.CreateTime.Format(time.RFC3339),
	}
	if row.FromArtifact != nil {
		item.FromArtifactID = *row.FromArtifact
	}
	if row.ToArtifact != nil {
		item.ToArtifactID = *row.ToArtifact
	}
	return item
}
