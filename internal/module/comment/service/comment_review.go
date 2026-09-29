package commentservice

// comment_review.go — 审核用例（后台控制面：批量通过 / 驳回）。
//
// 为什么批量用**一条 UPDATE** 而不是「逐条走单条路径」：
//
//   - 审核是**幂等**的状态设置（把一批 id 设成同一个状态），不是「每条都有不同的
//     附加动作」—— 逐条执行只会把 N 次往返摊在请求里，且需要 N 个事务；
//   - 单条 SQL 天然原子：要么这一批都改了，要么一条都没改（不存在「改了一半」的中间态）；
//   - 受影响行数（RowsAffected）如实回带「成功 N 条」—— 与请求条数不等时说明：
//     并发的另一次审核已经把它改成了目标状态（id 仍在、条件仍匹配，但值没变），
//     或者 id 不属于这个工程（RLS 与 WHERE 双重过滤掉了）。
//
// 权限不在这里判：审核是管理面写操作，权限点在路由注册处声明（comment:review），
// 由 Casbin 在进入 handler 之前拦掉（AGENTS.md §认证与鉴权）。service 只判**业务合法性**
// （状态是不是审核动作能设的那个、id 集合是不是空的）。

import (
	"context"

	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/logger"
)

// Review 批量设置审核状态（approved / rejected）。
func (s *Service) Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error) {
	if req == nil {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if len(req.IDs) == 0 {
		return nil, errParam(commentenums.ErrNothingSelected)
	}
	// 集合上限：handler 已经经 shell.BulkIDs 去重限过量，这里再判一次 ——
	// service 是契约的最终执行者，不能押注在调用方身上（将来多一个 API 调用点就漏了）。
	if len(req.IDs) > commentenums.MaxReviewIDs {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	// 审核动作只能设 approved / rejected（把一条评论「改成待审」不是判断结果）。
	if !commentenums.IsReviewableStatus(req.Status) {
		return nil, errParam(commentenums.ErrInvalidParam)
	}
	if err = s.validateProject(req.ProjectID); err != nil {
		return nil, err
	}
	affected, err := s.model.UpdateStatus(ctx, req.ProjectID, req.IDs, req.Status, req.ReviewerID)
	if err != nil {
		logger.Scene(errScene).
			With("reviewer_id", req.ReviewerID).
			With("status", req.Status).
			With("count", len(req.IDs)).
			Error(err, "批量审核写入失败")
		return nil, err
	}
	logger.Scene(errScene).
		With("reviewer_id", req.ReviewerID).
		With("status", req.Status).
		With("requested", len(req.IDs)).
		With("affected", affected).
		Info("批量审核完成")
	return &commentdto.ReviewResp{Affected: affected, Status: req.Status}, nil
}
