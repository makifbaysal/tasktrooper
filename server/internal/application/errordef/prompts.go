package errordef

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

type errorTaskInput struct {
	// Fence quotes Message and Sample: longer than any backtick run in them.
	Fence       string
	Definition  string
	Category    string
	Severity    string
	Rationale   string
	Component   string
	Path        string
	Provider    string
	Fingerprint string
	Count       int
	FirstSeen   string
	LastSeen    string
	Message     string
	Sample      string
	ExternalURL string
}

var errorTaskKey = prompt.Define("observability.error_task", errorTaskInput{
	Fence: "```", Definition: "Payment provider timeout", Category: "dependency", Severity: "high",
	Rationale: "The checkout calls the provider synchronously.", Component: "api", Path: "services/api",
	Provider: "gcp", Fingerprint: "a1b2c3", Count: 42, FirstSeen: "2026-10-16T10:00:00Z",
	LastSeen: "2026-10-17T09:00:00Z", Message: "context deadline exceeded calling POST /v1/charges",
	Sample: "goroutine 1 [running]:\nmain.charge()", ExternalURL: "https://console.cloud.google.com/errors/a1b2c3",
})

type errorTaskTitleInput struct {
	Definition string
	Message    string
}

var errorTaskTitleKey = prompt.Define("observability.error_task_title", errorTaskTitleInput{
	Definition: "Payment provider timeout", Message: "context deadline exceeded calling POST /v1/charges",
})
