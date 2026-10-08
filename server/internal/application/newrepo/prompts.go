package newrepo

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

type bootstrapItem struct {
	Number    int
	Kind      string // "skeleton", "doc" or "claude"
	Path      string
	KindLabel string
}

type bootstrapDoc struct {
	Path         string
	Instructions string
}

type bootstrapDesign struct {
	ProjectName string
	Version     int
}

type bootstrapDescriptionInput struct {
	Name        string
	Role        string
	Description string
	Stack       string
	Notes       string
	Scaffold    bool
	HasDocs     bool
	Items       []bootstrapItem
	Docs        []bootstrapDoc
	Design      *bootstrapDesign
}

var bootstrapDescriptionKey = prompt.Define[bootstrapDescriptionInput]("briefs.newrepo.bootstrap_description", bootstrapDescriptionInput{
	Name: "payments-api",
	Role: "backend",
	Items: []bootstrapItem{
		{Number: 1, Kind: "claude"},
	},
})
