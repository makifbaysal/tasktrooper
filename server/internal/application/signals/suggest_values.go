package signals

import (
	"context"
	"maps"
	"slices"
	"sort"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// paramSetsFromValues is the label probe of a kind that lists one label's
// values (a Loki label, an Elasticsearch field, a Tempo tag) rather than
// answering a PromQL count: the values of each param's label that best name
// the component. With one such param it offers up to maxParamSetsPerRecipe
// values at the best score; with several (whose values it cannot pair up),
// one set of each param's best. A param compared to the OpenTelemetry
// deployment.environment is matched against the names of env instead of the
// component's, and only fills in a set something else named. False when a
// listing failed.
func paramSetsFromValues(ctx context.Context, rd *reader, ds domain.DataSource, labels map[string]string, names []string, env domain.DeployEnvironment) ([]map[string]string, bool) {
	none := []map[string]string{{}}
	best := map[string][]string{}
	named := false
	envParams := map[string]bool{}
	for _, param := range slices.Sorted(maps.Keys(labels)) {
		isEnv := labels[param] == domain.TelemetryLabelEnvironment
		want := names
		if isEnv {
			want = domain.TelemetryEnvironmentNames(env)
			envParams[param] = true
		}
		values, err := rd.batch.LabelValues(ctx, ds.ID, labels[param])
		if err != nil {
			log.Debug().Err(err).Str("data_source", ds.ID.String()).Str("label", labels[param]).Msg("signals: listing label values failed; the suggestion needs its params")
			return none, false
		}
		top := 0
		var picked []string
		for _, v := range values {
			switch score := matchScore(v, want); {
			case score == 0 || score < top:
			case score > top:
				top, picked = score, []string{v}
			default:
				picked = append(picked, v)
			}
		}
		if top > 0 {
			sort.Strings(picked)
			best[param] = slices.Compact(picked)
			named = named || !isEnv
		}
	}
	switch {
	case !named:
		return none, true
	case len(labels)-len(envParams) == 1:
		var out []map[string]string
		for param, values := range best {
			if envParams[param] {
				continue
			}
			for _, v := range values[:min(len(values), maxParamSetsPerRecipe)] {
				set := map[string]string{param: v}
				for p := range envParams {
					if vs := best[p]; len(vs) > 0 {
						set[p] = vs[0]
					}
				}
				out = append(out, set)
			}
		}
		return out, true
	}
	set := map[string]string{}
	for param, values := range best {
		set[param] = values[0]
	}
	return []map[string]string{set}, true
}
