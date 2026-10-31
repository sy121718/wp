package adminmodel

import "go_wp/pkg/datarule"

func AdminDataRuleDomain() (datarule.DomainConfig, error) {
	return datarule.DomainFromEntity("ADMIN", "管理员", AdminEntity{})
}
