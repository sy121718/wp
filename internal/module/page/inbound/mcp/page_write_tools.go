package pagemcp

// page_write_tools.go — 页面的骨架与上线动作（建页 / 发布 / 改网址 / 删页）。
//
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
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/permission"
)

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
