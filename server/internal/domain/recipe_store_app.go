package domain

// The per-entity tables a mobile view reads: RecipeTableCrashes one row per
// crash or error issue, RecipeTableSignatures one row per diagnostic
// signature (a hang, a slow launch, a disk write hot spot).
const (
	RecipeTableCrashes    = "crashes"
	RecipeTableSignatures = "signatures"
)

// StoreAppValue reads the field of a linked store app a recipe's
// store_params names: "identifier" (the bundle id or package name) or
// "store_app_id" (App Store Connect's app id; empty for Google Play).
func StoreAppValue(app MobileStoreApp, field string) (string, bool) {
	switch field {
	case "identifier":
		return app.Identifier, true
	case "store_app_id":
		return app.StoreAppID, true
	}
	return "", false
}

func ValidStoreAppField(field string) bool {
	_, ok := StoreAppValue(MobileStoreApp{}, field)
	return ok
}

// ParamsFromStoreApp fills the params StoreParams maps from app, leaving out
// a field the app has no value for.
func (r Recipe) ParamsFromStoreApp(app MobileStoreApp) map[string]string {
	out := map[string]string{}
	for param, field := range r.AppliesTo.StoreParams {
		if v, ok := StoreAppValue(app, field); ok && v != "" {
			out[param] = v
		}
	}
	return out
}
