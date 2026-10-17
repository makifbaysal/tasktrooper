package opsai

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

type jobPromptInput struct {
	Repository    string
	Component     string
	Path          string
	Role          string
	Environment   string
	Components    []string
	FromCode      bool
	UnboundGolden []string
	Unbound       []string
}

var jobSample = jobPromptInput{
	Repository: "acme-shop", Component: "api", Path: "services/api", Role: "backend", Environment: "production",
	Components:    []string{"services/api (backend)", "web (frontend)"},
	UnboundGolden: []string{"http.error_ratio", "http.latency"}, Unbound: []string{"queue.backlog"},
}

var (
	defineErrorsKey = prompt.Define("observability.define_errors", jobSample)
	mapSignalsKey   = prompt.Define("observability.map_signals", jobSample)
)

type jobTitleInput struct {
	Kind        string
	Scope       string
	Environment string
}

var jobTitleKey = prompt.Define("observability.job_title", jobTitleInput{
	Kind: "define_errors", Scope: "acme-shop · api", Environment: "production",
})

type enableBriefInput struct {
	Repository      string
	Component       string
	Path            string
	Role            string
	Environment     string
	Provider        string
	Stack           string
	Goals           []string
	Target          string
	OTLPEndpoint    string
	Bound           []string
	Unbound         []string
	Instrumentation []string
}

var enableBriefKey = prompt.Define("observability.enable_brief", enableBriefInput{
	Repository: "acme-shop", Component: "api", Path: "services/api", Role: "backend", Environment: "production",
	Provider: "gcp", Stack: "Go 1.23, chi", Goals: []string{"metrics", "traces", "logs"}, Target: "own_cloud",
	Bound: []string{"http.throughput"}, Unbound: []string{"http.error_ratio", "http.latency"},
	Instrumentation: []string{"prometheus"},
})

type enableTitleInput struct {
	Component   string
	Environment string
}

var enableTitleKey = prompt.Define("observability.enable_task_title", enableTitleInput{Component: "api", Environment: "production"})
