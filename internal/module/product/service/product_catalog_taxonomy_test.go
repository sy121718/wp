package productservice

import "testing"

func TestCatalogTaxonomyEmptyProjectSkipsDataQueries(t *testing.T) {

	s := NewService(nil, nil)
	brands, err := s.CatalogBrands(t.Context(), "")
	if err != nil || brands == nil || len(brands) != 0 {
		t.Fatalf("空工程品牌应为空集合：%+v %v", brands, err)
	}
	tags, err := s.CatalogTags(t.Context(), "")
	if err != nil || tags == nil || len(tags) != 0 {
		t.Fatalf("空工程标签应为空集合：%+v %v", tags, err)
	}
	attributes, err := s.CatalogAttributes(t.Context(), "")
	if err != nil || attributes == nil || len(attributes) != 0 {
		t.Fatalf("空工程属性应为空集合：%+v %v", attributes, err)
	}
}
