package sysconfigcontract

import (
	"context"
	"errors"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
)

type Service interface {
	GetGroup(ctx context.Context, groupKey string) (*Group, error)

	ListGroups(ctx context.Context) ([]Group, error)

	SetGroup(ctx context.Context, req *SetGroupReq) (*Group, error)

	SetGroups(ctx context.Context, req *SetGroupsReq) ([]Group, error)
}

type DictReader interface {
	ListDictOptions(ctx context.Context, dictType string) ([]DictOption, error)

	ListCountryOptions(ctx context.Context, lang string) ([]CountryOption, error)

	CountryLabel(ctx context.Context, lang, code string) string
}

var ErrGroupNotFound = errors.New("sysconfig: 配置分组不存在")

type ConfigReader interface {
	GetGroup(ctx context.Context, groupKey string) (*Group, error)
}

type Group = sysconfigdto.Group

type SetGroupReq = sysconfigdto.SetGroupReq

type DictOption = sysconfigdto.DictOption

type CountryOption = sysconfigdto.CountryOption

type SetGroupsReq = sysconfigdto.SetGroupsReq

const (
	GroupI18n = "i18n"

	GroupTrade = "trade"

	GroupAI = "ai"
)

const (
	KeyMCPEnabled = "mcp_enabled"
)

const (
	KeyDefaultLang = "default_lang"

	KeySiteLangURLMode = "site_lang_url_mode"

	KeyLangURLCodes = "lang_url_codes"
)
