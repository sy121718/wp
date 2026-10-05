package pagemcp

// page_tools.go — 页面模块的只读工具。
//
// 为什么补这一个：用户问「有没有关于退换货的页面」「那个帮助页叫什么来着」
// 时，答案在 pages 表里，而在此之前 AI 侧看不到任何页面数据 —— 它只能去猜
// 或者让用户自己找。底层缺的不是数据而是入口。
//
// 只读：依赖收窄到 PageQueryReader（一个方法，见契约文件头），手里没有发布 /
// 回滚 / 删除 —— 「AI 顺手把一个页面下线了」不会在某次改动里变得可能。
//
// **关键词匹配在这里做**（不在 model 层）：页面列表通道按工程与主题筛，
// 返回体还带着整份草稿文档（每页几百 KB），为「找一个页面」去拉那些正文是浪费；
// 而标题清单很轻（百级、每条几十字节），一次全拉进内存过滤即可。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/permission"
)

// pageMatchLimit 一次最多把几个匹配的页面交给模型。
//
// 与其它工具的上限一个道理：命中 80 个页面时全铺出来只会挤掉回答，
// 而用户真正要的那个通常在前几条（标题前缀命中的排在前面，见 rankMatches）。
const pageMatchLimit = 15

// Tools 返回页面模块的只读工具集。
func Tools(reader pagecontract.PageQueryReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, errors.New("pagemcp: 页面查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{pageFind(reader)}, nil
}

type pageFindArgs struct {
	ProjectID string `json:"projectId"`
	Keyword   string `json:"keyword"`
}

func pageFind(reader pagecontract.PageQueryReader) mcp.Tool {
	return mcp.New("page_find", "按线索找页面",
		"按线索找站点页面，用于回答「有没有关于退换货的页面」「那个帮助页叫什么」「页面路径是什么」。\n"+
			"keyword 会同时匹配**标题与路径**（不区分大小写）；不给 keyword 就列出这个工程的全部页面。\n"+
			"标题前缀命中的排在前面 —— 用户说得越像页面名，第一条就越可能是他要的。\n"+
			"结果里的路径（线上路径 / 草稿路径）可以直接拼成站内链接。\n"+
			"要看页面里的内容、修订历史或 SEO 设置：本工具只给标题与路径，正文不在其中。",
		permission.PageList,
		mcp.Object("查页面参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid）"),
			"keyword":   mcp.String("线索：页面标题或路径的一部分（可选；不传则列出全部）"),
		}, "projectId"),
		func(ctx context.Context, args pageFindArgs) (mcp.Result, error) {
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			all, err := reader.ListPageTitles(ctx, args.ProjectID)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: pageListText(all, args.Keyword), Data: all}, nil
		})
}

func pageListText(all []pagedto.PageTitleResp, keyword string) string {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		if len(all) == 0 {
			return "这个工程还没有建任何页面。"
		}
		shown := all
		truncated := false
		if len(shown) > pageMatchLimit {
			shown, truncated = shown[:pageMatchLimit], true
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个页面：\n", len(all))
		writePageRows(&b, shown)
		if truncated {
			fmt.Fprintf(&b, "（只列了前 %d 个；要精确找某一个，给 keyword）", pageMatchLimit)
		}
		return strings.TrimRight(b.String(), "\n")
	}

	matches := rankMatches(all, kw)
	if len(matches) == 0 {
		// 空结果要把「怎么找」说清楚：用户给的名字与页面标题不一致很常见，
		// 而这句提示能让他直接换个词再问一次，不必来回猜。
		return fmt.Sprintf("没有匹配「%s」的页面（共 %d 个页面）。可以换一个更短的词，"+
			"或者不给关键词让它把全部页面列出来。", keyword, len(all))
	}
	truncated := false
	if len(matches) > pageMatchLimit {
		matches, truncated = matches[:pageMatchLimit], true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "匹配「%s」的页面 %d 个：\n", keyword, len(matches))
	writePageRows(&b, matches)
	if truncated {
		fmt.Fprintf(&b, "（只列了前 %d 个，用更精确的词可以缩小范围）", pageMatchLimit)
	}
	return strings.TrimRight(b.String(), "\n")
}

// rankMatches 先匹配、后按「命中位置」排序。
//
// 前缀命中排在包含命中之前：用户说「帮助」时，「帮助中心」比
// 「如何联系帮助台」更可能是他要的那个。同档内按原始顺序（服务端已排好），
// 用稳定排序保住它 —— 否则两次问同一句话得到两个顺序，看起来像数据变了。
func rankMatches(all []pagedto.PageTitleResp, kw string) []pagedto.PageTitleResp {
	type scored struct {
		page pagedto.PageTitleResp
		rank int
	}
	hits := make([]scored, 0, len(all))
	for _, p := range all {
		rank := matchRank(p, kw)
		if rank < 0 {
			continue
		}
		hits = append(hits, scored{page: p, rank: rank})
	}
	// 插入排序（同档保持原序）：命中数通常在几十以内，不必引入 sort 包的开销与复杂度。
	for i := 1; i < len(hits); i++ {
		cur := hits[i]
		j := i - 1
		for j >= 0 && hits[j].rank > cur.rank {
			hits[j+1] = hits[j]
			j--
		}
		hits[j+1] = cur
	}
	out := make([]pagedto.PageTitleResp, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.page)
	}
	return out
}

// matchRank 0 = 标题前缀命中（最相关），1 = 标题包含，2 = 路径命中，-1 = 不匹配。
func matchRank(p pagedto.PageTitleResp, kw string) int {
	title := strings.ToLower(p.SEOTitle)
	if strings.HasPrefix(title, kw) {
		return 0
	}
	if strings.Contains(title, kw) {
		return 1
	}
	if strings.Contains(strings.ToLower(p.DraftPath), kw) {
		return 2
	}
	if p.ActivePath != nil && strings.Contains(strings.ToLower(*p.ActivePath), kw) {
		return 2
	}
	return -1
}

func writePageRows(b *strings.Builder, rows []pagedto.PageTitleResp) {
	for i, p := range rows {
		fmt.Fprintf(b, "%d. id=%s「%s」· 类型 %s · 线上 %s · 草稿 %s\n",
			i+1, p.ID, emptyAsDash(p.SEOTitle), kindText(p.Kind),
			activePathText(p), emptyAsDash(p.DraftPath))
	}
}

// kindText 页面类型的中文说法。
//
// 列表必须带类型：只给标题与路径分不出「这是首页还是普通页」，
// 而 page_create 只能建功能页、建之前要先确认有没有同用途的那一张。
// 普通内容页（page / article / tag）也标出来 —— 它们跟着内容走，
// 看到它们就知道「这类页面不是在这里建的」。
func kindText(kind string) string {
	switch strings.TrimSpace(kind) {
	case "home":
		return "首页"
	case "archive":
		return "归档页"
	case "search":
		return "搜索结果页"
	case "notFound":
		return "404 页"
	case "page":
		return "普通页（绑定内容）"
	case "article":
		return "文章页（绑定文章）"
	case "tag":
		return "标签页（绑定标签）"
	}
	return emptyAsDash(kind)
}

// activePathText 线上路径。未发布时**明说未发布**而不是回退成草稿路径 ——
// 两者拼出来的链接一个能打开、一个 404，混在一起会让模型给出打不开的地址。
func activePathText(p pagedto.PageTitleResp) string {
	if p.ActivePath == nil || strings.TrimSpace(*p.ActivePath) == "" {
		return "（未发布）"
	}
	return *p.ActivePath
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
