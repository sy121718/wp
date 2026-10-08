package pagemcp

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

// **这一批刻意不碰页面文档本身。** 页面的 AST（页面里放哪些块、怎么排版）
// 属于「可视化页面选组件」，那套编辑器的语义（拖拽、块树、槽位绑定）不是
// 一段 JSON 参数能表达清楚的 —— 模型盲写一份 AST，产物在编辑器里打开时
// 往往是一片无法维护的结构。所以这里只开：
//
//	建页（只有 kind 与路径，文档留给编辑器）→ 发布 → 改网址 → 删页
//
// 建页时的 DraftDocument 一律给空文档：调用方不需要、也不应该手写 AST。
// 想从模板起手就用 BlueprintID（蓝图是「用完即弃」的初始化输入，
// 服务端会把 AST 完整复制并重生成节点 ID）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
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

type pageWriter interface {
	Create(ctx context.Context, req *pagedto.CreateReq) (*pagedto.PageResp, error)
	Publish(ctx context.Context, req *pagedto.PublishReq) (*pagedto.PublishResp, error)
	UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (*pagedto.PublishResp, error)
	Delete(ctx context.Context, req *pagedto.DeleteReq) error
}

// WriteTools 返回页面写工具集（4 个）。
func WriteTools(w pageWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("pagemcp: 页面写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{pageCreate(w), pagePublish(w), pageURLUpdate(w), pageDelete(w)}, nil
}

// emptyDraftDocument 建页时的空文档。
//
// 形状必须与 builder.Page 对齐：`settings` 是对象、**`root` 是数组**（不是对象）。
// 写成 `{"root":{"type":"page"}}` 会让 builder.ParsePage 反序列化失败，
// 服务端报 ErrInvalidDocument —— 症状是「建页失败」但看不出哪里错了。
var emptyDraftDocument = json.RawMessage(`{"settings":{},"root":[]}`)

// structuralKinds 允许用本工具建的类型。
//
// **只有功能页。** pages 表有一条 content contract 约束：
// kind=page 必须带 content_target_type='page' + 非空的 content_target_id，
// 也就是说「普通页面」本质上是**某条内容（或某个主题页）的展示壳**，
// 它跟着内容走 —— 内容模块自己会带出对应的页面。
// 让调用方在这里凭空给一个 targetId，等于伪造一条内容关联。
//
// 而 home / archive / search / notFound 是站点自己的功能页，content_target_type='none'，
// 没有任何内容依赖。它们通常在建站时就已存在，这个工具的用途是「站点缺了某张功能页
// 时补一张」—— 所以描述里要求先 page_find 看一眼。
var structuralKinds = []string{"home", "archive", "search", "notFound"}

type pageCreateArgs struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
	DraftPath string `json:"draftPath"`
	Blueprint string `json:"blueprintId"`
}

