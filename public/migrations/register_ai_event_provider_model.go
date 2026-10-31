package migrations

import "sync"

func registerAIEventProviderModel() {
	registerAIEventProviderModelOnce.Do(registerAIEventProviderModelSeed)
}

var registerAIEventProviderModelOnce sync.Once

func registerAIEventProviderModelSeed() {
	registerSeed(Seed{
		Version:   "529-ai-event-provider-model",
		TableName: "ai_event",
		ConditionSQL: "SELECT CASE WHEN " +
			"EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'ai_event' AND column_name = 'provider_key') " +
			"AND EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'ai_event' AND column_name = 'model_id') " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("529_ai_event_provider_model.sql"),
	})
}
