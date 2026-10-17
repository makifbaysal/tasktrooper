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
// one set of each param's best. False when a listing failed.
func paramSetsFromValues(ctx context.Context, rd *reader, ds domain.DataSource, labels map[string]string, names []string) ([]map[string]string, bool) {
	none := []map[string]string{{}}
	best := map[string][]string{}
	for _, param := range slices.Sorted(maps.Keys(labels)) {
		values, err := rd.batch.LabelValues(ctx, ds.ID, labels[param])
		if err != nil {
			log.Debug().Err(err).Str("data_source", ds.ID.String()).Str("label", labels[param]).Msg("signals: listing label values failed; the suggestion needs its params")
			return none, false
		}
		top := 0
		var picked []string
		for _, v := range values {
			switch score := matchScore(v, names); {
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
		}
	}
	switch {
	case len(best) == 0:
		return none, true
	case len(labels) == 1:
		var out []map[string]string
		for param, values := range best {
			for _, v := range values[:min(len(values), maxParamSetsPerRecipe)] {
				out = append(out, map[string]string{param: v})
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
