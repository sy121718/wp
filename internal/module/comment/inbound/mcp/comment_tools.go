package commentmcp

// comment_tools.go — 「按线索找评论」的只读工具。
//
// 为什么补这一个：评论模块此前对外只有两件事 —— 公开列表（ListApproved，必须给
// 具体实体：EntityType + EntityID）与后台审核页。运营真正会问的是
// 「有哪些评论还在等审核」「提到退货的评论有多少」—— 前者的答案横跨所有实体
// （不该逼用户先说出实体 id），后者是**正文关键词**。底层一直是齐的：
// service.AdminList 支持工程 + 状态 + 实体类型 + 正文关键词 + 分页。
//
// 只读面与写面分开成两个文件：本文件只有 QueryReader（一个方法），
// 审核在 comment_review_tools.go 里另走 WriteTools —— 两者的依赖、权限点、
// 变更风险都不是一回事，混在一个装配函数里会让「只想加个查询」顺手把审核也带进来。
//
//（本文初版写的「审核是人的判断，模型代替不了这个位置」已被推翻：判断是谁做出的
// 取决于谁授权的，而 ReviewerID 取自登录身份、不由模型提供。详见 review 文件头。）

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/internal/permission"
)

const (
	commentFindDefaultPageSize = 10
	commentFindMaxPageSize     = 50
	// commentBodyInlineMax 正文在输出里的截断长度（按字符）。
	//
	// 评论正文可能有几千字，而一次筛选会返回十条 —— 不截的话工具结果里全是正文，
	// 挤掉后面几批查询的位置。截断处标明还有多少字，模型据此知道要不要细看某条。
	commentBodyInlineMax = 120
)

