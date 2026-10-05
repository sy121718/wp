package contentmcp

// content_write_tools.go — 内容（文章）模块暴露给模型的**写**工具。
//
// 与只读工具的区别全在 mcp.NewWrite 里（显式确认位 + 幂等键 + 顺序保证，
// 见 internal/mcp/write.go 的说明）。这里只负责三件事：把业务字段映射成 schema、
// 把权限点对准、把 service 的错误翻成给模型看的话。
//
// 依赖收窄到内容写服务的一个端口（三个方法），而不是整个 ContentService：
// 工具层握着 Publish / RegisterEntityTypes 时，「AI 顺手发一版」会从
// 「需要显式加一个工具」退化成「随手就能做」—— 能力边界该由接口形状决定。

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/permission"
)

// ContentWriter 写工具需要的内容模块能力（三个方法，各自一个工具）。
type ContentWriter interface {
	Create(ctx context.Context, req *contentdto.CreateReq) (*contentdto.ContentResp, error)
	Update(ctx context.Context, req *contentdto.UpdateReq) (*contentdto.ContentResp, error)
	Delete(ctx context.Context, req *contentdto.DeleteReq) error
}

// ContentReader 读内容（单条 + 列表）。
type ContentReader interface {
	Get(ctx context.Context, req *contentdto.GetReq) (*contentdto.ContentResp, error)
	List(ctx context.Context, req *contentdto.ListReq) ([]*contentdto.ContentResp, error)
}

// Tools 返回内容模块的全部工具。
//
// **`content_get` 与三个写工具是一批的**，不能只上写工具：update 是整份替换，
// 没有「先读出来」的入口时模型只能凭记忆拼字段集，漏一个就抹掉一个字段 ——
// 而那次调用会成功返回、看起来完成得很好。把读入口与写入口放在一批里，
// 是让这条正确用法**在工具集里本来就成立**，而不是写在文档里等人遵守。
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

// contentFindArgs content_find 的入参。
type contentFindArgs struct {
	EntityType string `json:"entityType"`
	Keyword    string `json:"keyword"`
}

