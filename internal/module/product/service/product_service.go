package productservice

import (
	inventorycontract "go_wp/internal/module/inventory/contract"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

type Service struct {
	m       *productmodel.Model
	project projectcontract.ProjectService

	inv inventorycontract.ProductStockReader

	translationStore ProductTranslationStorePort

	translationInstances ProductTranslationInstancePort

	i18nStaleMarker I18nStaleMarker

	contentStore i18n.ContentStore

	invSvc inventorycontract.ProductStockPort

	availability productcontract.VariantAvailabilityPort

	changes masterdatacontract.MasterDataService

	publishedLocator presentationcontract.PublishedEntityLocator

	archiveEnsurer presentationcontract.ArchiveInstanceEnsurer

	invalidator productcontract.DependencyInvalidator

	purchases productcontract.PurchaseChecker

	pageWarehouses productcontract.WarehouseOptionsPort
	pageTemplates  productcontract.EntityTemplatePort

	pageDetailTemplates productcontract.DetailTemplatePort

	pagePublishAttempts productcontract.PublishAttemptReader
}

func (s *Service) SetDetailTemplatePort(port productcontract.DetailTemplatePort) {
	s.pageDetailTemplates = port
}

func (s *Service) SetPublishAttemptPort(port productcontract.PublishAttemptReader) {
	s.pagePublishAttempts = port
}

func (s *Service) SetInventory(reader inventorycontract.ProductStockReader) { s.inv = reader }

// NewService 交出商品服务：m、project 与审计变更记录三件必需依赖都在这里给齐。
//
// changes 是必需依赖而不是可晚接的 setter：商品的每次写都要在同一个事务里记审计变更，
// 缺它只能在运行到写路径时才炸，那是装配错误被发现得最晚的一种。nil 直接 panic，
// 让装配期就报出「没接 masterdata」。
func NewService(m *productmodel.Model, project projectcontract.ProjectService,
	changes masterdatacontract.MasterDataService) *Service {
	if changes == nil {
		panic("productservice.NewService：缺少 masterdata 审计变更依赖")
	}
	return &Service{m: m, project: project, changes: changes}
}

func (s *Service) SetContentStore(store i18n.ContentStore) {
	s.contentStore = store
}

func (s *Service) SetInventoryService(port inventorycontract.ProductStockPort) {
	s.invSvc = port
}

func (s *Service) SetAvailabilityPort(port productcontract.VariantAvailabilityPort) {
	s.availability = port
}

func (s *Service) SetArchiveInstanceEnsurer(port presentationcontract.ArchiveInstanceEnsurer) {
	s.archiveEnsurer = port
}

func (s *Service) SetPublishedEntityLocator(port presentationcontract.PublishedEntityLocator) {
	s.publishedLocator = port
}

var (
	_ productcontract.ProductService = (*Service)(nil)

	_ productcontract.ProductDataSource = (*Service)(nil)

	_ productcontract.VariantCostPort = (*Service)(nil)
)

func (s *Service) SetDependencyInvalidator(inv productcontract.DependencyInvalidator) {
	if s == nil {
		return
	}
	s.invalidator = inv
}
