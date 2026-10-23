package productservice

import "testing"

func TestIsProductStatus(t *testing.T) {
	for _, ok := range []string{"draft", "published", "archived"} {
		if !isProductStatus(ok) {
			t.Errorf("%q 应是合法状态", ok)
		}
	}
	for _, bad := range []string{"", "DRAFT", "deleted", "published ", "active"} {
		if isProductStatus(bad) {
			t.Errorf("%q 不该是合法状态", bad)
		}
	}
}