// contentFind 按关键词找内容。
//
// **这一环是必需的**：用户说的是「把那篇讲戒烟的文章标题改一下」，他不会念 uuid，
// 而 uuid 是其余工具的唯一入口。没有它，模型只能向用户索要 uuid（用户给不出）
// 或者猜一个（猜出来的 id 不存在，报错会归到「内容不存在」，看起来像数据问题）。
func contentFind(r ContentReader) mcp.Tool {
	return mcp.New("content_find", "搜索内容",
		"按关键词搜索内容（匹配标题与 slug），返回 id 与标题。**用户只会说文章名**，"+
			"所以要先用它把名字换成 id，再去调 content_get / content_update / content_delete。"+
			"不传关键词则列出该类型最近的内容。",
		permission.ContentList,
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

// contentFindLimit 搜索返回的条数上限（与商品侧同口径）。
const contentFindLimit = 20

// contentGetArgs content_get 的入参。
type contentGetArgs struct {
	ID string `json:"id"`
}

func contentGet(r ContentReader) mcp.Tool {
	return mcp.New("content_get", "读取内容",
		"按 id 读出一条内容的全部字段（用于看清现状，或核对改动结果）。"+
			"**id 要先经 content_find 拿到**（用户通常只说标题）。"+
			"注意：返回值可能被剪枝（很长的正文只保留开头），所以它**不是** content_update 的输入 ——"+
			"content_update 只传要改的字段就够了。",
		permission.ContentGet,
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

// describeContent 把一条内容摊成便于模型读的文本。
//
// 字段逐个列出而不是丢一个 JSON：模型要在下一轮把这份内容改完写回，
// 而它读 JSON 时容易把「空串」与「字段不存在」当成一回事 ——
// 这两者在整份替换下后果不同（前者写回空串、后者抹掉字段）。
func describeContent(res *contentdto.ContentResp) string {
	out := "内容 " + res.EntityType + " / " + res.Slug + "（id=" + res.ID +
		"，revision=" + strconv.FormatInt(res.Revision, 10) + "）\n"
	// 更新时间要带上：用户会问「这篇什么时候改过」，而 content_update 拿到的
	// revision 只有变大才知道改过 —— 时间是他真正用来判断的那条线索。
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

// emptyAsDash 空值显示成「（空）」而不是留白（与商品侧的 writeLine 同一口径：
// 模型要靠「存在但为空」与「不存在」区分不同情况）。
func emptyAsDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "（空）"
	}
	return v
}

// stringify 把字段值摊成一行文本（字符串原样，其余走 JSON）。
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

// contentCreateArgs content_create 的业务入参。
//
// Data 用 map[string]any 而不是结构体：字段白名单是**按类型分**的（article 有 7 个），
// 而工具声明的 schema 是静态的 —— 让它成为结构体等于要么把字段写死，
// 要么让新增字段变成一次工具改动。逐字段的严格判定在 service（validateData）。
type contentCreateArgs struct {
	EntityType string         `json:"entityType"`
	Slug       string         `json:"slug"`
	Data       map[string]any `json:"data"`
}

func contentCreate(w ContentWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("content_create", "新建内容",
		"新建一条内容实体（如文章）。slug 在同类型内必须唯一；data 只接受该类型的白名单字段，"+
			"字段名见参数说明。",
		permission.ContentCreate,
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

// contentUpdateArgs content_update 的业务入参。
type contentUpdateArgs struct {
	ID   string         `json:"id"`
	Data map[string]any `json:"data"`
}

func contentUpdate(w ContentWriter, r ContentReader, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("content_update", "更新内容",
		"按 id 更新一条内容的**部分字段**（revision 递增）。data 里只写要改的字段即可，"+
			"没写的字段保持原值 —— 合并由服务端做（先读出当前内容、再全量写回），"+
			"所以**不需要**把整份内容传进来。想把某个字段清空就显式传空串。",
		permission.ContentUpdate,
		mcp.Object("更新参数", map[string]mcp.Schema{
			"id": mcp.String("内容 id（新建时的返回值里带）"),
			"data": contentDataSchema("要修改的字段。**只传要改的那几个**，" +
				"没写的字段不会被清空；要清空某字段就显式传空串。"),
		}, "id", "data"),
		store,
		func(ctx context.Context, args contentUpdateArgs) (mcp.Result, error) {
			// **读-改-写在服务端做**，不交给模型：service 的 Update 是整份替换，
			// 而模型的意图是「改这个字段」。让模型自己取全量再写回的结构有两个洞：
			//   · 工具结果会被剪枝（长正文只留前 N 字），它拿不到完整字段集；
			//   · 就算拿到了，一次「我以为我传全了」也会静默抹掉别的字段。
			// 这两条都是真调一次模型才会暴露的（实测：它自己发现 body 被截断，
			// 于是拒绝写入并要用户确认 —— 判断是对的，但这个死结该由工具解开）。
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

// contentDeleteArgs content_delete 的业务入参。
type contentDeleteArgs struct {
	ID string `json:"id"`
}

func contentDelete(w ContentWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("content_delete", "删除内容",
		"按 id 删除一条内容。**这是不可逆操作**：删之前先把 id 与标题说给用户听，"+
			"确认后再调用本工具。",
		permission.ContentDelete,
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

// contentDataSchema 生成 data 字段的 schema。
//
// 属性名从**契约的字段白名单并集**来（contentcontract.AllFields），不手抄：
// 手抄的清单在「契约里加了字段」时不会跟着变，而失败形态是模型的参数被工具层
// 拒掉、或者（更糟的写法下）被静默丢弃。类型一律 string —— 白名单只给名字不给类型，
// 而这几个字段在库里全是字符串（媒体字段也是地址串）。
func contentDataSchema(desc string) mcp.Schema {
	props := map[string]mcp.Schema{}
	for _, f := range contentcontract.AllFields() {
		props[f] = mcp.String("字段 " + f)
	}
	return mcp.Object(desc, props)
}
