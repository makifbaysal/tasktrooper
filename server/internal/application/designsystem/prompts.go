package designsystem

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

type briefRepository struct {
	Name         string
	ID           string
	Kind         string
	RootPath     string
	LayerVersion int
}

type projectTaskInput struct {
	ProjectName        string
	ProjectID          string
	ProjectDescription string
	Repositories       []briefRepository
	CurrentVersion     int
	Notes              string
}

var projectTaskKey = prompt.Define("briefs.designsystem.project_task", projectTaskInput{
	ProjectName: "Shop", ProjectID: "p", Repositories: []briefRepository{{Name: "web", ID: "r", Kind: "frontend"}},
})

type repositoryTaskInput struct {
	RepositoryName string
	RepositoryID   string
	RepositoryKind string
	ProjectName    string
	BaseVersion    int
	LayerVersion   int
	Ambiguous      bool
	Notes          string
}

var repositoryTaskKey = prompt.Define("briefs.designsystem.repository_task", repositoryTaskInput{
	RepositoryName: "web", RepositoryID: "r", RepositoryKind: "frontend", ProjectName: "Shop", BaseVersion: 1,
})

type contextNoteInput struct {
	ProjectName    string
	BaseVersion    string
	LayerVersion   string
	LayerRationale string
	Excerpt        string
	Truncated      bool
	Ambiguous      bool
	ShowTool       bool
}

var contextNoteKey = prompt.Define("designsystem.context_note", contextNoteInput{
	ProjectName: "Shop", BaseVersion: "v1", Excerpt: "## Overview", ShowTool: true,
})

type fileInput struct {
	ProjectName    string
	BaseVersion    int
	RepositoryName string
	LayerVersion   int
	BaseDesignMD   string
	LayerRationale string
	LayerDesignMD  string
	BaseInventory  string
	LayerInventory string
}

var designMDFileKey = prompt.Define("designsystem.design_md_file", fileInput{ProjectName: "Shop", BaseVersion: 1, BaseDesignMD: "## Overview"})

var inventoryFileKey = prompt.Define("designsystem.inventory_file", fileInput{ProjectName: "Shop", BaseVersion: 1, BaseInventory: "- Button"})
