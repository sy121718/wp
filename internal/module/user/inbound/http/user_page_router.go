package userhttp

import (
	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"
	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	pageview "go_wp/pkg/view"
)

const (
	CustomersListPageTemplate    = "admin/user/customers.html"
	CustomerDetailPageTemplate   = "admin/user/customer_detail.html"
	CustomerOverviewPageTemplate = "admin/user/customer_overview"
	CustomerCohortPageTemplate   = "admin/user/customer_cohort"
	CustomerRfmPageTemplate      = "admin/user/customer_rfm"
)

func SetupCustomerPages(pages *gin.RouterGroup,
	users usercontract.CustomerAdminPort,
	orders ordercontract.CustomerOrderSummaryReader,
	projects projectcontract.ProjectService,
	membership membershipcontract.Reader,
	growth ordercontract.CustomerGrowthReader,
	segments ordercontract.CustomerSegmentReader,
	rfm ordercontract.CustomerRfmReader,
	cohort ordercontract.CustomerCohortReader,
	membershipAdmin membershipcontract.AssignmentAdminPort,
	membershipTiers membershipcontract.TierAdminPort) {
	if pages == nil {
		return
	}

	h := NewCustomerPageHandle(users, orders, projects)

	h.SetMembershipDisplay(membership)
	h.SetCustomerGrowth(growth)
	h.SetCustomerSegments(segments)
	h.SetCustomerRfm(rfm)
	h.SetCustomerCohort(cohort)
	h.SetMembershipFilters(membershipAdmin, membershipTiers)
	pages.GET("/customers",
		mw.PageAuthz("/api/customer/list"),
		pageview.UseRenderer(pageview.RenderPage),
		h.CustomersPage)

	pages.GET("/customers/overview",
		mw.PageAuthz("/api/customer/list"),
		pageview.UseRenderer(pageview.RenderPage),
		h.CustomerOverviewPage)

	pages.GET("/customers/rfm",
		mw.PageAuthz("/api/customer/list"),
		pageview.UseRenderer(pageview.RenderPage),
		h.CustomerRfmPage)

	pages.GET("/customers/cohort",
		mw.PageAuthz("/api/customer/list"),
		pageview.UseRenderer(pageview.RenderPage),
		h.CustomerCohortPage)

	pages.GET("/customers/detail",
		mw.PageAuthz("/api/customer/list"),
		pageview.UseRenderer(pageview.RenderPage),
		h.CustomerDetailPage)
}
