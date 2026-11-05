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

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/logger"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func SetupArtifactRoutes(rg *mw.RouteGroup, db *gorm.DB) artifactcontract.ArtifactService {
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	g := rg.Group("/artifact", builtin.SessionAuthMiddleware())

	g.GET("/detail", artifactenums.ArtifactDetail, ArtifactDetail(svc))
	return svc
}

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

func artifactDetailResponse(c *gin.Context, res *artifactdto.ArtifactResp, err error) {
	switch {
	case err == nil && res != nil:
		response.SuccessWithMessage(c, artifactenums.MsgArtifactFound, res)

	case err != nil && (err.Error() == artifactenums.ErrArtifactNotFound || errors.Is(err, gorm.ErrRecordNotFound)):
		response.ErrorWithMessage(c, http.StatusNotFound, artifactenums.ErrArtifactNotFound)
	case err != nil && err.Error() == artifactenums.ErrInvalidParam:
		response.ErrorWithMessage(c, http.StatusBadRequest, artifactenums.ErrInvalidParam)
	default:
		logger.Scene("artifact").With("path", c.Request.URL.Path).Error(err, "产物详情查询失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, artifactenums.ErrArtifactNotFound)
	}
}
