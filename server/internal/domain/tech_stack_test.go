package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSkillsForStacks(t *testing.T) {
	mk := func(n string) TechStack { return TechStack{ID: uuid.New(), Name: n} }
	goS, java, dotnet, node, rust, pg := mk("Go"), mk("Java"), mk(".NET"), mk("TypeScript (Node.js)"), mk("Rust"), mk("PostgreSQL")
	stacks := []TechStack{goS, java, dotnet, node, rust, pg}
	skillIn := func(name string, st *TechStack) Skill {
		sk := Skill{ID: uuid.New(), Name: name}
		if st != nil {
			id := st.ID
			sk.TechStackID = &id
		}
		return sk
	}
	skills := []Skill{skillIn("general", nil), skillIn("go", &goS), skillIn("java", &java), skillIn("dn", &dotnet), skillIn("node", &node), skillIn("rust", &rust), skillIn("pg", &pg)}
	names := func(in []Skill) []string {
		var o []string
		for _, s := range in {
			o = append(o, s.Name)
		}
		return o
	}

	goOnly := []ComponentStack{{Languages: []StackItem{{Name: "Go"}}}}
	assert.Equal(t, []string{"general", "go", "pg"}, names(SkillsForStacks(skills, stacks, goOnly)))

	mixed := []ComponentStack{{Languages: []StackItem{{Name: "C#"}}}, {Languages: []StackItem{{Name: "JavaScript"}}}}
	assert.Equal(t, []string{"general", "dn", "node", "pg"}, names(SkillsForStacks(skills, stacks, mixed)))

	assert.Len(t, SkillsForStacks(skills, stacks, nil), len(skills), "nothing detected keeps everything")
	assert.Len(t, SkillsForStacks(skills, stacks, []ComponentStack{{}}), len(skills))
}
