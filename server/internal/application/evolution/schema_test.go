package evolution

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Mirrors the orchestrator package's check: strict json_schema mode rejects a schema unless every object sets "additionalProperties": false and lists every one of its own properties in "required". parseReflectionOutput never checks missing keys — domain.ReflectionOutput is unmarshaled directly — so this is purely the API-level constraint, the one a hand-edited schema would otherwise only break in production.
func assertStrictObjectSchema(t *testing.T, schema map[string]interface{}, path string) {
	t.Helper()
	typ, _ := schema["type"].(string)
	switch typ {
	case "object":
		if addl, ok := schema["additionalProperties"].(bool); !ok || addl != false {
			t.Errorf("%s: additionalProperties = %v, want literal false", path, schema["additionalProperties"])
		}
		props, _ := schema["properties"].(map[string]interface{})
		required := schemaRequired(schema)
		reqSet := make(map[string]bool, len(required))
		for _, r := range required {
			reqSet[r] = true
		}
		var missing []string
		for name := range props {
			if !reqSet[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: properties not listed in required: %v", path, missing)
		}
		if len(required) != len(props) {
			t.Errorf("%s: required has %d entries, properties has %d", path, len(required), len(props))
		}
		for name, sub := range props {
			if subSchema, ok := sub.(map[string]interface{}); ok {
				assertStrictObjectSchema(t, subSchema, path+"."+name)
			}
		}
	case "array":
		if items, ok := schema["items"].(map[string]interface{}); ok {
			assertStrictObjectSchema(t, items, path+"[]")
		}
	}
}

// schemaRequired reads "required" leniently: a schema built in Go carries it
// as []string, but one loaded from catalog/system/schemas/*.json through
// encoding/json carries it as []interface{} of strings.
func schemaRequired(schema map[string]interface{}) []string {
	switch v := schema["required"].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func TestReflectionOutputSchema_MarshalsAndSatisfiesStrictMode(t *testing.T) {
	schema := agentReflectionSchemaKey.Map()

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("marshaled schema is empty")
	}
	var roundTrip map[string]interface{}
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("unmarshal schema back: %v", err)
	}
	if roundTrip["type"] != "object" {
		t.Errorf("type = %v, want object", roundTrip["type"])
	}

	assertStrictObjectSchema(t, schema, "reflection")
}

// Locks the contract that made the prompt-only instruction unenforceable: a CLI-path model has no schema, so parseReflectionOutput is the only enforcement there is; the HTTP-path schema still requires reason on every change kind so strict providers enforce it too.
func TestReflectionOutputSchema_ChangesRequireReason(t *testing.T) {
	schema := agentReflectionSchemaKey.Map()
	props, _ := schema["properties"].(map[string]interface{})

	for _, key := range []string{"skills", "rules", "memories"} {
		arr, ok := props[key].(map[string]interface{})
		if !ok {
			t.Fatalf("schema missing array property %q", key)
		}
		items, ok := arr["items"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: missing items schema", key)
		}
		required := schemaRequired(items)
		found := false
		for _, r := range required {
			if r == "reason" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: items schema does not require \"reason\": %v", key, required)
		}
		itemProps, _ := items["properties"].(map[string]interface{})
		if _, ok := itemProps["reason"]; !ok {
			t.Errorf("%s: items schema has no \"reason\" property", key)
		}
	}
}

type recordingLLM struct {
	answer   string
	requests []domain.AgentRequest
}

func (r *recordingLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	r.requests = append(r.requests, req)
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: r.answer}}, nil
}

func (r *recordingLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (r *recordingLLM) Models(_ context.Context) ([]string, error) { return nil, nil }
func (r *recordingLLM) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return nil, nil
}

// runLLM used to send bare {"type":"json_object"} with no schema, so a provider with strict structured-output support had nothing to constrain decoding against; it must now carry the real schema.
func TestRunLLM_RequestCarriesTheReflectionSchema(t *testing.T) {
	llm := &recordingLLM{answer: `{"self_assessment":"ok","skills":[],"rules":[],"memories":[],"reverts":[]}`}
	svc := &Service{
		llm: llm,
		cfg: domain.EvolutionConfig{MaxSkillChanges: 3, MaxRuleChanges: 3, MaxMemoryChanges: 5},
	}

	output, raw, _, err := svc.runLLM(context.Background(), domain.Agent{Name: "backend-developer"}, "evidence text")
	if err != nil {
		t.Fatalf("runLLM: %v", err)
	}
	if output.SelfAssessment != "ok" {
		t.Errorf("self_assessment = %q, want %q", output.SelfAssessment, "ok")
	}
	if raw == "" {
		t.Error("raw output must be recorded even on success")
	}
	if len(llm.requests) != 1 {
		t.Fatalf("llm calls = %d, want 1 (no parse failure to retry)", len(llm.requests))
	}

	rf := llm.requests[0].ResponseFormat
	if rf == nil {
		t.Fatal("ResponseFormat is nil, want the reflection schema")
	}
	if rf.Type != domain.ResponseFormatJSONSchema {
		t.Errorf("ResponseFormat.Type = %q, want %q", rf.Type, domain.ResponseFormatJSONSchema)
	}
	if rf.Name == "" {
		t.Error("ResponseFormat.Name is empty; some providers reject an unnamed schema")
	}
	if rf.Schema == nil {
		t.Fatal("ResponseFormat.Schema is nil, want the reflection output schema")
	}
	if rf.Schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", rf.Schema["type"])
	}
}

