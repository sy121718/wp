package pagehttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
	"strings"
	"time"
)

// receiptBacklogObserver 收敛积压的只读观测（page service 实现）。
//
// 用隐式接口而不是扩 pagecontract：可观测不是跨模块能力（与 routers 侧的
// pendingReceiptBacklog 同一判据），契约扩一次会让所有测试替身跟着实现一遍，
// 而这只服务列表页上的一个状态条。
type receiptBacklogObserver interface {
	PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error)
}

// receiptBacklog 读取待收敛回执的观测值，转成**事实**：待收敛条数 / 最老一条的等待时长文本 /
// 本进程最近一次收敛时刻（时间对象 —— 格式化由模板的 dateTimeSecond() 决定，秒精度）。
//
// 未实现观测（测试替身 / 降级装配）与读取失败都返回零值：状态条退化成「正常」那一支，
// 列表页本身不受影响。失败只记日志 —— 这是观测，不是页面数据
// （原文不进模板，见 AGENTS.md 的错误文案三件套）。
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
	// 零值给 nil 而不是零值时间：模板按「空串」分流，而零值时间会被格式化层渲染成
	// 0001-01-01（伪造时刻）。旧实现用 IsZero 守卫避开了它，这里把守卫的**结果**交出去。
	if !lastConvergeAt.IsZero() {
		last = utils.NewJSONTimePtr(&lastConvergeAt)
	}
	return count, oldest, last
}

// formatReceiptAge 把等待时长压成人读的一行（秒 / 分 / 小时 / 天）。
//
// 不做 i18n：单位是 SI 记法（s/m/h/d），中英文都读得懂，也不需要复数规则 ——
// 为它造四条词条只会让词条表更长，不带来任何可读性。
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

// staleOverviewLimit 「全站待重建」区块一次列出的页面数。
//
// 这是**消费者口径**，所以定义在调用方：ListStalePages 的 limit 由调用方给
// （service 不写死业务条件），不填时才落到 model 的 DefaultStaleListLimit = 50 ——
// 一份 50 行的折叠清单会把页面列表顶出首屏，而那正是审计 02-L P1-10 记录的原缺陷
// （只读影响面卡占了列表主位）。
//
// 数为什么是 8：只读区块的作用是「让人看见影响面」，不是完整清单；被截断的条数由 Total
// 给出并在页面上显式说明。service 里那份同名同值的未导出常量已随之删除（它没有任何调用方，
// 留着会让「这个数由谁定」出现两个答案）。
const staleOverviewLimit = 8

// staleOverview 取「全站待重建」区块的数据（只读观测）。
//
// 作用域：ProjectID 传空 = **全部站点工程**。这与本页下方列表的口径不同 —— 列表是单工程聚焦
// （focusProjectID 从 ?project= 解析），而块 / 文章 / 主题 / 词条改动影响的是全站。
// 两个数不是同一个，模板侧把「全站」写进标题与说明。
//
// 失败不降级成空清单：ListStalePages 的语义是「读不到即失败」，这里把它翻成 Available=false
// （模板显示「本次读不到」），**绝不渲染成「0 个待重建」**—— 「影响面 0」与「读不到影响面」
// 混在一起，会让一次读取失败在页面上看起来像一切正常，运维再也不会去看
// （判据与发布回执观测的 ReceiptKnown 一致）。原文只进日志。
//
// 唯一的例外是「一个站点工程都没有」：ListStalePages 按语义返回 ErrProjectRequired
// （没有可作用域的工程），而那时全站确实没有任何页面 —— 那是确定的事实，不是读取失败，
// 按空态处理（此时下方列表也正落在「还没有页面」那一档）。
// tr 由调用方传入（取词只在 handler 层做）：本函数要组装「最近一次重建失败」的文案，
// 而它自己拿不到 gin.Context。
func (h *pagesAdminHandle) staleOverview(ctx context.Context) gin.H {
	empty := gin.H{
		"Available": true, "Total": 0, "Pages": []pagedto.StaleItem{},
		// Limit 给真实口径（模板在空态下不读它，但零值会让「清单上限是多少」在两个分支里
		// 出现两种答案 —— 将来若空态也要说一句「最多列 8 条」，零值就是错的）。
		"Limit": staleOverviewLimit, "Truncated": false,
	}
	unavailable := gin.H{
		"Available": false, "Total": 0, "Pages": []pagedto.StaleItem{},
		"Limit": 0, "Truncated": false,
	}
	if h == nil || h.pages == nil {
		// 契约未注入（降级装配、或只覆盖写路径的测试句柄）：与「读不到」同形，
		// 但没有错误可记，静默给失败态即可。
		return unavailable
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		// 条数由本页给（不填会落到 model 的 50 条默认），排序按标记时间倒序：
		// 「最近这次改动影响的」排在最前（service 文件头对这个消费者的描述就是这个次序）。
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
		// 契约返回 (nil, nil) 是异常形态：按「读不到」处理，不给「0 个待重建」的假结论。
		logger.Scene("page").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable
	}
	items := make([]pagedto.StaleItem, 0, len(res.Pages))
	for i := range res.Pages {
		stage, at := rebuildFailureFact(res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt)
		items = append(items, pagedto.StaleItem{
			ID:                  res.Pages[i].ID,
			Path:                strings.TrimSpace(res.Pages[i].Path),
			ProjectName:         strings.TrimSpace(res.Pages[i].ProjectName),
			Published:           res.Pages[i].Published,
			FailedStageKey:      stage.Key,
			FailedStageFallback: stage.Fallback,
			FailedAt:            at,
		})
	}
	return gin.H{
		"Available": true,
		"Total":     res.Total,
		"Pages":     items,
		"Limit":     res.Limit,
		"Truncated": res.Truncated,
	}
}

// rebuildFailureNote 组装「最近一次自动重建失败」的一句文案；没有失败痕迹时返回空串。
//
// 阶段与时刻来自迁移 474 落在 pages 上的两列；**不含错误原文**（后台页面不得直出内部错误，
// 原文只进结构化日志，带 page_id 可定位）。与 /admin/articles、/admin/blocks 上同名函数
// 是三份：各自在自己的模块包内（跨模块共用要走契约，而这是纯展示装配）。
// 三处文案与阶段取值必须一致，改一处请同步另两处。
// rebuildFailureFact 失败痕迹 → **事实**（阶段标签 + 时刻）。
//
// 原先这里把三段拼成一句成品串交给模板直出（`{{.FailedNote}}`）：拼接的那一刻就是
// 控制器替渲染期定语言的时刻，而语言是请求级的。拆成事实之后三段各自由模板组合
// （前缀词条 + 阶段词条 + siteDateTime），控制器不再需要取词函数参数 ——
// 这也是 staleOverview 的 `tr` 参数被删掉的原因。
//
// 返回的时刻与入参同一个（nil 即没有时刻），调用点原样塞进视图字段。
func rebuildFailureFact(stage string, at *utils.JSONTime) (pageMsg, *utils.JSONTime) {
	stage = strings.TrimSpace(stage)
	if stage == "" && at == nil {
		return pageMsg{}, nil
	}
	// 阶段 → 文案的映射归 pageenums（值域与词条 key 都属本模块 enums，
	// 见 internal/module/CLAUDE.md §enums）：未知阶段在那边回落到原值 ——
	// 页面上的「plan2」至少能让人去查，一句通用话不能。
	key, fallback := pageenums.RebuildStageLabel(stage)
	return pageMsg{Key: key, Fallback: fallback}, at
}
