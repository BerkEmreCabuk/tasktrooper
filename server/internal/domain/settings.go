package domain

type AppSettings struct {
	WorkspaceRoot            string `json:"workspace_root"`
	DefaultLanguage          string `json:"default_language"`
	PipelineContainerRuntime string `json:"pipeline_container_runtime"`
	BoilerplateCatalogRepo   string `json:"boilerplate_catalog_repo"`
}

type UpdateSettingsRequest struct {
	WorkspaceRoot            string `json:"workspace_root"`
	DefaultLanguage          string `json:"default_language"`
	PipelineContainerRuntime string `json:"pipeline_container_runtime"`
	BoilerplateCatalogRepo   string `json:"boilerplate_catalog_repo"`
}