// ---- Schema/struct parity ----
//
// Checks each strict-mode schema's promise about the wire shape against the
// Go struct the response actually decodes into, reflecting over its json
// tags so the two cannot drift silently. See the orchestrator package's
// schemas_test.go for the shared design and this package's WP9a report for
// the known loosenesses (parseReflectionOutput's per-field, non-struct
// decode chief among them — this parity check targets the shape it fills
// domain.ReflectionOutput with, not its own permissive extraction).

func schemaProperties(schema map[string]interface{}) map[string]map[string]interface{} {
	props, _ := schema["properties"].(map[string]interface{})
	out := make(map[string]map[string]interface{}, len(props))
	for name, raw := range props {
		if sub, ok := raw.(map[string]interface{}); ok {
			out[name] = sub
		}
	}
	return out
}

func schemaPropertyType(prop map[string]interface{}) string {
	typ, _ := prop["type"].(string)
	return typ
}

func arrayItemSchema(prop map[string]interface{}) map[string]interface{} {
	items, _ := prop["items"].(map[string]interface{})
	return items
}

func jsonKindOf(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return ""
	}
}

func structFieldTypes(t *testing.T, sample interface{}) map[string]string {
	t.Helper()
	rt := reflect.TypeOf(sample)
	for rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		t.Fatalf("structFieldTypes: %v is not a struct", rt)
	}
	out := make(map[string]string, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		out[name] = jsonKindOf(f.Type)
	}
	return out
}

func assertSchemaStructParity(t *testing.T, name string, schema map[string]interface{}, sample interface{}) {
	t.Helper()
	schemaProps := schemaProperties(schema)
	required := schemaRequired(schema)
	reqSet := make(map[string]bool, len(required))
	for _, r := range required {
		reqSet[r] = true
	}
	structFields := structFieldTypes(t, sample)

	for propName, prop := range schemaProps {
		if _, ok := structFields[propName]; !ok {
			t.Errorf("%s: schema property %q has no matching struct field", name, propName)
			continue
		}
		if !reqSet[propName] {
			t.Errorf("%s: schema property %q not in required", name, propName)
		}
		wantType := schemaPropertyType(prop)
		gotType := structFields[propName]
		if wantType == "" || gotType == "" {
			continue
		}
		if wantType != gotType {
			t.Errorf("%s.%s: schema type %q, struct decodes as %q", name, propName, wantType, gotType)
		}
	}
	for fieldName := range structFields {
		if _, ok := schemaProps[fieldName]; !ok {
			t.Errorf("%s: struct field %q has no matching schema property", name, fieldName)
		}
	}
}

func TestSchemaStructParity_AgentReflection(t *testing.T) {
	schema := agentReflectionSchemaKey.Map()
	assertSchemaStructParity(t, "agent_reflection", schema, domain.ReflectionOutput{})
	props := schemaProperties(schema)
	assertSchemaStructParity(t, "agent_reflection.skills[]", arrayItemSchema(props["skills"]), domain.ReflectionSkillChange{})
	assertSchemaStructParity(t, "agent_reflection.rules[]", arrayItemSchema(props["rules"]), domain.ReflectionRuleChange{})
	assertSchemaStructParity(t, "agent_reflection.memories[]", arrayItemSchema(props["memories"]), domain.ReflectionMemoryChange{})
	assertSchemaStructParity(t, "agent_reflection.reverts[]", arrayItemSchema(props["reverts"]), domain.ReflectionRevert{})
}

func TestSchemaStructParity_ProbeScores(t *testing.T) {
	schema := probeScoresSchemaKey.Map()
	assertSchemaStructParity(t, "probe_scores", schema, probeScores{})
	props := schemaProperties(schema)
	assertSchemaStructParity(t, "probe_scores.scores[]", arrayItemSchema(props["scores"]), probeScore{})
}

func TestSchemaStructParity_EvolutionLesson(t *testing.T) {
	assertSchemaStructParity(t, "evolution_lesson", evolutionLessonSchemaKey.Map(), lessonOutput{})
}

func TestSchemaStructParity_MemoryPromotion(t *testing.T) {
	schema := memoryPromotionSchemaKey.Map()
	assertSchemaStructParity(t, "memory_promotion", schema, promotionOutput{})
	props := schemaProperties(schema)
	assertSchemaStructParity(t, "memory_promotion.promotions[]", arrayItemSchema(props["promotions"]), promotionItem{})
}

func TestSchemaStructParity_MemoryOrSkill(t *testing.T) {
	assertSchemaStructParity(t, "memory_or_skill", memoryOrSkillSchemaKey.Map(), saveClassification{})
}
