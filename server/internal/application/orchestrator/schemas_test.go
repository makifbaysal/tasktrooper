package orchestrator

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Checks what strict json_schema mode enforces at the provider boundary: every object
// sets additionalProperties=false and every declared property is required.
func assertStrictObjectSchema(t *testing.T, schema map[string]interface{}, path string) {
	t.Helper()
	typ, _ := schema["type"].(string)
	switch typ {
	case "object":
		addl, ok := schema["additionalProperties"].(bool)
		if !ok || addl != false {
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
			t.Errorf("%s: required has %d entries, properties has %d — every property must be required in strict mode", path, len(required), len(props))
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
// encoding/json carries it as []interface{} of strings — both are valid
// depending on which side of the migration a test runs against.
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

func TestPipelineSchemas_MarshalAndSatisfyStrictMode(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"planner":   plannerOutputSchemaKey.Map(),
		"replanner": replannerOutputSchemaKey.Map(),
		"intake":    intakeOutputSchemaKey.Map(),
		"verifier":  verifierOutputSchemaKey.Map(),
	}
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(schema)
			require.NoError(t, err, "schema must marshal to valid JSON")
			assert.NotEmpty(t, raw)

			var roundTrip map[string]interface{}
			require.NoError(t, json.Unmarshal(raw, &roundTrip))
			assert.Equal(t, "object", roundTrip["type"])

			assertStrictObjectSchema(t, schema, name)
		})
	}
}

// The planner's shape promises difficulty; the replanner's prompt never mentions it.
func TestPlannerTaskSchema_HasDifficultyReplannerTaskSchemaDoesNot(t *testing.T) {
	plannerProps := plannerOutputSchemaKey.Map()["properties"].(map[string]interface{})
	plannerTask := plannerProps["tasks"].(map[string]interface{})["items"].(map[string]interface{})
	plannerTaskProps := plannerTask["properties"].(map[string]interface{})
	assert.Contains(t, plannerTaskProps, "difficulty")

	replannerProps := replannerOutputSchemaKey.Map()["properties"].(map[string]interface{})
	repairTask := replannerProps["tasks"].(map[string]interface{})["items"].(map[string]interface{})
	repairTaskProps := repairTask["properties"].(map[string]interface{})
	assert.NotContains(t, repairTaskProps, "difficulty")
}

// Both schemas embed the ask_user question shape; keep them on prompt.AskUserQuestionJSONShape.
func TestClarificationQuestionSchema_SharedByPlannerAndIntake(t *testing.T) {
	plannerProps := plannerOutputSchemaKey.Map()["properties"].(map[string]interface{})
	plannerQ := plannerProps["questions"].(map[string]interface{})["items"].(map[string]interface{})

	intakeProps := intakeOutputSchemaKey.Map()["properties"].(map[string]interface{})
	intakeQ := intakeProps["questions"].(map[string]interface{})["items"].(map[string]interface{})

	plannerQJSON, err := json.Marshal(plannerQ)
	require.NoError(t, err)
	intakeQJSON, err := json.Marshal(intakeQ)
	require.NoError(t, err)
	assert.JSONEq(t, string(plannerQJSON), string(intakeQJSON))
}

// ---- Schema/struct parity ----
//
// A strict-mode schema is a promise about the wire shape a provider is
// constrained to; these tests check that promise against the Go struct the
// response is actually decoded into (reflecting over its json tags), so the
// two cannot drift silently. json.RawMessage fields (tool_names, summary)
// decode dynamically by design — see this package's WP9a report — and are
// checked for presence only, not for a specific declared type.

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

// arrayItemSchema unwraps an array property's {"type":"array","items":{...}}
// down to the item object schema, for descending into a nested list.
func arrayItemSchema(prop map[string]interface{}) map[string]interface{} {
	items, _ := prop["items"].(map[string]interface{})
	return items
}

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