func pageCreate(w pageWriter) mcp.Tool {
	return mcp.NewWrite("page_create", "新建站点功能页",
		"建一个**空的功能页**（首页 / 归档页 / 搜索结果页 / 404 页）。"+
			"页面里放什么内容由可视化编辑器来做 —— 这个工具不接收、也不生成页面结构。\n"+
			"**只能建功能页，建不了普通内容页。** 原因在数据层：pages 表要求普通页必须绑定一条内容"+
			"（文章 / 分类 / 标签），也就是说内容页是**跟着内容自动带出来的展示壳** —— "+
			"建文章时它就有了，不该在这里凭空造一个。要建内容页，去建那条内容。\n"+
			"kind：`home` 首页 / `archive` 归档页 / `search` 搜索结果页 / `notFound` 404 页。\n"+
			"**这些页面站点通常已经有了** —— 同一用途再建一张不会生效（前台只认一张）。"+
			"所以建之前先 `page_find` 看一眼，已经有的应该是去改它而不是加一张。\n"+
			"draftPath 是草稿路径（如 /about），要**以 / 开头**。它只是暂存地址，"+
			"真正对外可见要再走 `page_publish`。\n"+
			"想从模板起手可以给 blueprintId（用 blueprint 列表拿）—— "+
			"蓝图是「用完即弃」的初始化输入，服务端会把结构复制一份到新页面。",
		permission.PageCreate,
		mcp.Object("新建页面参数", map[string]mcp.Schema{
			"projectId":   mcp.String("工程 id（用 site_projects 拿）"),
			"kind":        mcp.Enum("页面类型（只有功能页；内容页跟着内容走，不在这里建）", structuralKinds...),
			"draftPath":   mcp.String("草稿路径，以 / 开头，如 /about"),
			"blueprintId": mcp.String("可选：从哪份蓝图初始化内容（不填 = 完全空白的页面）"),
		}, "projectId", "kind", "draftPath"),
		nil,
		func(ctx context.Context, args pageCreateArgs) (mcp.Result, error) {
			req := &pagedto.CreateReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				Kind:      strings.TrimSpace(args.Kind),
				DraftPath: ensureLeadingSlash(args.DraftPath),
				// 功能页没有内容依赖（pages 表的 content contract 要求 none）。
				ContentTargetType: "none",
				DraftDocument:     emptyDraftDocument,
			}
			req.BlueprintID = strings.TrimSpace(args.Blueprint)
			res, err := w.Create(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil {
				return mcp.Result{Text: "页面已创建。"}, nil
			}
			src := "空白页面"
			if req.BlueprintID != "" {
				src = "按蓝图初始化"
			}
			return mcp.Result{Text: fmt.Sprintf(
				"功能页已创建（id=%s，草稿路径 %s，%s）。\n"+
					"**现在还没有内容** —— 去可视化编辑器里放块，排版完成后再用 page_publish 上线。"+
					"没发布前前台访问不到这个地址。\n"+
					"如果站点本来就有一张同用途的页面，前台认的是原有的那张 —— "+
					"确认一下要不要删掉这张重复的。",
				res.ID, emptyAsDash(res.DraftPath), src)}, nil
		})
}

