// ai_session_calls.go — 会话行的「最近调用」预览：把 ai_call_log 的流水读成悬浮卡能渲染的形状。
//
// 与 ai_session_usage.go 的分工：那边算的是**聚合**（这条会话在某家模型上总共烧了多少），
// 这边取的是**流水**（每一次调用各自多久、多少 token、成没成）。两者消费同一批会话 id，
// 但查询、口径与展示位置都不同 —— 合一个文件只会让「改聚合时误伤流水」。
package aiservice

import (
	"context"
	"fmt"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

// sessionCallPreview 悬浮卡里显示的最近调用条数。
//
// 5 条是「一停就能看完」的上限：悬浮卡不是详情页，再多就得滚动，而滚动区在鼠标移开时
// 会消失 —— 那反而看不成。所以总条数另行给出，读的人知道自己是看到了全部还是片段。
const sessionCallPreview = 5

// CallLogReader 会话层需要的调用流水读取能力（由 model 实现，装配期注入）。
//
// 用窄接口而不是直接持 *aimodel.CallLogModel：会话层对这张表只要「批量取最近几条」这一件事，
// 端口形状把能力收窄到这一件事上（用例也能塞一个假读取器）。
type CallLogReader interface {
	RecentCallsBySession(ctx context.Context, sessionIDs []int64, perSession int) ([]aimodel.SessionCallLogRow, error)
}

// SetCallLogReader 注入调用流水读取端口；未注入时 SessionCallsOf 回空（不 panic）。
func (s *SessionService) SetCallLogReader(r CallLogReader) { s.callLog = r }

// SessionCallsOf 批量取一组会话的调用流水预览（最近若干条 + 每条会话的总条数）。
//
// 一次查完整页（见 model.RecentCallsBySession）：列表 20 行逐行查就是 20 次往返，
// 而这块数据是「鼠标一停就要看到」的。
// 失败向上返回：调用方（页面）自己决定是整块不显示还是给一句提示。
func (s *SessionService) SessionCallsOf(ctx context.Context, sessionIDs []int64) (map[int64]aidto.SessionCalls, error) {
	out := make(map[int64]aidto.SessionCalls, len(sessionIDs))
	if s.callLog == nil || len(sessionIDs) == 0 {
		return out, nil
	}
	rows, err := s.callLog.RecentCallsBySession(ctx, sessionIDs, sessionCallPreview)
	if err != nil {
		return out, err
	}
	for i := range rows {
		row := rows[i]
		item := out[row.SessionID]
		item.Total = row.Total
		item.Rows = append(item.Rows, callRowOf(row))
		out[row.SessionID] = item
	}
	return out, nil
}

// callRowOf 把一条流水翻成展示形状。
//
// 失败原因的取法：ErrorKey 是 i18n key（哨兵值），ErrorText 取 FacingMessages 的中文兜底 ——
// 模板写 tr(key, fallback)，与页面其它错误提示走同一条链。不在白名单里的 key
// （理论上不会有，callErrorKey 已经归口过）兜底成「调用失败」，绝不让一个裸 key 上页面。
func callRowOf(row aimodel.SessionCallLogRow) aidto.SessionCallRow {
	ok := row.Status == string(aienums.CallStatusOK)
	out := aidto.SessionCallRow{
		Time:        row.CreateTime.Format("01-02 15:04"),
		ProviderKey: row.ProviderKey,
		ModelID:     row.ModelID,
		LatencyText: formatLatency(row.LatencyMs),
		Tokens:      row.TotalTokens,
		TokensText:  formatTokens(row.TotalTokens),
		OK:          ok,
	}
	if !ok {
		// status 不是 ok 却没记错误原因（历史上可能被写进默认空值）：归口到 ErrInternal，
		// 而不是把一个空 key 交给模板 —— tr("") 的行为取决于 i18n 实现，不该让它决定页面长什么样。
		key := strings.TrimSpace(row.ErrorKey)
		if key == "" {
			key = aienums.ErrInternal
		}
		out.ErrorKey = key
		if text, has := aienums.FacingText(key); has {
			out.ErrorText = text
		} else {
			out.ErrorText = "调用失败"
		}
	}
	return out
}

// formatLatency 把毫秒翻成短文本。
//
// 阈值取 1 秒：低于 1 秒的调用用毫秒读更准（「480ms」比「0.5s」有信息量），
// 高于 1 秒的用秒读更省心（「2.9s」比「2900ms」一眼看得出量级）。
func formatLatency(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
