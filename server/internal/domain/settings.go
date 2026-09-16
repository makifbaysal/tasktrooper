package domain

import "errors"

// ErrMissingAnalizTools marks the save-time refusal of an analiz-assignment
// change whose target agent is missing a tool RequiredAnalizTools names, so
// the transport can answer 422 with the missing list instead of 500.
var ErrMissingAnalizTools = errors.New("agent is missing required analiz tools")

// MissingAnalizToolsError carries the per-area missing-tool lists behind
// ErrMissingAnalizTools, so a caller that only needs the sentinel can use
// errors.Is and one that needs the detail can errors.As into this type.
type MissingAnalizToolsError struct {
	Missing map[string][]string
}

func (e *MissingAnalizToolsError) Error() string { return ErrMissingAnalizTools.Error() }
func (e *MissingAnalizToolsError) Unwrap() error { return ErrMissingAnalizTools }

type AppSettings struct {
	WorkspaceRoot            string `json:"workspace_root"`
	DefaultLanguage          string `json:"default_language"`
	PipelineContainerRuntime string `json:"pipeline_container_runtime"`
	BoilerplateCatalogRepo   string `json:"boilerplate_catalog_repo"`
	// AnalizAssigneeBackend/Frontend/Mobile name the agent an analiz task for
	// that area is auto-assigned to on creation (CreateTask). An agent name
	// (e.g. "system-architect", "backend-developer"), not a UUID, matching the
	// convention create_board_task's own assignee argument already uses.
	// Empty means "system-architect", the same default SettingsStore.Get
	// falls back to when the app_settings rows do not exist yet.
	AnalizAssigneeBackend  string `json:"analiz_assignee_backend"`
	AnalizAssigneeFrontend string `json:"analiz_assignee_frontend"`
	AnalizAssigneeMobile   string `json:"analiz_assignee_mobile"`
}

type UpdateSettingsRequest struct {
	WorkspaceRoot            string `json:"workspace_root"`
	DefaultLanguage          string `json:"default_language"`
	PipelineContainerRuntime string `json:"pipeline_container_runtime"`
	BoilerplateCatalogRepo   string `json:"boilerplate_catalog_repo"`
}

// UpdateAnalizAssignmentRequest changes which agent an analiz task for a given
// area is auto-assigned to. Backend/Frontend/Mobile accept an agent name, "-"
// to reset to the default, or "" to leave that area unchanged (the same
// leave-alone/reset contract UpdateSettingsRequest already uses for
// PipelineContainerRuntime/BoilerplateCatalogRepo). ConfirmGrantTools is the
// user's answer to "this agent is missing tools the analiz workflow needs —
// add them?"; without it the save is refused when any changed area's agent is
// missing a required tool.
type UpdateAnalizAssignmentRequest struct {
	Backend           string `json:"backend"`
	Frontend          string `json:"frontend"`
	Mobile            string `json:"mobile"`
	ConfirmGrantTools bool   `json:"confirm_grant_tools"`
}

// AnalizAssignmentResult is the response to PUT /v1/settings/analiz-assignment.
// Saved is false only when MissingTools is non-empty: the request changed an
// area to an agent missing required tools and ConfirmGrantTools was not set,
// so nothing was written.
type AnalizAssignmentResult struct {
	Saved        bool                `json:"saved"`
	Settings     *AppSettings        `json:"settings,omitempty"`
	MissingTools map[string][]string `json:"missing_tools,omitempty"`
	GrantedTools map[string][]string `json:"granted_tools,omitempty"`
	Hint         string              `json:"hint,omitempty"`
}
