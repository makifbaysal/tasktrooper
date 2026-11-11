package domain

type AppSettings struct {
	WorkspaceRoot            string `json:"workspace_root"`
	DefaultLanguage          string `json:"default_language"`
	PipelineContainerRuntime string `json:"pipeline_container_runtime"`
	BoilerplateCatalogRepo   string `json:"boilerplate_catalog_repo"`
	// MaxConcurrentAgents is how many agent runs may execute at once from this
	// board runner; 0 means unlimited. Read per run, so a change takes effect
	// on the next dispatch without a restart.
	MaxConcurrentAgents int `json:"max_concurrent_agents"`
	// MaxConcurrentTasks is how many distinct tasks may hold a live run at once
	// from this board runner; 0 means unlimited.
	MaxConcurrentTasks int `json:"max_concurrent_tasks"`
}

type UpdateSettingsRequest struct {
	WorkspaceRoot            string `json:"workspace_root"`
	DefaultLanguage          string `json:"default_language"`
	PipelineContainerRuntime string `json:"pipeline_container_runtime"`
	BoilerplateCatalogRepo   string `json:"boilerplate_catalog_repo"`
	// The two limits are options so an unset key leaves the stored value alone
	// (0 is a real, wanted value: unlimited).
	MaxConcurrentAgents *int `json:"max_concurrent_agents"`
	MaxConcurrentTasks  *int `json:"max_concurrent_tasks"`
}
