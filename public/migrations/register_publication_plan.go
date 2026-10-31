package migrations

import "sync"

func registerPagePublicationPlans() {
	registerPagePublicationPlansOnce.Do(registerPagePublicationPlansSQL)
}

var registerPagePublicationPlansOnce sync.Once

func registerPagePublicationPlansSQL() {
	register(Migration{
		Version:   "308-page-publication-plans",
		TableName: "page_publication_plans",
		SQL:       mustSQL("308_page_publication_plans.sql"),
	})
}
