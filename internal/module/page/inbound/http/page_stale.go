package pagehttp

import (
	"context"
	"errors"
	"fmt"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"strings"
	"time"
)

type receiptBacklogObserver interface {
	PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error)
}

func (h *pagesAdminHandle) receiptBacklog(ctx context.Context) (pending int64, oldest string, last *utils.JSONTime) {
	observer, ok := h.pages.(receiptBacklogObserver)
	if !ok {
		return 0, "", nil
	}
	count, oldestAge, lastConvergeAt, err := observer.PendingReceiptBacklog(ctx)
	if err != nil {
		logger.Scene("page").With("err", err).Warn("读取待收敛发布回执失败（列表页不显示回执状态）")
		return 0, "", nil
	}
	if count > 0 && oldestAge > 0 {
		oldest = formatReceiptAge(oldestAge)
	}

	if !lastConvergeAt.IsZero() {
		last = utils.NewJSONTimePtr(&lastConvergeAt)
	}
	return count, oldest, last
}

func formatReceiptAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

const staleOverviewLimit = 8

func (h *pagesAdminHandle) staleOverview(ctx context.Context) *StaleOverviewResp {
	empty := &StaleOverviewResp{
		Available: true, Total: 0, Pages: []StaleItemResp{},

		Limit: staleOverviewLimit, Truncated: false,
	}
	unavailable := &StaleOverviewResp{
		Available: false, Total: 0, Pages: []StaleItemResp{},
		Limit: 0, Truncated: false,
	}
	if h == nil || h.pages == nil {

		return unavailable
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{

		Limit:      staleOverviewLimit,
		Descending: true,
	})
	if err != nil {
		if errors.Is(err, pageservice.ErrProjectRequired) {
			return empty
		}
		logger.Scene("page").With("err", err).Warn("读取全站待重建清单失败（页面列表页的该区块显示为不可用）")
		return unavailable
	}
	if res == nil {

		logger.Scene("page").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable
	}
	items := make([]StaleItemResp, 0, len(res.Pages))
	for i := range res.Pages {
		stage, at := rebuildFailureFact(res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt)
		items = append(items, StaleItemResp{
			ID:                  res.Pages[i].ID,
			Path:                strings.TrimSpace(res.Pages[i].Path),
			ProjectName:         strings.TrimSpace(res.Pages[i].ProjectName),
			Published:           res.Pages[i].Published,
			FailedStageKey:      stage.Key,
			FailedStageFallback: stage.Fallback,
			FailedAt:            at,
		})
	}
	return &StaleOverviewResp{
		Available: true,
		Total:     res.Total,
		Pages:     items,
		Limit:     res.Limit,
		Truncated: res.Truncated,
	}
}

func rebuildFailureFact(stage string, at *utils.JSONTime) (pageMsg, *utils.JSONTime) {
	stage = strings.TrimSpace(stage)
	if stage == "" && at == nil {
		return pageMsg{}, nil
	}

	key, fallback := pageenums.RebuildStageLabel(stage)
	return pageMsg{Key: key, Fallback: fallback}, at
}
