package pageenums

import "testing"

func TestValidatePageContentContract(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	if !ValidatePageContentContract(PageKindHome, "none", nil) {
		t.Fatal("home should allow none target")
	}
	if ValidatePageContentContract("product", "product", &id) {
		t.Fatal("product kind removed in migration 080")
	}
	if !ValidatePageContentContract(PageKindArticle, "article", &id) {
		t.Fatal("article with target should pass")
	}
}
