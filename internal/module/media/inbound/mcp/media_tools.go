package mediamcp

// media_tools.go — 媒体模块暴露给模型的工具（附件：一个搜 + 一个读 + 两个写）。
//
// 与商品/内容同构。**媒体是「元数据能改、文件本身改不了」的一类**：
// 它的写操作只有两件事 —— 改文件名/分类/alt 那类字段，或者删掉。
// 换图（Replace）与上传（Upload）都要求 multipart 文件，模型给不出，
// 所以它们不在工具集里；这一点写进下面的说明，免得模型对着「换个图」的请求硬凑一个调用。
//
// **依赖断言成收窄的端口**：MediaService 同时握着 ReconcileStorage / GenerateVariants /
// ReplayVariantBackfill，工具层握着它们时「顺手重算一遍变体」会从
// 「显式加一个工具」退化成「随手就能做」。

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"go_wp/internal/mcp"
	mediadto "go_wp/internal/module/media/dto"
	"go_wp/internal/permission"
)

// MediaReader 读附件（单条 + 列表）。
type MediaReader interface {
	Detail(ctx context.Context, req *mediadto.DetailReq) (*mediadto.AttachmentResp, error)
	List(ctx context.Context, req *mediadto.ListReq) (*mediadto.ListResp, error)
}

// MediaWriter 附件上的两个写操作。
type MediaWriter interface {
	UpdateAttachment(ctx context.Context, req *mediadto.AttachmentUpdateReq) error
	Delete(ctx context.Context, req *mediadto.DeleteReq) error
}

