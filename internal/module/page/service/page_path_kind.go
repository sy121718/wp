package pageservice

// page_path_kind.go — 按线上访问路径批量反查页面类型（只读，跨模块聚合的入口）。
//
// 这一层只做工程校验与委托：路径归一化（去重、剔空）与 SQL 谓词都在 model 里，
// 这里再写一遍就会出现两份「什么算同一路径」的判定。

import (
	"context"
	"strings"
)

// KindsOfPaths 按已发布的访问路径批量反查页面类型（path → kind）。
//
// 返回的 map 只含**本工程内、未删除、且已发布**的路径；查不到的路径不出现在结果里
// （调用方据此把它们排除，而不是猜一个默认类型）。
func (s *Service) KindsOfPaths(ctx context.Context, projectID string, paths []string) (kinds map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if err = s.requireProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.model.KindsOfPaths(ctx, projectID, paths)
}
