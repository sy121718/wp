package userhttp

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	usercontract "go_wp/internal/module/user/contract"
	usermodel "go_wp/internal/module/user/model"
	userservice "go_wp/internal/module/user/service"
	"go_wp/internal/permission"
)

const (
	userSensitiveRateLimit  = 10
	userSensitiveRateWindow = time.Minute
)

func SetupUserRoutes(
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
	setupSiteUserRoutes(router, h)

	return svc
}

func SetupCustomerAdminRoutes(rg *permission.RouteGroup, svc usercontract.CustomerAdminPort) {
	if rg == nil || svc == nil {
		return
	}
	h := NewCustomerHandle(svc)
	g := rg.Group("/customer")
	g.GET("/list", permission.UserCustomerList, h.ListCustomers)
	g.GET("/get", permission.UserCustomerDetail, h.GetCustomer)
	g.POST("/status", permission.UserCustomerStatus, h.SetCustomerStatus)
	g.POST("/unlock", permission.UserCustomerUnlock, h.UnlockCustomer)
	g.POST("/bulk-status", permission.UserCustomerBulkStatus, h.BulkCustomerStatus)
	g.POST("/bulk-unlock", permission.UserCustomerBulkUnlock, h.BulkCustomerUnlock)
}