// QueryTools 返回评论模块的只读查询工具集。
func QueryTools(query commentcontract.QueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("commentmcp: 评论查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{commentFind(query)}, nil
}

// commentFindArgs comment_find 的入参。
type commentFindArgs struct {
	ProjectID  string `json:"projectId"`
	Status     string `json:"status"`
	EntityType string `json:"entityType"`
	Keyword    string `json:"keyword"`
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
}

func commentFind(query commentcontract.QueryReader) mcp.Tool {
	return mcp.New("comment_find", "按线索查评论",
		"按线索查评论，用于回答「有哪些评论还在等审核」「提到退货的评论有几条」「商品下面最近评论了什么」。\n"+
			"keyword 匹配**评论正文**（模糊，不区分大小写），所以用户随口提的一个词就能用来筛。\n"+
			"status / entityType 都可以不传：不传 status 就是全部状态（含待审、垃圾），\n"+
			"不传 entityType 就是所有可评论的实体类型。\n"+
			"本工具只读；要改状态（通过 / 驳回）用 comment_review。",
		permission.CommentList,
		mcp.Object("按线索查评论参数", map[string]mcp.Schema{
			"projectId":  mcp.String("站点工程 id（uuid，必填：评论按工程隔离）"),
			"status":     mcp.Enum("只看某个审核状态（可选；不传则全部状态）", commentenums.Statuses()...),
			"entityType": mcp.String("只看某类实体下的评论（可选，取值见站长后台的评论筛选下拉，如 article / product）"),
			"keyword":    mcp.String("评论正文关键词，任一命中即算（可选）"),
			"page":       mcp.Integer("页码，从 1 开始（可选，默认 1）"),
			"pageSize":   mcp.Integer("每页条数，默认 10，上限 50"),
		}, "projectId"),
		func(ctx context.Context, args commentFindArgs) (mcp.Result, error) {
			res, err := query.AdminList(ctx, &commentdto.AdminListReq{
				ProjectID:  args.ProjectID,
				Status:     args.Status,
				EntityType: args.EntityType,
				Keyword:    args.Keyword,
				Page:       args.Page,
				PageSize:   clampCommentPageSize(args.PageSize),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: findCommentsText(res), Data: res}, nil
		})
}

// clampCommentPageSize 把每页条数夹到合法区间。
//
// ≤0 给默认值而不是报错：模型不传这个参数时 JSON 里就没有它，反序列化得到 0，
// 那不是「用户要 0 条」。
func clampCommentPageSize(n int) int {
	if n <= 0 {
		return commentFindDefaultPageSize
	}
	if n > commentFindMaxPageSize {
		return commentFindMaxPageSize
	}
	return n
}

// findCommentsText 把审核队列写成模型能直接引用的几句。
//
// 每行都给**评论 id**：它是运营在后台找到这条评论的唯一钥匙，只给正文时
// 用户得自己在列表里翻。
func findCommentsText(res *commentdto.AdminListResp) string {
	if res == nil {
		return "没有查到评论。"
	}
	var b strings.Builder
	if len(res.Items) == 0 {
		// 空结果要区分「这个筛选下确实没有」与「这个工程一条评论都没有」——
		// 两者对用户的意义不同（前者让他换条件，后者说明还没人评论过）。
		if res.Total == 0 {
			b.WriteString("没有符合条件的评论。换个关键词或去掉筛选（不传 status / entityType）再看一次。")
		} else {
			fmt.Fprintf(&b, "这一页没有评论（共 %d 条符合条件的记录，可能页码超出范围）。", res.Total)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "共 %d 条符合条件的评论，本页 %d 条：\n", res.Total, len(res.Items))
	for i, it := range res.Items {
		fmt.Fprintf(&b, "%d. #%d · %s · %s%s · 提交 %s\n   %s\n",
			i+1, it.ID, commentStatusText(it), commentEntityText(it),
			replyMark(it), commentTimeText(it), inlineCommentBody(it.Body))
	}
	return strings.TrimRight(b.String(), "\n")
}

// commentStatusText 取状态的中文说法。
//
// 优先用服务端按请求语言取好的 StatusLabel；工具调用没有请求语言，所以它通常是空的 ——
// 这时回落到 enums 的中文兜底。**不自己写一张状态表**：状态枚举的中文说法只有一处真源
// （commentenums），各写一份时新增一种状态会出现「页面上叫一个名字、AI 嘴里叫另一个」。
func commentStatusText(it commentdto.AdminItem) string {
	if strings.TrimSpace(it.StatusLabel) != "" {
		return it.StatusLabel
	}
	if pair := commentenums.StatusLabel(it.Status); strings.TrimSpace(pair.Fallback) != "" {
		return pair.Fallback
	}
	return it.Status
}

// commentEntityText 写出这条评论挂在哪个实体上。
//
// 只有类型与 id，没有实体名字：评论模块**不认识**这些实体的细节（契约里写明了），
// 要名字就得再调一次对应模块的工具。模型据此知道下一步该调谁。
func commentEntityText(it commentdto.AdminItem) string {
	if it.EntityType == "" && it.EntityID == "" {
		return "（未知实体）"
	}
	if it.EntityID == "" {
		return it.EntityType
	}
	return it.EntityType + " " + it.EntityID
}

func replyMark(it commentdto.AdminItem) string {
	if it.IsReply {
		return "（回复）"
	}
	return ""
}

func commentTimeText(it commentdto.AdminItem) string {
	if it.CreateTime.IsZero() {
		return "-"
	}
	return it.CreateTime.Time().Format("2006-01-02 15:04")
}

// inlineCommentBody 把正文压成一行并截断。
//
// 换行压成空格：表格 / 列表里换行会让一行变成多行，模型引用时容易只截到半句。
func inlineCommentBody(body string) string {
	flat := strings.Join(strings.Fields(body), " ")
	if flat == "" {
		return "（空正文）"
	}
	runes := []rune(flat)
	if len(runes) <= commentBodyInlineMax {
		return flat
	}
	// 按字符截断（中文一个字三字节），并说明还剩多少字 —— 只截不说会让模型
	// 以为自己看到的是全文。
	return string(runes[:commentBodyInlineMax]) + fmt.Sprintf("…（还有 %d 字）", len(runes)-commentBodyInlineMax)
}
