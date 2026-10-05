package commentcontract

// comment_query.go — 评论模块的**只读**查询契约（AI 工具用）。
//
// 为什么不直接复用 CommentService：那个接口里还有 Submit / Review —— 前者要 IP 哈希、
// 后者是审核动作。AI 工具手里只该有「读后台审核队列」这一件事，把审核动作留在
// 管理页面上：本模块的审核是**人做出的判断**（谁在什么时刻放行了哪条评论是审计事实），
// 交给模型执行会让这条留痕失去意义。
//
// 与 comment_admin.go 的 CommentAdminPort 的关系（两者都读后台队列，看似重复）：
// CommentAdminPort 面向后台**页面**，返回的是页面渲染需要的形态（带分页游标、
// 供 tab 计数用）；这里的 QueryReader 面向工具，只需要「按工程 + 条件列出条目与总数」。
// 合成一个接口的话，页面要的东西会变成工具的强制依赖 —— 工具不需要分页游标，
// 但它会随页面一起漂移。

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
