package commentmcp

// comment_review_tools.go — 评论审核（批量置为通过 / 驳回）的写工具。
//
// 为什么把它加进来（推翻过一版判断，理由留在这里备查）：
// comment_tools.go 最初写着「审核是人的判断，模型代替不了这个位置」。这句话
// 把位置和***谁授权***弄混了 —— 关键在于 ReviewerID 是不是真实的登录者，
// 而它取自 context（Runner 在执行前注入），参数里根本没有这个字段。
// 后台管理员点「通过」与让助手点「通过」是同一次由人授权的操作，留痕一样成立。
//
// 真正需要守住的因此变成了另外两件事，都写在这里：
//   · 目标状态只允许 approved / rejected（service 也判，这里判是为了给出人能看懂的错）；
//   · projectId 必填 —— 评论按工程隔离，一次不带工程的批量改动是危险的越界。

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

// WriteTools 返回评论模块的写工具集。
func WriteTools(w commentcontract.ReviewWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("commentmcp: 评论审核依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{commentReview(w)}, nil
}

// commentReviewArgs comment_review 的入参。
//
// 没有 reviewerId：操作人从登录身份来，见文件头。
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
			// 去重 + 定量：重复 id 会让 service 的「影响条数」对不上请求条数，
			// 而那个数字是回答里唯一能证明「到底改了几条」的东西。
			ids := dedupeIDs(args.IDs)
			if len(ids) > commentenums.MaxReviewIDs {
				return mcp.Result{}, &mcp.ArgsError{
					Msg: fmt.Sprintf("一次最多处理 %d 条，现在给了 %d 条。分批来。", commentenums.MaxReviewIDs, len(ids)),
				}
			}
			// 操作人必须是真实登录者：拿不到身份就不执行。
			// 放行等于写一条「审核人是 0」的流水，那条留痕谁也追溯不了。
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

// dedupeIDs 去重并保持原顺序（顺序是模型给的优先级，不要打乱它）。
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

// reviewResultText 审核结果正文。
//
// 必须把「请求了几条 / 实际改了几条」都说出来：两者不等时用户才知道
// 有一部分评论已经不在待审状态（可能刚被别人处理过），而不是以为全处理完了。
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

// reviewStatusLabel 目标状态的中文说法。
//
// 这里只有两个取值（approved / rejected），不走 commentdto.AdminItem 那条
// StatusLabel 优先的路径 —— 那是给**已有条目**用的（条目上带着页面渲染时的译文），
// 而这里说的是「我刚设成了什么」，没有条目可依。
func reviewStatusLabel(status string) string {
	if pair := commentenums.StatusLabel(status); strings.TrimSpace(pair.Fallback) != "" {
		return pair.Fallback
	}
	return status
}