func ensureLeadingSlash(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

type pagePublishArgs struct {
	ID       string `json:"pageId"`
	Lang     string `json:"lang"`
	AllLangs bool   `json:"allLangs"`
}

func pagePublish(w pageWriter) mcp.Tool {
	return mcp.NewWrite("page_publish", "发布页面",
		"把页面的当前草稿**构建并上线**，前台从此能访问它的网址。\n"+
			"这是把编辑结果对外生效的那一步 —— 编辑器里保存只是存草稿。\n"+
			"**多语言站点**：不传 lang 且 allLangs 为假时只发一种语言；"+
			"传 allLangs=true 会按站点启用的语言清单逐语言构建 + 激活，"+
			"单种语言失败不阻断其余语言（错误按语言逐条回传，回执会列出来）。\n"+
			"发布可能要几秒（要跑构建）；这一步**会覆盖线下的同名路径**。"+
			"发布后改动立刻对访客可见，改之前先跟用户确认。",
		permission.PagePublish,
		mcp.Object("发布页面参数", map[string]mcp.Schema{
			"pageId":   mcp.String("页面 id（用 page_find 拿）"),
			"lang":     mcp.String("发布哪种语言（可选；多语言站点用，如 zh-CN）"),
			"allLangs": mcp.Boolean("发布站点启用的全部语言（可选；置真时 lang 被忽略）"),
		}, "pageId"),
		nil,
		func(ctx context.Context, args pagePublishArgs) (mcp.Result, error) {
			req := &pagedto.PublishReq{
				ID:       strings.TrimSpace(args.ID),
				Lang:     strings.TrimSpace(args.Lang),
				AllLangs: args.AllLangs,
			}
			res, err := w.Publish(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: publishResultText(res, "已发布")}, nil
		})
}

// publishResultText 报出激活后的真实网址 —— 用户下一步就是要拿它去访问。
func publishResultText(res *pagedto.PublishResp, action string) string {
	if res == nil {
		return action + "。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "页面 %s %s（状态 %s）", res.PageID, action, emptyAsDash(res.Status))
	if p := strings.TrimSpace(res.DraftPath); p != "" {
		fmt.Fprintf(&b, "，当前路径 %s", p)
	}
	if h := strings.TrimSpace(res.ActiveHash); h != "" {
		fmt.Fprintf(&b, "，线上产物 %s", shortHash(h))
	}
	b.WriteString("。")
	return b.String()
}

// shortHash 产物哈希太长，回执里只留前 8 位（够人对账，不占版面）。
func shortHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}

type pageURLUpdateArgs struct {
	PageID       string `json:"pageId"`
	NewPath      string `json:"newPath"`
	WithRedirect bool   `json:"withRedirect"`
	Lang         string `json:"lang"`
}

func pageURLUpdate(w pageWriter) mcp.Tool {
	return mcp.NewWrite("page_url_update", "改页面线上网址",
		"把一个已上线页面的网址换成新路径（同时会重新构建并激活）。\n"+
			"**老网址会失效** —— 除非 withRedirect=true，那样服务端会自动加一条"+
			"「老地址 → 新地址」的跳转，保住已有的收藏与外部链接。"+
			"**改动公开网址时基本都该置真**，除非你确定那个地址从没对外用过。\n"+
			"多语言站点可以用 lang 只改某一种语言的路径；不传则改默认语言。",
		permission.PagePublish,
		mcp.Object("改页面网址参数", map[string]mcp.Schema{
			"pageId":       mcp.String("页面 id（用 page_find 拿）"),
			"newPath":      mcp.String("新的线上路径，以 / 开头，如 /about-us"),
			"withRedirect": mcp.Boolean("是否自动加「老地址 → 新地址」跳转（可选，默认 false）"),
			"lang":         mcp.String("只改哪种语言的路径（可选，多语言站点用）"),
		}, "pageId", "newPath"),
		nil,
		func(ctx context.Context, args pageURLUpdateArgs) (mcp.Result, error) {
			req := &pagedto.UpdateURLReq{
				ID:           strings.TrimSpace(args.PageID),
				NewPath:      ensureLeadingSlash(args.NewPath),
				WithRedirect: args.WithRedirect,
				Lang:         strings.TrimSpace(args.Lang),
			}
			res, err := w.UpdateURL(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			text := publishResultText(res, "网址已改")
			if !args.WithRedirect {
				text += "\n**没有加跳转**，老地址现在会 404 —— 如果它以前对外用过，现在补一条跳转。" +
					"（去后台的跳转规则里加，或用 page_url_update 带上 withRedirect 再改一次。）"
			}
			return mcp.Result{Text: text}, nil
		})
}

type pageDeleteArgs struct {
	ID string `json:"pageId"`
}

func pageDelete(w pageWriter) mcp.Tool {
	return mcp.NewWrite("page_delete", "删除页面",
		"删掉一个页面（草稿与线上产物一起去掉）。**不可撤销**。\n"+
			"删之前先 `page_find` 确认删的是哪一张、路径是什么 —— "+
			"页面标题常常很像（比如「关于我们」与「关于我们（旧）」）。\n"+
			"**首页 / 404 / 搜索结果这类功能页删掉会让站点出问题**"+
			"（前台找不到对应页面时会回落到默认模板）。这类页面只该改内容，不该删。\n"+
			"如果只是不想让它被访问到，通常有更合适的做法（改路径 + 跳转、或把内容替换掉），"+
			"先问清楚用户的意图。",
		permission.PageDelete,
		mcp.Object("删除页面参数", map[string]mcp.Schema{
			"pageId": mcp.String("页面 id（用 page_find 拿）"),
		}, "pageId"),
		nil,
		func(ctx context.Context, args pageDeleteArgs) (mcp.Result, error) {
			id := strings.TrimSpace(args.ID)
			if id == "" {
				return mcp.Result{}, errors.New("pageId 不能为空")
			}
			if err := w.Delete(ctx, &pagedto.DeleteReq{ID: id}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"页面 %s 已删除（草稿与线上产物一起）。前台如果还有指向它的链接，现在会 404。", id)}, nil
		})
}
