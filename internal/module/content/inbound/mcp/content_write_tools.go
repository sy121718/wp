package contentmcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
)

type ContentWriter interface {
	Create(ctx context.Context, req *contentdto.CreateReq) (*contentdto.ContentResp, error)
	Update(ctx context.Context, req *contentdto.UpdateReq) (*contentdto.ContentResp, error)
	Delete(ctx context.Context, req *contentdto.DeleteReq) error
}

type ContentReader interface {
	Get(ctx context.Context, req *contentdto.GetReq) (*contentdto.ContentResp, error)
	List(ctx context.Context, req *contentdto.ListReq) ([]*contentdto.ContentResp, error)
}

func Tools(w ContentWriter, r ContentReader, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if w == nil || r == nil {
		return nil, errors.New("contentmcp: 内容读写服务依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		contentFind(r),
		contentGet(r),
		contentCreate(w, store),
		contentUpdate(w, r, store),
		contentDelete(w, store),
	}, nil
}

type contentFindArgs struct {
	EntityType string `json:"entityType"`
	Keyword    string `json:"keyword"`
}

func contentFind(r ContentReader) mcp.Tool {
	return mcp.New("content_find", "搜索内容",
		"按关键词搜索内容（匹配标题与 slug），返回 id 与标题。**用户只会说文章名**，"+
			"所以要先用它把名字换成 id，再去调 content_get / content_update / content_delete。"+
			"不传关键词则列出该类型最近的内容。",
		contentenums.ContentList,
		mcp.Object("搜索参数", map[string]mcp.Schema{
			"entityType": mcp.Enum("内容类型（留空则不限类型）", contentcontract.ContentTypes()...),
			"keyword":    mcp.String("标题的一部分或 slug（可为空，空则列出最近的内容）"),
		}),
		func(ctx context.Context, args contentFindArgs) (mcp.Result, error) {
			list, err := r.List(ctx, &contentdto.ListReq{
				EntityType: args.EntityType, Keyword: args.Keyword, Limit: contentFindLimit,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if len(list) == 0 {
				return mcp.Result{Text: "没有匹配的内容（关键词：" + strings.TrimSpace(args.Keyword) + "）。" +
					"换个词试试，或者先问用户标题的准确写法。"}, nil
			}
			var b strings.Builder
			b.WriteString("匹配到 " + strconv.Itoa(len(list)) + " 条：\n")
			for _, item := range list {
				title, _ := item.Data["title"].(string)
				if strings.TrimSpace(title) == "" {
					title = "（标题为空）"
				}
				b.WriteString("- " + title + "（id=" + item.ID + "，slug=" + item.Slug + "）\n")
			}
			if len(list) >= contentFindLimit {
				b.WriteString("（只列了前 " + strconv.Itoa(contentFindLimit) + " 条；换个更具体的关键词能缩小范围。）\n")
			}
			return mcp.Result{Text: b.String()}, nil
		},
	)
}

const contentFindLimit = 20

type contentGetArgs struct {
	ID string `json:"id"`
}

func contentGet(r ContentReader) mcp.Tool {
	return mcp.New("content_get", "读取内容",
		"按 id 读出一条内容的全部字段（用于看清现状，或核对改动结果）。"+
			"**id 要先经 content_find 拿到**（用户通常只说标题）。"+
			"注意：返回值可能被剪枝（很长的正文只保留开头），所以它**不是** content_update 的输入 ——"+
			"content_update 只传要改的字段就够了。",
		contentenums.ContentGet,
		mcp.Object("读取参数", map[string]mcp.Schema{
			"id": mcp.String("内容 id"),
		}, "id"),
		func(ctx context.Context, args contentGetArgs) (mcp.Result, error) {
			res, err := r.Get(ctx, &contentdto.GetReq{ID: args.ID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: describeContent(res)}, nil
		},
	)
}

func describeContent(res *contentdto.ContentResp) string {
	out := "内容 " + res.EntityType + " / " + res.Slug + "（id=" + res.ID +
		"，revision=" + strconv.FormatInt(res.Revision, 10) + "）\n"

	out += "- 更新时间: " + emptyAsDash(res.UpdatedAt) + "\n"
	for _, f := range contentcontract.FieldWhitelist(res.EntityType) {
		v, ok := res.Data[f]
		if !ok {
			out += "- " + f + ": （此字段不存在）\n"
			continue
		}
		out += "- " + f + ": " + stringify(v) + "\n"
	}
	return out
}

func emptyAsDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "（空）"
	}
	return v
}

func stringify(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

type contentCreateArgs struct {
	EntityType string         `json:"entityType"`
	Slug       string         `json:"slug"`
	Data       map[string]any `json:"data"`
}

func contentCreate(w ContentWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("content_create", "新建内容",
		"新建一条内容实体（如文章）。slug 在同类型内必须唯一；data 只接受该类型的白名单字段，"+
			"字段名见参数说明。",
		contentenums.ContentCreate,
		mcp.Object("新建内容的参数", map[string]mcp.Schema{
			"entityType": mcp.Enum("内容类型", contentcontract.ContentTypes()...),
			"slug":       mcp.String("同类型内唯一的标识（字母数字与短横线）"),
			"data": contentDataSchema("要写入的字段与取值。**只有该类型白名单内的字段会被接受**，" +
				"传了白名单外的字段会整次失败（不是被忽略）。"),
		}, "entityType", "slug", "data"),
		store,
		func(ctx context.Context, args contentCreateArgs) (mcp.Result, error) {
			res, err := w.Create(ctx, &contentdto.CreateReq{
				EntityType: args.EntityType, Slug: args.Slug, Data: args.Data,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已新建内容 " + res.EntityType + " / " + res.Slug + "（id=" + res.ID + "）"}, nil
		},
	)
}

type contentUpdateArgs struct {
	ID   string         `json:"id"`
	Data map[string]any `json:"data"`
}

func contentUpdate(w ContentWriter, r ContentReader, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("content_update", "更新内容",
		"按 id 更新一条内容的**部分字段**（revision 递增）。data 里只写要改的字段即可，"+
			"没写的字段保持原值 —— 合并由服务端做（先读出当前内容、再全量写回），"+
			"所以**不需要**把整份内容传进来。想把某个字段清空就显式传空串。",
		contentenums.ContentUpdate,
		mcp.Object("更新参数", map[string]mcp.Schema{
			"id": mcp.String("内容 id（新建时的返回值里带）"),
			"data": contentDataSchema("要修改的字段。**只传要改的那几个**，" +
				"没写的字段不会被清空；要清空某字段就显式传空串。"),
		}, "id", "data"),
		store,
		func(ctx context.Context, args contentUpdateArgs) (mcp.Result, error) {

			cur, err := r.Get(ctx, &contentdto.GetReq{ID: args.ID})
			if err != nil {
				return mcp.Result{}, err
			}
			merged := make(map[string]any, len(cur.Data)+len(args.Data))
			for k, v := range cur.Data {
				merged[k] = v
			}
			for k, v := range args.Data {
				merged[k] = v
			}
			res, err := w.Update(ctx, &contentdto.UpdateReq{ID: args.ID, Data: merged})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已更新内容 " + res.EntityType + " / " + res.Slug +
				"（revision=" + strconv.FormatInt(res.Revision, 10) + "）"}, nil
		},
	)
}

type contentDeleteArgs struct {
	ID string `json:"id"`
}

func contentDelete(w ContentWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("content_delete", "删除内容",
		"按 id 删除一条内容。**这是不可逆操作**：删之前先把 id 与标题说给用户听，"+
			"确认后再调用本工具。",
		contentenums.ContentDelete,
		mcp.Object("删除参数", map[string]mcp.Schema{
			"id": mcp.String("要删除的内容 id"),
		}, "id"),
		store,
		func(ctx context.Context, args contentDeleteArgs) (mcp.Result, error) {
			if err := w.Delete(ctx, &contentdto.DeleteReq{ID: args.ID}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已删除内容 " + args.ID}, nil
		},
	)
}

func contentDataSchema(desc string) mcp.Schema {
	props := map[string]mcp.Schema{}
	for _, f := range contentcontract.AllFields() {
		props[f] = mcp.String("字段 " + f)
	}
	return mcp.Object(desc, props)
}
