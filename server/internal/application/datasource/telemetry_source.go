package datasource

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// FieldDuplicate, on key "kind": the kind allows one source and one exists.
const FieldDuplicate = "duplicate"

// TelemetrySetter must be implemented by the tasktrooper_telemetry kind: the
// service hands it the connection saved in Settings, which it reads through.
type TelemetrySetter interface {
	SetTelemetry(port.TelemetryReader)
}

// TelemetryConnection is the one TaskTrooper Telemetry connection
// (application/telemetry.Service): what the kind reads through, and whether
// both its URL and key are saved.
type TelemetryConnection interface {
	port.TelemetryReader
	Configured(ctx context.Context) (bool, error)
}

// SetTelemetry wires the telemetry connection into the tasktrooper_telemetry
// kind and turns on its managed source (SyncTelemetrySource); marks may be
// nil, and then a deleted source comes back on the next sync. Call it only
// before serving.
func (s *Service) SetTelemetry(conn TelemetryConnection, marks port.TelemetrySourceMarkStore) {
	s.telemetry, s.telemetryMarks = conn, marks
	for _, k := range s.kinds {
		if setter, ok := k.(TelemetrySetter); ok {
			setter.SetTelemetry(conn)
		}
	}
}

func isSingleton(kind domain.DataSourceKind) bool {
	return kind == domain.DataSourceKindTaskTrooperTelemetry
}

// SyncTelemetrySource makes the telemetry data source once the connection in
// Settings is saved, so it needs no "add data source" step and no second key:
// the source holds nothing, and reads through that connection. One a person
// deleted is not made again (they can add it back by hand), and an existing
// one is verified again, since the connection it reads may have moved. Safe
// to call as often as wanted.
func (s *Service) SyncTelemetrySource(ctx context.Context) {
	kind := domain.DataSourceKindTaskTrooperTelemetry
	if s.telemetry == nil {
		return
	}
	if _, ok := s.kinds[kind]; !ok {
		return
	}
	configured, err := s.telemetry.Configured(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("data sources: reading the telemetry connection failed")
		return
	}
	s.provisionMu.Lock()
	existing, err := s.sourcesOf(ctx, kind)
	if err != nil {
		s.provisionMu.Unlock()
		log.Warn().Err(err).Msg("data sources: listing sources failed; the telemetry source was not synced")
		return
	}
	if len(existing) > 0 {
		s.provisionMu.Unlock()
		for _, id := range existing {
			s.changed(id)
			if _, err := s.Verify(ctx, id); err != nil {
				log.Debug().Err(err).Msg("data sources: verifying the telemetry source failed")
			}
		}
		return
	}
	defer s.provisionMu.Unlock()
	if !configured {
		return
	}
	if s.telemetryMarks != nil {
		deleted, err := s.telemetryMarks.TelemetrySourceDeleted(ctx)
		if err != nil || deleted {
			if err != nil {
				log.Warn().Err(err).Msg("data sources: reading the telemetry source mark failed")
			}
			return
		}
	}
	if _, err := s.create(ctx, domain.SaveDataSourceRequest{Kind: kind}); err != nil {
		log.Warn().Err(err).Msg("data sources: making the telemetry source failed")
	}
}

func (s *Service) sourcesOf(ctx context.Context, kind domain.DataSourceKind) ([]uuid.UUID, error) {
	list, err := s.store.ListDataSources(ctx)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, ds := range list {
		if ds.Kind == kind {
			out = append(out, ds.ID)
		}
	}
	return out, nil
}

func (s *Service) markTelemetrySource(ctx context.Context, deleted bool) {
	if s.telemetryMarks == nil {
		return
	}
	if err := s.telemetryMarks.SetTelemetrySourceDeleted(context.WithoutCancel(ctx), deleted); err != nil {
		log.Warn().Err(err).Msg("data sources: recording the telemetry source mark failed")
	}
}
