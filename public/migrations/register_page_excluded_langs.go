package migrations

import "sync"

func registerPageExcludedLangs() {
	registerPageExcludedLangsOnce.Do(func() {
		register(Migration{
			Version: "491-page-excluded-langs",
			SQL:     mustSQL("491_page_excluded_langs.sql"),
		})
	})
}

var registerPageExcludedLangsOnce sync.Once
