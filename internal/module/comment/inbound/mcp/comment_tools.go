package commentmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	"go_wp/internal/module/comment/contract"
	"go_wp/internal/module/comment/dto"
	"go_wp/internal/module/comment/enums"
	"go_wp/internal/permission"
)

func WriteTools(w commentcontract.ReviewWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("commentmcp: 评论审核依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{commentReview(w)}, nil
}

type commentReviewArgs struct {
	ProjectID string  `json:"projectId"`
	IDs       []int64 `json:"ids"`
	Status    string  `json:"status"`
}

func commentReview(w commentcontract.ReviewWriter) mcp.Tool {
	return mcp.NewWrite("comment_review", "审核评论（通过 / 驳回）",
		"把一批评论置为「已通过」或「已驳回」。ids 是评论 id 列表，先用 comment_find 拿到。\n"+
			"status 只接受 approved（通过，之后会出现在站点公开列表里）或 rejected（驳回，保留记录但不公开）。\n"+
			"一次最多 "+fmt.Sprint(commentenums.MaxReviewIDs)+" 条。\n"+
			"**执行前必须先把要处理的评论念给用户确认**（哪几条、正文摘要、改成什么状态）—— "+
			"通过会让评论立刻公开可见，用户往往并不是要放行全部待审评论。\n"+
			"执行后如实报出影响条数：如果实际条数与请求条数不一致，说明其中一些已经不是待审状态了。",
		permission.CommentReview,
		mcp.Object("评论审核参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid，必填：评论按工程隔离）"),
			"ids":       mcp.Array("要处理的评论 id 列表（用 comment_find 拿到的 id）", mcp.Integer("评论 id")),
			"status": mcp.Enum("目标状态：approved=通过（公开可见），rejected=驳回（不公开）",
				commentenums.StatusApproved, commentenums.StatusRejected),
		}, "projectId", "ids", "status"),
		nil,
		func(ctx context.Context, args commentReviewArgs) (mcp.Result, error) {
			if len(args.IDs) == 0 {
				return mcp.Result{}, &mcp.ArgsError{Msg: "ids 不能为空：要说明处理哪几条评论。先用 comment_find 拿到 id。"}
			}

			ids := dedupeIDs(args.IDs)
			if len(ids) > commentenums.MaxReviewIDs {
				return mcp.Result{}, &mcp.ArgsError{
					Msg: fmt.Sprintf("一次最多处理 %d 条，现在给了 %d 条。分批来。", commentenums.MaxReviewIDs, len(ids)),
				}
			}

			reviewer := mcp.UserIDFrom(ctx)
			if reviewer <= 0 {
				return mcp.Result{}, errors.New("commentmcp: 拿不到操作人身份，拒绝执行审核")
			}
			res, err := w.Review(ctx, &commentdto.ReviewReq{
				ProjectID:  strings.TrimSpace(args.ProjectID),
				IDs:        ids,
				Status:     args.Status,
				ReviewerID: uint64(reviewer),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: reviewResultText(args.Status, len(ids), res)}, nil
		})
}

func dedupeIDs(in []int64) []int64 {
	seen := make(map[int64]struct{}, len(in))
	out := make([]int64, 0, len(in))
	for _, id := range in {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func reviewResultText(status string, requested int, res *commentdto.ReviewResp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "已把 %d 条评论置为「%s」。", res.Affected, reviewStatusLabel(status))
	if res.Affected < int64(requested) {
		fmt.Fprintf(&b, "\n请求了 %d 条，实际改动 %d 条 —— 其余几条可能已经不在待审状态（别人先处理过），或不属于这个工程。",
			requested, res.Affected)
	}
	if status == commentenums.StatusApproved {
		b.WriteString("\n通过后它们会出现在站点的公开评论列表里。")
	} else {
		b.WriteString("\n驳回只是不公开，记录仍然保留，可以再改回来。")
	}
	return b.String()
}

func reviewStatusLabel(status string) string {
	if pair := commentenums.StatusLabel(status); strings.TrimSpace(pair.Fallback) != "" {
		return pair.Fallback
	}
	return status
}

const (
	commentFindDefaultPageSize = 10
	commentFindMaxPageSize     = 50

	commentBodyInlineMax = 120
)

func QueryTools(query commentcontract.QueryReader) ([]mcp.Tool, error) {
	if query == nil {
		return nil, errors.New("commentmcp: 评论查询依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{commentFind(query)}, nil
}

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

func clampCommentPageSize(n int) int {
	if n <= 0 {
		return commentFindDefaultPageSize
	}
	if n > commentFindMaxPageSize {
		return commentFindMaxPageSize
	}
	return n
}

func findCommentsText(res *commentdto.AdminListResp) string {
	if res == nil {
		return "没有查到评论。"
	}
	var b strings.Builder
	if len(res.Items) == 0 {

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

func commentStatusText(it commentdto.AdminItem) string {
	if strings.TrimSpace(it.StatusLabel) != "" {
		return it.StatusLabel
	}
	if pair := commentenums.StatusLabel(it.Status); strings.TrimSpace(pair.Fallback) != "" {
		return pair.Fallback
	}
	return it.Status
}

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

func inlineCommentBody(body string) string {
	flat := strings.Join(strings.Fields(body), " ")
	if flat == "" {
		return "（空正文）"
	}
	runes := []rune(flat)
	if len(runes) <= commentBodyInlineMax {
		return flat
	}

	return string(runes[:commentBodyInlineMax]) + fmt.Sprintf("…（还有 %d 字）", len(runes)-commentBodyInlineMax)
}
