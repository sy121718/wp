package userhttp

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	usercontract "go_wp/internal/module/user/contract"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const (
	userSensitiveRateLimit  = 10
	userSensitiveRateWindow = time.Minute
)

func SetupUserRoutes(
	reg *mw.Registry,
	router *gin.Engine,
	db *gorm.DB,
	mail usercontract.MailSender,
	siteName string,
) usercontract.UserService {

	if err := setupUserCookieStore(); err != nil {
		panic("用户模块：初始化访客会话存储失败: " + err.Error())
	}

	svc := userservice.NewService(
		usermodel.NewUserModel(db),
		usermodel.NewUserProfileModel(db),
		usermodel.NewUserPreferenceModel(db),
		mail,
		siteName,
	)
	h := NewHandle(svc)
	setupSiteUserRoutes(reg, router, h)

	return svc
}

func SetupCustomerAdminRoutes(rg *mw.RouteGroup, svc usercontract.CustomerAdminPort) {
	if rg == nil || svc == nil {
		return
	}
	h := NewCustomerHandle(svc)
	g := rg.Group("/customer")
	g.GET("/list", userenums.UserCustomerList, h.ListCustomers)
	g.GET("/get", userenums.UserCustomerDetail, h.GetCustomer)
	g.POST("/status", userenums.UserCustomerStatus, pageview.HTMLFragment, h.SetCustomerStatus)
	g.POST("/unlock", userenums.UserCustomerUnlock, pageview.HTMLFragment, h.UnlockCustomer)
	g.POST("/bulk-status", userenums.UserCustomerBulkStatus, pageview.HTMLFragment, h.BulkCustomerStatus)
	g.POST("/bulk-unlock", userenums.UserCustomerBulkUnlock, pageview.HTMLFragment, h.BulkCustomerUnlock)
}
