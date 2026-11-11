package readiness

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

type componentBrief struct {
	Path     string
	Role     string
	Stack    string
	Commands []string
}

type guideInput struct {
	Repository string
	Kind       string
	Missing    []string
	Components []componentBrief
}

var guideKey = prompt.Define("readiness.guide", guideInput{
	Repository: "acme", Kind: "backend", Missing: []string{"readme", "agent_guide"},
	Components: []componentBrief{{Path: ".", Role: "backend", Stack: "Go", Commands: []string{"test: go test ./..."}}},
})

type testGap struct {
	Path        string
	Role        string
	Stack       string
	TestCommand string
}

type testsInput struct {
	Repository string
	Components []testGap
}

var testsKey = prompt.Define("readiness.tests", testsInput{
	Repository: "acme", Components: []testGap{{Path: ".", Role: "backend", Stack: "Go"}},
})
