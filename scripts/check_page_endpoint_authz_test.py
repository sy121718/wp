import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('authz', Path(__file__).with_name('check-page-endpoint-authz.py'))
authz = importlib.util.module_from_spec(spec)
spec.loader.exec_module(authz)

class VisitorRouteShapeTest(unittest.TestCase):
    def routes(self, source):
        clean, mask = authz.strip_comments(source)
        return [authz.site_api_route(clean, mask, at, authz.first_string_literal(authz.split_args(args)[0])) for at,args in authz.iter_calls(clean,mask,'.POST(')]

    def test_resolves_visitor_group_and_private_child(self):
        self.assertEqual(self.routes('''func setup(router *gin.Engine) {
 api := router.Group("/api/site/user", VisitorCSRFMiddleware())
 api.POST("/login", h.Login)
 own := api.Group("", requireSiteUser())
 own.POST("/profile", h.Profile)
}'''), [True,True])

    def test_does_not_accept_admin_api_or_similarly_named_path(self):
        self.assertEqual(self.routes('''func setup(router *gin.Engine) {
 admin := router.Group("/admin")
 admin.POST("/api/site/user/login", h.Login)
 api := router.Group("/api/siteevil/user")
 api.POST("/login", h.Login)
 router.POST("/api/product/create", h.Create)
}'''), [False,False,False])

    def test_same_variable_in_other_function_does_not_inherit_public_scope(self):
        self.assertEqual(self.routes('''func public(router *gin.Engine) {
 g := router.Group("/api/site/cart", middleware...)
 g.POST("/add", h.Add)
}
func admin(router *gin.Engine) {
 g := router.Group("/admin")
 g.POST("/delete", h.Delete)
}'''), [True,False])

if __name__ == '__main__':
    unittest.main()
