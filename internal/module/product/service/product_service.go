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

	publishedLocator   presentationcontract.PublishedEntityLocator
	publicCatalogIndex presentationcontract.PublishedEntityIndex

	archiveEnsurer presentationcontract.ArchiveInstanceEnsurer

	invalidator productcontract.DependencyInvalidator

	purchases productcontract.PurchaseChecker

	pageWarehouses productcontract.WarehouseOptionsPort
	pageTemplates  productcontract.EntityTemplatePort

	pageDetailTemplates productcontract.DetailTemplatePort

	pagePublishRecords productcontract.PublishRecordReader

	pagePublishAttempts productcontract.PublishAttemptReader
}

func (s *Service) SetDetailTemplatePort(port productcontract.DetailTemplatePort) {
	s.pageDetailTemplates = port
}

func (s *Service) SetPublishRecordPort(port productcontract.PublishRecordReader) {
	s.pagePublishRecords = port
}

func (s *Service) SetPublishAttemptPort(port productcontract.PublishAttemptReader) {
	s.pagePublishAttempts = port
}

func (s *Service) SetInventory(reader inventorycontract.ProductStockReader) { s.inv = reader }

func NewService(m *productmodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
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

func (s *Service) SetMasterDataChanges(port masterdatacontract.MasterDataService) {
	s.changes = port
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