func jsonKindOf(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == rawMessageType {
		return "" // dynamic — presence-only, see the looseness note above
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

// structFieldTypes reflects sample's json tags into name -> declared JSON
// Schema "type" ("" for a dynamic/json.RawMessage field).
func structFieldTypes(t *testing.T, sample interface{}) map[string]string {
	t.Helper()
	rt := reflect.TypeOf(sample)
	for rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	require.Equal(t, reflect.Struct, rt.Kind())
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

// assertSchemaStructParity checks the schema's property/required set against
// the wire struct's json-tagged fields, in both directions. allowExtraFields
// names struct fields the schema deliberately does not declare — a known,
// reported looseness (e.g. plannerOutputWire.Purpose/Goal, which the model
// is never asked for and which Generate always overwrites after parsing;
// see this package's WP9a report) — rather than a silent gap.
func assertSchemaStructParity(t *testing.T, name string, schema map[string]interface{}, sample interface{}, allowExtraFields ...string) {
	t.Helper()
	allowExtra := make(map[string]bool, len(allowExtraFields))
	for _, f := range allowExtraFields {
		allowExtra[f] = true
	}
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
			continue // one side is dynamic (json.RawMessage) — presence already checked
		}
		if wantType != gotType {
			t.Errorf("%s.%s: schema type %q, struct decodes as %q", name, propName, wantType, gotType)
		}
	}
	for fieldName := range structFields {
		if _, ok := schemaProps[fieldName]; !ok && !allowExtra[fieldName] {
			t.Errorf("%s: struct field %q has no matching schema property", name, fieldName)
		}
	}
}

func TestSchemaStructParity_GoalIntake(t *testing.T) {
	assertSchemaStructParity(t, "goal_intake", intakeOutputSchemaKey.Map(), intakeWireOutput{})
	props := schemaProperties(intakeOutputSchemaKey.Map())
	questionSchema := arrayItemSchema(props["questions"])
	assertSchemaStructParity(t, "goal_intake.questions[]", questionSchema, domain.ClarificationQuestion{}, "max_selections")
	qProps := schemaProperties(questionSchema)
	assertSchemaStructParity(t, "goal_intake.questions[].options[]", arrayItemSchema(qProps["options"]), domain.ClarificationOption{})
}

func TestSchemaStructParity_VerificationResult(t *testing.T) {
	assertSchemaStructParity(t, "verification_result", verifierOutputSchemaKey.Map(), verifierWireOutput{})
}

func TestSchemaStructParity_PlannerOutput(t *testing.T) {
	assertSchemaStructParity(t, "planner_output", plannerOutputSchemaKey.Map(), plannerOutputWire{}, "purpose", "goal")
	props := schemaProperties(plannerOutputSchemaKey.Map())
	assertSchemaStructParity(t, "planner_output.tasks[]", arrayItemSchema(props["tasks"]), plannerTaskWire{})
	assertSchemaStructParity(t, "planner_output.questions[]", arrayItemSchema(props["questions"]), domain.ClarificationQuestion{}, "max_selections")
}

// replan_output shares plannerTaskWire with planner_output, but its schema
// deliberately omits "difficulty" (see this package's WP9a looseness note):
// the wire struct still carries that field, decoding it to its zero value.
// The parity check here is one-directional (schema properties <= struct
// fields) rather than the two-way check above, to allow that one known gap.
func TestSchemaStructParity_ReplanOutput(t *testing.T) {
	schema := replannerOutputSchemaKey.Map()
	props := schemaProperties(schema)
	taskSchema := arrayItemSchema(props["tasks"])

	structFields := structFieldTypes(t, plannerTaskWire{})
	taskProps := schemaProperties(taskSchema)
	required := schemaRequired(taskSchema)
	reqSet := make(map[string]bool, len(required))
	for _, r := range required {
		reqSet[r] = true
	}
	for propName, prop := range taskProps {
		if _, ok := structFields[propName]; !ok {
			t.Errorf("replan_output.tasks[]: schema property %q has no matching struct field", propName)
			continue
		}
		if !reqSet[propName] {
			t.Errorf("replan_output.tasks[]: schema property %q not in required", propName)
		}
		wantType := schemaPropertyType(prop)
		gotType := structFields[propName]
		if wantType != "" && gotType != "" && wantType != gotType {
			t.Errorf("replan_output.tasks[].%s: schema type %q, struct decodes as %q", propName, wantType, gotType)
		}
	}
	extra := map[string]bool{}
	for f := range structFields {
		if _, ok := taskProps[f]; !ok {
			extra[f] = true
		}
	}
	assert.Equal(t, map[string]bool{"difficulty": true}, extra, "replan_output.tasks[]: unexpected struct/schema drift beyond the known difficulty gap")
}
