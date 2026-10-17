package signals

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Links is where the detector learns which brokers a component talks to: the
// discovery scan links it to a queue resource for each messaging library its
// manifests name (server/internal/application/discovery/links/catalog.yaml).
type Links interface {
	ListLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error)
	ListResources(ctx context.Context, ids []uuid.UUID) ([]domain.SystemResource, error)
}

// messagingSystems are the vendors of the queue resources the component is
// linked to (kafka, rabbitmq, nats, pubsub, sqs, redis): the names a recipe's
// applies_to.messaging_system and a broker kind use. A dismissed or missing
// link does not count; a read that fails answers none, which detects as if
// nothing were known.
func (s *Service) messagingSystems(ctx context.Context, comp domain.Component) map[string]bool {
	out := map[string]bool{}
	if s.links == nil {
		return out
	}
	links, err := s.links.ListLinks(ctx, comp.RepositoryID)
	if err != nil {
		log.Debug().Err(err).Str("component", comp.ID.String()).Msg("signals: reading the component's links failed")
		return out
	}
	var ids []uuid.UUID
	for _, l := range links {
		if l.FromComponentID != comp.ID || l.ToResourceID == nil || l.Status == domain.LinkDismissed || l.Missing {
			continue
		}
		if !slices.Contains(ids, *l.ToResourceID) {
			ids = append(ids, *l.ToResourceID)
		}
	}
	if len(ids) == 0 {
		return out
	}
	resources, err := s.links.ListResources(ctx, ids)
	if err != nil {
		log.Debug().Err(err).Str("component", comp.ID.String()).Msg("signals: reading the component's resources failed")
		return out
	}
	for _, r := range resources {
		if r.Kind == domain.ResourceQueue && r.Vendor != "" {
			out[r.Vendor] = true
		}
	}
	return out
}

// brokerSystem: a broker kind reads its own broker, so the kind is the
// messaging system.
func brokerSystem(kind domain.DataSourceKind) string {
	switch kind {
	case domain.DataSourceKindKafka, domain.DataSourceKindRabbitMQ, domain.DataSourceKindNATS:
		return string(kind)
	}
	return ""
}

// systemOf is the messaging system a suggestion reads: its recipe's, or its
// broker kind's for a query of its own.
func systemOf(l lookup, b domain.SignalBinding) string {
	if r, ok := l.recipes[b.RecipeID]; ok && r.AppliesTo.MessagingSystem != "" {
		return r.AppliesTo.MessagingSystem
	}
	return brokerSystem(l.sources[b.DataSourceID].Kind)
}

// rankByBrokers moves the suggestions reading one of the component's brokers
// to the front, keeping the order otherwise, so the per-signal cap keeps them
// first.
func rankByBrokers(l lookup, found []domain.SignalBinding, systems map[string]bool) {
	if len(systems) == 0 {
		return
	}
	slices.SortStableFunc(found, func(a, b domain.SignalBinding) int {
		ma, mb := systems[systemOf(l, a)], systems[systemOf(l, b)]
		switch {
		case ma && !mb:
			return -1
		case mb && !ma:
			return 1
		}
		return 0
	})
}
