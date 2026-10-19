package evolution

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// Go-side handles onto catalog/system/schemas/*.json (see
// catalog/system/README.md, "Schemas").
var (
	agentReflectionSchemaKey = prompt.DefineSchema("agent_reflection")
	probeScoresSchemaKey     = prompt.DefineSchema("probe_scores")
	evolutionLessonSchemaKey = prompt.DefineSchema("evolution_lesson")
	memoryPromotionSchemaKey = prompt.DefineSchema("memory_promotion")
	memoryOrSkillSchemaKey   = prompt.DefineSchema("memory_or_skill")
)
