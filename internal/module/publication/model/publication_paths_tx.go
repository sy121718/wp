package pubmodel

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// ListRoutePathsByPageTx 在调用方事务内重读某页面当前持有的访问路径集合。
//
// 删除/下线页面必须在业务事务的行锁内用本方法复核「登记意图时的路径集合」，
// 不能顺手调用事务外的 ListRoutePathsByPage —— 后者会新起一个事务，
// 读到的快照与锁内的行不是同一视图，比较结果不可信。
//
// 事务的工程作用域由调用方设置（RLS 会话变量是事务级的）；
// 路径按字典序返回，供调用方做稳定比较。
func (m *Model) ListRoutePathsByPageTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) (paths []string, err error) {
	if terr := requireRealTx(tx); terr != nil {
		return nil, terr
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(pageID) == "" {
		return nil, errors.New("publication: 事务内读取页面路径缺少工程或页面")
	}
	paths = []string{}
	err = tx.WithContext(ctx).Model(&RouteEntity{}).
		Where("project_id = ? AND page_id = ? AND route_kind IN ?",
			projectID, pageID, []string{RouteActive, RouteRedirect}).
		Order("path ASC").
		Pluck("path", &paths).Error
	if err != nil {
		return nil, err
	}
	return paths, nil
}
