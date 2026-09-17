package adminhttp

import (
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	datarulepkg "go_wp/pkg/datarule"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

// bootstrapDataRule 装配 admin 的数据权限：注册数据域 → 注入 RuleProvider → 挂 GORM 插件。
//
// 域声明（白名单、表名）来自 AdminEntity 字段上的 datarule tag，本函数只负责把它交给引擎；
// 声明写错（未知操作符、缺 label、表名对不上）在这里直接 panic：域没注册上的后果是
// resolveDomain 永不命中，该表的行级过滤整体静默失效（beforeQuery fail-open）——
// 这类错必须停在启动阶段，不能等到「规则存进去了却一条都没拦住」才发现。
func bootstrapDataRule(svc *adminservice.Service, db *gorm.DB) {
	domain, err := adminmodel.AdminDataRuleDomain()
	if err != nil {
		panic("admin 数据域声明不合法: " + err.Error())
	}
	if err := datarulepkg.RegisterDomain(domain); err != nil {
		panic("admin 数据域注册失败: " + err.Error())
	}

	datarulepkg.SetProvider(svc)
	if err := datarulepkg.RegisterPluginWithDB(db); err != nil {
		logger.Scene("init").Error(err, "注册 datarule GORM 插件失败")
	}
}
