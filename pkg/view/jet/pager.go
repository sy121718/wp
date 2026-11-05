package jet

import "go_wp/pkg/pagination"

type Pager = pagination.Derived

func PagerOf(p pagination.Page) Pager {
	return pagination.Derive(p)
}
