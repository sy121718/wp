package artifacthttp

import (
	"errors"
	"net/http"
	"strings"

	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/internal/permission"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupArtifactRoutes 自装配 artifact 模块（产物元数据查询接口）。
func SetupArtifactRoutes(rg *permission.RouteGroup, db *gorm.DB) artifactcontract.ArtifactService {
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	g := rg.Group("/artifact", builtin.SessionAuthMiddleware())
	// 产物元数据查询（FIX-24）：接口与实现一直都在（contract 的 Detail / DetailByID，
	// service/artifact_record.go），此前只有路由是个 501 占位 + 一句「尚未实现」的错注释
	// —— 「做完了，门忘了开」。它是运维判断「产物文件丢了算不算重建」的入口。
	//
	// **两种形状共用一个端点**：按 (pageId, hash) 查（哈希覆盖 Manifest.lang，不需要语言参数）
	// 与按产物行 id 查。用 query 里出现 `id` 就视为按 id —— 两者都是必填单值，
	// 同时给出 `id` 与 `pageId` 时按 id 优先（它更精确），不去猜调用方意图。
	g.GET("/detail", permission.ArtifactDetail, ArtifactDetail(svc))
	return svc
}

// ArtifactDetail 产物详情查询 handler（导出以便集成测试直接调用：
// 路由上挂着 Session + Casbin，测试环境不具备会话时无法经 HTTP 打进来，
// 而这两个端点此前**从未**被接线过，必须能在不改中间件的前提下验证）。
func ArtifactDetail(svc artifactcontract.ArtifactService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if id := strings.TrimSpace(c.Query("id")); id != "" {
			res, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: id})
			artifactDetailResponse(c, res, err)
			return
		}
		res, err := svc.Detail(ctx, &artifactdto.DetailReq{
			PageID: strings.TrimSpace(c.Query("pageId")),
			Hash:   strings.TrimSpace(c.Query("hash")),
		})
		artifactDetailResponse(c, res, err)
	}
}

// artifactDetailResponse 产物详情的统一出口。
//
// 三种结果分开报：缺参（400，调用方传错）、找不到（404）、其它（500 归口）。
// 「找不到」不能返回 200 空体 —— 运维正是要靠这个端点区分「产物行不在」与「产物文件丢了」，
// 空体会把前者伪装成后者。
func artifactDetailResponse(c *gin.Context, res *artifactdto.ArtifactResp, err error) {
	switch {
	case err == nil && res != nil:
		response.SuccessWithMessage(c, artifactenums.MsgArtifactFound, res)
	// 服务的错误是**文案哨兵**（errors.New(artifactenums.Xxx)），不是导出的哨兵变量 ——
	// 因此这里按 enums 常量比对（与模块内既有出口同一口径），并兜住 gorm 的 not found。
	case err != nil && (err.Error() == artifactenums.ErrArtifactNotFound || errors.Is(err, gorm.ErrRecordNotFound)):
		response.ErrorWithMessage(c, http.StatusNotFound, artifactenums.ErrArtifactNotFound)
	case err != nil && err.Error() == artifactenums.ErrInvalidParam:
		response.ErrorWithMessage(c, http.StatusBadRequest, artifactenums.ErrInvalidParam)
	default:
		logger.Scene("artifact").With("path", c.Request.URL.Path).Error(err, "产物详情查询失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, artifactenums.ErrArtifactNotFound)
	}
}
