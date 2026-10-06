package pageservice

// page_translation_miss.go — 缺译报告（U2）：按 页面 × 语言 列出「有内容没译」的地方。
//
// 与站点级准入（U1，project.SaveLocales 的词条门槛）的分工，两边是不同粒度的事：
//
//   - **U1 拦的是「这个语言整体没准备好」**：一个界面词条都没有的语言不该被启用，
//     否则整站固定文案逐字段回退原文（运营看到「已启用」、访客看到原始语言）；
//   - **U2 处理的是「语言准备好了，但某些页面的内容没译」**：界面词条齐了（所以语言
//     合法），而某个页面的内容译文缺 —— 那是**逐页面**的决策：要么补译文，要么把这一页
//     从这个语言撤下来（`ExcludePageLang`，本报告的操作列就是它）。
//
// 两个动作的终点一致（该语言不该出现在这一页上），但起点与代价不同：U1 拒绝保存（拦住
// 整站），U2 精准下线一个页面的一种语言。混在一起会让「一种语言整站不可用」与「一页缺译」
// 用同一个开关表达。

import (
	"context"
	"errors"
	"sort"
	"strings"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
)

// UntranslatedPageLangs 列出该工程缺译的（页面 × 语言），只含 misses > 0。
//
// 已排除该语言的页面**不出现在报告里**：排除之后这一页不再产出该语言，
// 也就不存在「缺译」——继续列出来会让运营以为「点了取消但它还在」，
// 于是反复点（而第二次会得到「已被排除」的提示）。
func (s *Service) UntranslatedPageLangs(ctx context.Context, projectID string) (rows []pagedto.TranslationMissRow, err error) {
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return nil, ErrInvalidParam
	}
	// 两步取数：先用自己的 pages 表算出「本工程有哪些页面」，把 page_id 清单交给
	// artifact 契约去问 page_artifacts（那张表属它）。合在一条 SQL 里时会 JOIN 到别人的表上，
	// 而「哪些页面属于本工程」本来就是本模块最清楚的事。
	titles, err := s.model.ListPageTitles(ctx, pid)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(titles))
	pathOf := make(map[string]string, len(titles))
	for i := range titles {
		ids = append(ids, titles[i].ID)
		pathOf[titles[i].ID] = titles[i].DraftPath
	}
	if s.pageArtifacts == nil {
		return nil, errors.New("page: artifact 契约未注入，无法读取产物缺译计数")
	}
	raw, err := s.pageArtifacts.TranslationMisses(ctx, ids)
	if err != nil {
		return nil, err
	}
	excluded, err := s.excludedLangsOfProject(ctx, pid)
	if err != nil {
		return nil, err
	}
	rows = make([]pagedto.TranslationMissRow, 0, len(raw))
	for i := range raw {
		r := raw[i]
		if excluded[r.PageID][strings.TrimSpace(r.Lang)] {
			continue
		}
		rows = append(rows, pagedto.TranslationMissRow{
			PageID: r.PageID, DraftPath: pathOf[r.PageID], Lang: r.Lang,
			Misses: r.Misses, Candidates: r.Candidates,
		})
	}
	// 排序口径与原 SQL 一致（misses 降序，再 draft_path / lang 升序）——契约只保证
	// 「每页每语言一行」，顺序由调用方按展示需要定，那里才有 draft_path。
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Misses != rows[j].Misses {
			return rows[i].Misses > rows[j].Misses
		}
		if rows[i].DraftPath != rows[j].DraftPath {
			return rows[i].DraftPath < rows[j].DraftPath
		}
		return rows[i].Lang < rows[j].Lang
	})
	return rows, nil
}

// excludedLangsOfProject 该工程各页面已排除的语言集合（page_id → lang → true）。
//
// 一次查询而不是逐页问：报告最长会列出全部页面 × 语言，逐页查会退化成 N 次往返，
// 而它在每次打开报告页时都要跑一遍。
func (s *Service) excludedLangsOfProject(ctx context.Context, projectID string) (map[string]map[string]bool, error) {
	pages, err := s.model.ListAll(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]bool, len(pages))
	for i := range pages {
		if len(pages[i].ExcludedLangs) == 0 {
			continue
		}
		set := make(map[string]bool, len(pages[i].ExcludedLangs))
		for _, l := range pages[i].ExcludedLangs {
			if v := strings.TrimSpace(l); v != "" {
				set[v] = true
			}
		}
		out[pages[i].ID] = set
	}
	return out, nil
}

// 编译期用途说明：本文件只用 model 的具名查询方法（ListPageTitles / ListAll），
// 不碰裸句柄 —— service 层的数据访问边界由 scripts/check-service-db-boundary.sh 守门。
// 缺译计数本身经 artifact 契约取（page_artifacts 属 artifact 模块）。
var _ = pagemodel.PageTitleRow{}
