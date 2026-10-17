package domain

// DataSourceOffer is a data source the detector would read a component's
// signals through, had a source of Kind existed: creating one with Config
// (POST /v1/data-sources {kind, config}) makes the next detection offer
// RecipeID for Signals with Params. StoreCredential names the workspace
// store credential the source reads, when its kind reads one; that
// credential must be saved first.
type DataSourceOffer struct {
	Kind            DataSourceKind    `json:"kind"`
	Config          map[string]string `json:"config"`
	RecipeID        string            `json:"recipe_id"`
	Params          map[string]string `json:"params"`
	Signals         []string          `json:"signals"`
	StoreCredential string            `json:"store_credential,omitempty"`
}