// Tools 返回媒体模块的工具集。
//
// **全部工具都不收 projectId**：`sys_attachment` 没有 project_id 列（媒体库站点级共享），
// 这个参数既不参与查询也不参与过滤。service 里有一段注释专门记着这件事 —— 它曾经
// 被当必填，唯一效果是让没带参数的调用方拿到一个 400（看起来像「附件不存在」）。
// 工具层重复这个错误会更糟：模型会把它当成「需要向用户索要的信息」而停下来问。
func Tools(r MediaReader, w MediaWriter, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if r == nil || w == nil {
		return nil, errors.New("mediamcp: 媒体读写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{
		mediaFind(r),
		mediaGet(r),
		mediaUpdate(w, store),
		mediaDelete(w, store),
	}, nil
}

// mediaFindArgs media_find 的入参。
type mediaFindArgs struct {
	Search   string  `json:"search"`
	FileType string  `json:"fileType"`
	Category *uint64 `json:"categoryId"`
}

func mediaFind(r MediaReader) mcp.Tool {
	return mcp.New("media_find", "搜索媒体",
		"按文件名搜索媒体附件，返回 id、文件名、类型与地址。**用户只会说文件大概叫什么**，"+
			"所以要先用它把名字换成 id，再去调 media_get / media_update / media_delete。",
		permission.MediaList,
		mcp.Object("搜索参数", map[string]mcp.Schema{
			"search":     mcp.String("文件名的一部分（可为空，空则列出最近的媒体）"),
			"fileType":   mcp.Enum("限定文件类型（可选）", "image", "video", "audio", "document", "other"),
			"categoryId": mcp.Integer("限定分类 id（可选）"),
		}),
		func(ctx context.Context, args mediaFindArgs) (mcp.Result, error) {
			res, err := r.List(ctx, &mediadto.ListReq{
				Search: args.Search, FileType: args.FileType, CategoryID: args.Category,
				Page: 1, Limit: mediaFindLimit,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			if res == nil || len(res.List) == 0 {
				return mcp.Result{Text: "没有匹配的媒体（关键词：" + strings.TrimSpace(args.Search) + "）。" +
					"换个词试试，或者先问用户文件名。"}, nil
			}
			var b strings.Builder
			b.WriteString("匹配到 " + strconv.Itoa(len(res.List)) + " 个（共 " +
				strconv.FormatInt(res.Total, 10) + " 个）：\n")
			for i := range res.List {
				a := &res.List[i]
				b.WriteString("- " + a.FileName + "（id=" + strconv.FormatUint(a.ID, 10) +
					"，类型=" + a.FileType + "，" + mediaMetaBrief(a.ExtraInfo) + "）\n")
			}
			return mcp.Result{Text: b.String()}, nil
		},
	)
}

// mediaFindLimit 搜索返回的条数上限（与商品/内容侧同口径）。
const mediaFindLimit = 20

// mediaGetArgs media_get 的入参。
type mediaGetArgs struct {
	ID uint64 `json:"id"`
}

func mediaGet(r MediaReader) mcp.Tool {
	return mcp.New("media_get", "读取媒体",
		"按 id 读出一个媒体附件的字段，含 alt / 标题 / 描述（SEO 与无障碍用的那几项）。"+
			"**id 要先经 media_find 拿到**；改之前先调它核对。",
		permission.MediaDetail,
		mcp.Object("读取参数", map[string]mcp.Schema{
			"id": mcp.Integer("媒体 id（数字，从 media_find 的返回值里取）"),
		}, "id"),
		func(ctx context.Context, args mediaGetArgs) (mcp.Result, error) {
			res, err := r.Detail(ctx, &mediadto.DetailReq{ID: args.ID})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: describeMedia(res)}, nil
		},
	)
}

// mediaUpdateArgs media_update 的业务入参。
//
// 指针类型与 service 同形（nil = 不改）。**分类用 *uint64 而不是 string**：
// service 用 0 表示「移入未分类」，所以这个字段的三态是「不传 / 0 / 某个 id」，
// 折成字符串（"" / "0" / "12"）会把前两者混成一个。
type mediaUpdateArgs struct {
	ID          uint64  `json:"id"`
	FileName    *string `json:"fileName"`
	CategoryID  *uint64 `json:"categoryId"`
	Alt         *string `json:"alt"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

func mediaUpdate(w MediaWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("media_update", "更新媒体信息",
		"更新媒体的元数据：文件名、所属分类、alt 文本、标题、描述。只传要改的字段，"+
			"没传的保持原值。**改不了文件本身**（换图要在后台上传新文件）。"+
			"alt 是给读屏软件与搜图用的，为空时图片对无障碍用户等于不存在。",
		permission.MediaUpdate,
		mcp.Object("更新参数", map[string]mcp.Schema{
			"id":          mcp.Integer("媒体 id（数字）"),
			"fileName":    mcp.String("新文件名（不传 = 不改）"),
			"categoryId":  mcp.Integer("新分类 id；**传 0 = 移入未分类**（不传 = 不改分类）"),
			"alt":         mcp.String("alt 文本（图片替代文字；不传 = 不改）"),
			"title":       mcp.String("标题（不传 = 不改）"),
			"description": mcp.String("描述（不传 = 不改）"),
		}, "id"),
		store,
		func(ctx context.Context, args mediaUpdateArgs) (mcp.Result, error) {
			err := w.UpdateAttachment(ctx, &mediadto.AttachmentUpdateReq{
				ID:          args.ID,
				FileName:    args.FileName,
				CategoryID:  args.CategoryID,
				Alt:         args.Alt,
				Title:       args.Title,
				Description: args.Description,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已更新媒体 " + strconv.FormatUint(args.ID, 10) + " 的信息"}, nil
		},
	)
}

// mediaDeleteArgs media_delete 的入参。
type mediaDeleteArgs struct {
	ID uint64 `json:"id"`
}

func mediaDelete(w MediaWriter, store mcp.IdempotencyStore) mcp.Tool {
	return mcp.NewWrite("media_delete", "删除媒体",
		"按 id 删除一个媒体附件。**这是不可逆操作**：文件会一起没了，而引用它的页面会出现死图。"+
			"删之前先用 media_get 读出文件名说给用户听，确认后再调用本工具。",
		permission.MediaDelete,
		mcp.Object("删除参数", map[string]mcp.Schema{
			"id": mcp.Integer("要删除的媒体 id"),
		}, "id"),
		store,
		func(ctx context.Context, args mediaDeleteArgs) (mcp.Result, error) {
			if err := w.Delete(ctx, &mediadto.DeleteReq{ID: args.ID}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "已删除媒体 " + strconv.FormatUint(args.ID, 10)}, nil
		},
	)
}

// describeMedia 把附件摊成文本。
//
// **必须把 alt / 标题 / 描述显式列出来**：它们藏在 extra_info 这一列 JSON 里，
// 不展平的话模型在 media_get 的输出里看不到它们，于是会「确认现状」之后
// 再写一遍同样的值（或者更糟：以为要清空）。
func describeMedia(res *mediadto.AttachmentResp) string {
	if res == nil {
		return "（没有读到媒体）"
	}
	extra := parseExtra(res.ExtraInfo)
	var b strings.Builder
	b.WriteString("媒体（id=" + strconv.FormatUint(res.ID, 10) + "）\n")
	writeLine(&b, "文件名", res.FileName)
	writeLine(&b, "类型", res.FileType)
	writeLine(&b, "MIME", res.MimeType)
	writeLine(&b, "大小", strconv.FormatInt(res.FileSize, 10)+" 字节")
	writeLine(&b, "地址", res.URL)
	writeLine(&b, "alt", extra["alt"])
	writeLine(&b, "标题", extra["title"])
	writeLine(&b, "描述", extra["description"])
	return b.String()
}

// mediaMetaBrief 列表项的一句摘要（只带 alt，免得列表太长）。
func mediaMetaBrief(extraInfo string) string {
	alt := parseExtra(extraInfo)["alt"]
	if strings.TrimSpace(alt) == "" {
		return "无 alt"
	}
	return "alt=" + alt
}

// parseExtra 解析 extra_info 里的那三个键。
//
// 坏 JSON 回空 map 而不是报错：这个字段是**读出来给人看的**，
// 解析失败不该让 media_get 整个失败（那时用户连文件名都看不到）。
func parseExtra(raw string) map[string]string {
	out := map[string]string{"alt": "", "title": "", "description": ""}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	for _, k := range []string{"alt", "title", "description"} {
		if v, ok := m[k].(string); ok {
			out[k] = v
		}
	}
	return out
}

// writeLine 写一行「字段: 值」，空值也写（标出「本字段为空」）。
func writeLine(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		b.WriteString("- " + label + ": （空）\n")
		return
	}
	b.WriteString("- " + label + ": " + value + "\n")
}
