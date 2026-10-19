package evolution

import (
	"encoding/json"
	"testing"
)

// Golden fixtures pin the exact ResponseFormat JSON the catalog/system/schemas/*.json
// files build through the library — these strings are what reflect.go's,
// golden.go's and promote.go's Go literals produced before the WP9a move; a
// change here means the catalog file's content changed, not just its
// location.
func TestGoldenResponseSchemas(t *testing.T) {
	cases := map[string]struct {
		schema map[string]interface{}
		want   string
	}{
		"agent_reflection": {
			schema: agentReflectionSchemaKey.Map(),
			want:   `{"additionalProperties":false,"properties":{"memories":{"items":{"additionalProperties":false,"properties":{"action":{"enum":["create","delete"],"type":"string"},"category":{"type":"string"},"content":{"type":"string"},"memory_id":{"type":"string"},"reason":{"type":"string"}},"required":["action","memory_id","content","category","reason"],"type":"object"},"type":"array"},"reverts":{"items":{"additionalProperties":false,"properties":{"evolution_event_id":{"type":"string"},"reason":{"type":"string"}},"required":["evolution_event_id","reason"],"type":"object"},"type":"array"},"rules":{"items":{"additionalProperties":false,"properties":{"action":{"enum":["create","update","delete"],"type":"string"},"content":{"type":"string"},"dimension":{"enum":["procedure","domain"],"type":"string"},"name":{"type":"string"},"priority":{"type":"integer"},"reason":{"type":"string"},"rule_id":{"type":"string"}},"required":["action","dimension","rule_id","name","content","priority","reason"],"type":"object"},"type":"array"},"self_assessment":{"type":"string"},"skills":{"items":{"additionalProperties":false,"properties":{"action":{"enum":["create","update","delete"],"type":"string"},"category":{"type":"string"},"content":{"type":"string"},"description":{"type":"string"},"dimension":{"enum":["procedure","domain"],"type":"string"},"name":{"type":"string"},"reason":{"type":"string"},"skill_id":{"type":"string"},"source_urls":{"items":{"type":"string"},"type":"array"}},"required":["action","dimension","skill_id","name","description","category","content","source_urls","reason"],"type":"object"},"type":"array"}},"required":["self_assessment","skills","rules","memories","reverts"],"type":"object"}`,
		},
		"probe_scores": {
			schema: probeScoresSchemaKey.Map(),
			want:   `{"additionalProperties":false,"properties":{"scores":{"items":{"additionalProperties":false,"properties":{"label":{"type":"string"},"reason":{"type":"string"},"score":{"type":"integer"}},"required":["label","score","reason"],"type":"object"},"type":"array"}},"required":["scores"],"type":"object"}`,
		},
		"evolution_lesson": {
			schema: evolutionLessonSchemaKey.Map(),
			want:   `{"additionalProperties":false,"properties":{"confidence":{"enum":["high","medium","low"],"type":"string"},"cue":{"type":"string"},"entry":{"type":"boolean"},"evidence":{"type":"string"},"lesson":{"type":"string"}},"required":["entry","cue","lesson","evidence","confidence"],"type":"object"}`,
		},
		"memory_promotion": {
			schema: memoryPromotionSchemaKey.Map(),
			want:   `{"additionalProperties":false,"properties":{"promotions":{"items":{"additionalProperties":false,"properties":{"agents":{"items":{"type":"string"},"type":"array"},"category":{"type":"string"},"content":{"type":"string"},"description":{"type":"string"},"memory_id":{"type":"string"},"name":{"type":"string"}},"required":["memory_id","name","description","category","content","agents"],"type":"object"},"type":"array"}},"required":["promotions"],"type":"object"}`,
		},
		"memory_or_skill": {
			schema: memoryOrSkillSchemaKey.Map(),
			want:   `{"additionalProperties":false,"properties":{"category":{"type":"string"},"content":{"type":"string"},"description":{"type":"string"},"name":{"type":"string"},"skill":{"type":"boolean"}},"required":["skill","name","description","category","content"],"type":"object"}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(tc.schema)
			if err != nil {
				t.Fatalf("marshal %s schema: %v", name, err)
			}
			if string(raw) != tc.want {
				t.Fatalf("%s schema changed\ngot:  %s\nwant: %s", name, raw, tc.want)
			}
		})
	}
}
