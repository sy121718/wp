package navigationcontract

import (
	"context"

	navigationdto "go_wp/internal/module/navigation/dto"
)

type MenuStaleDispatcher interface {
	InvalidateMenu(ctx context.Context, projectID, kind string) error

	InvalidateNavigation(ctx context.Context, projectID, navigationID string) error
}

type SourceResolver interface {
	ResolveSource(ctx context.Context, projectID, sourceType, sourceID string) (title, url string, err error)

	Candidates(ctx context.Context, projectID string) (groups []SourceGroup, err error)
}

type SourceGroup struct {
	Type string

	Title string

	Items []SourceCandidate
}

type SourceCandidate struct {
	ID string

	Label string

	Title string

	URL string
}

type NavigationService interface {
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)

	Update(ctx context.Context, req *navigationdto.UpdateReq) (res *navigationdto.NavigationResp, err error)

	Get(ctx context.Context, req *navigationdto.GetReq) (res *navigationdto.NavigationResp, err error)

	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)

	Tree(ctx context.Context, projectID, kind string) (nodes []*navigationdto.NavigationNode, err error)

	TreeByID(ctx context.Context, projectID, navigationID string) (nodes []*navigationdto.NavigationNode, err error)

	SetSourceResolver(r SourceResolver)

	SourceGroups(ctx context.Context, projectID string) (groups []SourceGroup, err error)

	SwapMove(ctx context.Context, req *navigationdto.SwapMoveReq) (res *navigationdto.SwapMoveResp, err error)

	Delete(ctx context.Context, req *navigationdto.DeleteReq) (err error)

	// BulkDelete 在一个事务里删除多个导航项：任一失败整批回滚。
	BulkDelete(ctx context.Context, req *navigationdto.BulkDeleteReq) error

	// AddSources 从来源候选批量加入菜单：候选规则、校验与写入同一事务，任一失败整批回滚。
	AddSources(ctx context.Context, req *navigationdto.AddSourceReq) (added int, err error)

	Render(ctx context.Context, projectID, kind string) (html string, err error)
}
