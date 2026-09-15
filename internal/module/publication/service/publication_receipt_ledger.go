// publication_receipt_ledger.go —— 访问面切换的发布回执（TX-009）。
//
// 与 Activate 内部已有的回执是同一张表、同一套状态机（pending / committed /
// rolled_back），区别只在**时机**：Activate 的回执描述「路由行写入」，
// 这里的回执描述「访问面切换」—— 后者才是不可回退的那一步。
//
// 崩溃恢复的判据（page 侧 RecoverPendingPublications 使用）：
//
//	pending 存在 + 符号链接指向回执记录的产物 → 切换成功、DB 没跟上 → 补完成
//	pending 存在 + 符号链接指向别处或不存在    → 切换没发生 → 标 rolled_back
package pubservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"

	"strconv"
)

// publishReceiptAction 访问面切换回执的动作名（与 Activate 的路由回执区分开：
// 恢复流程要按它筛出「切换类」回执，不能把普通路由变更一起卷进来）。
const publishReceiptAction = "switch_active"

// publishReceiptData 回执附加数据。
//
// 放进 receipt_data 是因为 publication_receipts 表没有 project_id 与 lang 列，
// 而这两项是「补完成」时定位页面与语言的必要信息（补完成要写 page_publications）。
type publishReceiptData struct {
	ProjectID string `json:"projectId,omitempty"`
	Lang      string `json:"lang,omitempty"`
}

// BeginPublishReceipt 登记 pending 回执。**必须在访问面切换之前调用** ——
// 登记放在切换之后，崩溃窗口里就查不到「这次发布发生过」。
func (s *Service) BeginPublishReceipt(ctx context.Context, req *pubdto.BeginPublishReceiptReq) (receiptID string, err error) {
	if req == nil || strings.TrimSpace(req.Path) == "" || strings.TrimSpace(req.ToArtifactID) == "" {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	owner, perr := parseRouteOwner(req.PageID, req.PresentationID)
	if perr != nil {
		return "", perr
	}
	data, merr := json.Marshal(publishReceiptData{
		ProjectID: strings.TrimSpace(req.ProjectID),
		Lang:      strings.TrimSpace(req.Lang),
	})
	if merr != nil {
		return "", merr
	}
	to := strings.TrimSpace(req.ToArtifactID)
	entity := &pubmodel.ReceiptEntity{
		SourceType:   owner.sourceType(),
		SourceID:     owner.sourceID(),
		Action:       publishReceiptAction,
		Path:         req.Path,
		ToArtifact:   &to,
		ReceiptState: pubmodel.ReceiptPending,
		ReceiptData:  data,
		CreateTime:   time.Now().UTC(),
	}
	if id := strings.TrimSpace(req.FromArtifactID); id != "" {
		entity.FromArtifact = &id
	}
	if err = s.model.ReceiptDB(ctx).Create(entity).Error; err != nil {
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
	return s.model.ReceiptDB(ctx).
		Where("id = ? AND receipt_state = ?", rid, pubmodel.ReceiptPending).
		Updates(map[string]any{"receipt_state": state, "completed_at": time.Now().UTC()}).Error
}

// ListPendingReceipts 列出未完成的回执（启动恢复的输入）。
func (s *Service) ListPendingReceipts(ctx context.Context) (list []pubdto.PendingReceiptResp, err error) {
	rows, lerr := s.model.ListPendingReceipts(ctx)
	if lerr != nil {
		return nil, lerr
	}
	list = make([]pubdto.PendingReceiptResp, 0, len(rows))
	for i := range rows {
		row := rows[i]
		var data publishReceiptData
		if len(row.ReceiptData) > 0 {
			_ = json.Unmarshal(row.ReceiptData, &data)
		}
		item := pubdto.PendingReceiptResp{
			ID: strconv.FormatInt(row.ID, 10), SourceType: row.SourceType, SourceID: row.SourceID,
			ProjectID: data.ProjectID, Lang: data.Lang, Path: row.Path, Action: row.Action,
			CreatedAt: row.CreateTime.Format(time.RFC3339),
		}
		if row.FromArtifact != nil {
			item.FromArtifactID = *row.FromArtifact
		}
		if row.ToArtifact != nil {
			item.ToArtifactID = *row.ToArtifact
		}
		list = append(list, item)
	}
	return list, nil
}
