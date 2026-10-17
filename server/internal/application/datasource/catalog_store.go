package datasource

import (
	"fmt"
	"maps"
	"slices"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func checkStoreApp(a domain.RecipeAppliesTo, declared map[string]bool) []string {
	var problems []string
	switch a.StorePlatform {
	case "", domain.MobileStorePlatformIOS, domain.MobileStorePlatformAndroid:
	default:
		problems = append(problems, fmt.Sprintf("applies_to.store_platform %q is not ios or android", a.StorePlatform))
	}
	if len(a.StoreParams) > 0 && a.StorePlatform == "" {
		problems = append(problems, "applies_to.store_params needs a store_platform")
	}
	for _, param := range slices.Sorted(maps.Keys(a.StoreParams)) {
		if !declared[param] {
			problems = append(problems, fmt.Sprintf("applies_to.store_params names %q, which is not a declared param", param))
		}
		if field := a.StoreParams[param]; !domain.ValidStoreAppField(field) {
			problems = append(problems, fmt.Sprintf("applies_to.store_params.%s: %q is not identifier or store_app_id", param, field))
		}
	}
	return problems
}
