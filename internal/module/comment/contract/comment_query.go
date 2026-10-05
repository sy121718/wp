package commentcontract

// comment_query.go — 评论模块的查询与审核契约（AI 工具用）。
//
// 为什么不直接复用 CommentService：那个接口里还有 Submit（要 IP 哈希、面向公开面），
// 工具手里只该有「读后台审核队列」与「改一条评论的审核状态」这两件事。
//
// 关于**审核动作要不要交给模型**（这里推翻过一版判断，留痕备查）：
// 初版把它排除在外，理由是「审核是人做出的判断，交给模型会让这条留痕失去意义」。
// 这个理由站不住 —— 判断是谁做出的，取决于**谁让模型去做的**，而不是谁点了按钮：
// ReviewerID 取自当前登录会话（工具从 context 拿，参数里没有这个字段），
// 后台管理员点「通过」与让助手点「通过」是同一次由人授权的操作，留痕同样成立。
// 真正需要守住的是别的东西，所以 Review 这一侧只有两条约束：
//   · 目标状态只允许 approved / rejected（service 已经在白名单里把关）；
//   · 必填 projectId —— 评论按工程隔离，不带工程的批量改动是危险的越界。

import (
	"context"

	commentdto "go_wp/internal/module/comment/dto"
)

// QueryReader 评论的只读查询能力。
type QueryReader interface {
	// AdminList 后台审核队列：按工程 + 状态 / 实体类型 / 正文关键词筛选，分页返回。
	//
	// **ProjectID 必填**：评论按工程隔离，不填就返回不了「这个站点的评论」——
	// 工具层把这一点写进 schema 的必填项，而不是让它悄悄查全部工程。
	AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error)
}

// ReviewWriter 评论的审核状态变更能力（只暴露 Review，不暴露 Submit）。
type ReviewWriter interface {
	// Review 批量把评论置为 approved / rejected。
	//
	// ReviewerID 由调用方（工具层）从 context 里的登录身份填入，**不由模型提供**。
	Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error)
}
