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
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"go_wp/internal/module/publication/dto"
	"go_wp/internal/module/publication/enums"
	"go_wp/internal/module/publication/model"
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
}

// BeginPublishReceipt 登记 pending 回执。**必须在访问面切换之前调用** ——
// 登记放在切换之后，崩溃窗口里就查不到「这次发布发生过」。
func (s *Service) BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	action := receiptAction(req.Action, publishReceiptAction)
	to := strings.TrimSpace(req.ToArtifactID)
	// update_url 的产物是切换时按新路径现编译的，登记时还没有产物行（见 DTO 字段注释），
	// 因此只对它放行空 ToArtifactID；其余动作仍要求给出目标产物，否则恢复无从判定。
	if to == "" && action != pubdto.ReceiptActionUpdateURL {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	owner, perr := parseRouteOwner(req.PageID, req.PresentationID)
	if perr != nil {
		return "", perr
	}
	data, merr := json.Marshal(publishReceiptData{
		ProjectID: strings.TrimSpace(req.ProjectID),
		Lang:      strings.TrimSpace(req.Lang),
		OldPath:   strings.TrimSpace(req.OldPath),
		Redirect:  req.Redirect,
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
		return errors.New(pubenums.ErrInvalidParam)
	}
	return s.model.FinishPendingReceipt(ctx, rid, state, time.Now().UTC())
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
func pendingReceiptRespOf(row pubmodel.ReceiptEntity) pubdto.PendingReceiptResp {
	var data publishReceiptData
	if len(row.ReceiptData) > 0 {
		_ = json.Unmarshal(row.ReceiptData, &data)
	}
	item := pubdto.PendingReceiptResp{
		ID: strconv.FormatInt(row.ID, 10), SourceType: row.SourceType, SourceID: row.SourceID,
		ProjectID: data.ProjectID, Lang: data.Lang, Path: row.Path, Action: row.Action,
		OldPath: data.OldPath, Redirect: data.Redirect,
		CreatedAt: row.CreateTime.Format(time.RFC3339),
	}
	if row.FromArtifact != nil {
		item.FromArtifactID = *row.FromArtifact
	}
	if row.ToArtifact != nil {
		item.ToArtifactID = *row.ToArtifact
	}
	return item
}
