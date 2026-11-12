// Package audiencetest holds the fakes audience-research tests share: an
// in-memory store and a scripted model.
package audiencetest

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Store is a port.AudienceStore in memory.
type Store struct {
	mu        sync.Mutex
	audiences map[uuid.UUID]domain.Audience
	personas  map[uuid.UUID][]domain.Persona // by audience
	studies   map[uuid.UUID]domain.Study
	answers   map[uuid.UUID][]domain.StudyAnswer
	messages  []domain.ChatMessage
	clock     time.Time
	choice    domain.AudienceRuntimeChoice
}

var _ port.AudienceStore = (*Store)(nil)

func NewStore() *Store {
	return &Store{
		audiences: map[uuid.UUID]domain.Audience{}, personas: map[uuid.UUID][]domain.Persona{},
		studies: map[uuid.UUID]domain.Study{}, answers: map[uuid.UUID][]domain.StudyAnswer{},
		clock: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC),
	}
}

func (s *Store) tick() time.Time {
	s.clock = s.clock.Add(time.Second)
	return s.clock
}

func (s *Store) CreateAudience(_ context.Context, a domain.Audience) (domain.Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID = uuid.New()
	a.CreatedAt, a.UpdatedAt = s.tick(), s.clock
	if a.Status == "" {
		a.Status = domain.AudienceDraft
	}
	s.audiences[a.ID] = a
	return a, nil
}

func (s *Store) withCounts(a domain.Audience) domain.Audience {
	a.PersonaCount = len(s.personas[a.ID])
	for _, st := range s.studies {
		if st.AudienceID == a.ID {
			a.StudyCount++
		}
	}
	return a
}

func (s *Store) GetAudience(_ context.Context, id uuid.UUID) (domain.Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.audiences[id]
	if !ok {
		return domain.Audience{}, domain.ErrAudienceNotFound
	}
	return s.withCounts(a), nil
}

func (s *Store) ListAudiences(context.Context) ([]domain.Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.Audience{}
	for _, a := range s.audiences {
		out = append(out, s.withCounts(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) SaveAudience(_ context.Context, a domain.Audience) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.audiences[a.ID]; !ok {
		return domain.ErrAudienceNotFound
	}
	a.UpdatedAt = s.tick()
	s.audiences[a.ID] = a
	return nil
}

func (s *Store) DeleteAudience(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.audiences[id]; !ok {
		return domain.ErrAudienceNotFound
	}
	delete(s.audiences, id)
	delete(s.personas, id)
	for sid, st := range s.studies {
		if st.AudienceID == id {
			delete(s.studies, sid)
			delete(s.answers, sid)
		}
	}
	return nil
}

func (s *Store) ReplacePersonas(_ context.Context, audienceID uuid.UUID, personas []domain.Persona) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Persona, len(personas))
	for i, p := range personas {
		p.ID, p.AudienceID, p.Ordinal, p.CreatedAt = uuid.New(), audienceID, i+1, s.tick()
		out[i] = p
	}
	s.personas[audienceID] = out
	return nil
}

func (s *Store) ListPersonas(_ context.Context, audienceID uuid.UUID) ([]domain.Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.Persona{}, s.personas[audienceID]...), nil
}

func (s *Store) GetPersona(_ context.Context, id uuid.UUID) (domain.Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, list := range s.personas {
		for _, p := range list {
			if p.ID == id {
				return p, nil
			}
		}
	}
	return domain.Persona{}, domain.ErrPersonaNotFound
}

func (s *Store) CreateStudy(_ context.Context, st domain.Study) (domain.Study, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.ID, st.CreatedAt = uuid.New(), s.tick()
	if st.Status == "" {
		st.Status = domain.StudyDraft
	}
	if st.Kind == "" {
		st.Kind = domain.KindUserInterviews
	}
	if st.ScriptState == "" {
		st.ScriptState = domain.ScriptNone
	}
	if st.Questions == nil {
		st.Questions = []string{}
	}
	if st.PricePoints == nil {
		st.PricePoints = []float64{}
	}
	s.studies[st.ID] = st
	return st, nil
}

func (s *Store) GetStudy(_ context.Context, id uuid.UUID) (domain.Study, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.studies[id]
	if !ok {
		return domain.Study{}, domain.ErrStudyNotFound
	}
	return st, nil
}

func (s *Store) ListStudies(_ context.Context, audienceID *uuid.UUID) ([]domain.Study, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.Study{}
	for _, st := range s.studies {
		if audienceID == nil || st.AudienceID == *audienceID {
			st.AudienceName = s.audiences[st.AudienceID].Name
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) SaveStudy(_ context.Context, st domain.Study) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.studies[st.ID]; !ok {
		return domain.ErrStudyNotFound
	}
	s.studies[st.ID] = st
	return nil
}

func (s *Store) DeleteStudy(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.studies[id]; !ok {
		return domain.ErrStudyNotFound
	}
	delete(s.studies, id)
	delete(s.answers, id)
	return nil
}

func (s *Store) BumpStudyProgress(_ context.Context, id uuid.UUID, done, failed int) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.studies[id]
	if !ok {
		return 0, 0, domain.ErrStudyNotFound
	}
	st.PersonaDone += done
	st.PersonaFailed += failed
	s.studies[id] = st
	return st.PersonaDone, st.PersonaFailed, nil
}

func (s *Store) SaveAnswer(_ context.Context, a domain.StudyAnswer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID, a.CreatedAt = uuid.New(), s.tick()
	s.answers[a.StudyID] = append(s.answers[a.StudyID], a)
	return nil
}

func (s *Store) DeleteFailedAnswers(_ context.Context, studyID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := []domain.StudyAnswer{}
	n := 0
	for _, a := range s.answers[studyID] {
		if a.Status == "failed" {
			n++
			continue
		}
		kept = append(kept, a)
	}
	s.answers[studyID] = kept
	return n, nil
}

func (s *Store) ListAnswers(_ context.Context, studyID uuid.UUID) ([]domain.StudyAnswer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.StudyAnswer{}, s.answers[studyID]...), nil
}

func (s *Store) AddMessages(_ context.Context, msgs []domain.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range msgs {
		m.ID, m.CreatedAt = uuid.New(), s.tick()
		s.messages = append(s.messages, m)
	}
	return nil
}

func (s *Store) filterMessages(keep func(domain.ChatMessage) bool) []domain.ChatMessage {
	out := []domain.ChatMessage{}
	for _, m := range s.messages {
		if keep(m) {
			out = append(out, m)
		}
	}
	return out
}

func (s *Store) ListMessages(_ context.Context, personaID uuid.UUID) ([]domain.ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filterMessages(func(m domain.ChatMessage) bool { return m.PersonaID == personaID }), nil
}

func (s *Store) ListStudyMessages(_ context.Context, studyID uuid.UUID) ([]domain.ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filterMessages(func(m domain.ChatMessage) bool { return m.StudyID != nil && *m.StudyID == studyID }), nil
}

func (s *Store) ClearMessages(_ context.Context, personaID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = s.filterMessages(func(m domain.ChatMessage) bool { return m.PersonaID != personaID })
	return nil
}

func (s *Store) FailInterrupted(_ context.Context, reason string) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, st := 0, 0
	for id, x := range s.audiences {
		if x.Status == domain.AudienceBuilding {
			x.Status, x.Error = domain.AudienceFailed, reason
			s.audiences[id] = x
			a++
		}
	}
	for id, x := range s.studies {
		if x.Status == domain.StudyRunning || x.Status == domain.StudySynthesizing {
			x.Status, x.Error = domain.StudyFailed, reason
			s.studies[id] = x
			st++
		} else if x.ScriptState == domain.ScriptGenerating {
			x.ScriptState, x.Error = domain.ScriptFailed, reason
			s.studies[id] = x
		}
	}
	return a, st, nil
}

// ---- the model

// Model is a port.LLMClient that answers by what the prompt asks: a script of
// handlers tried in order, the first whose key is in the system prompt wins.
type Model struct {
	mu       sync.Mutex
	handlers []handler
	Calls    []domain.AgentRequest
	// Active and MaxActive observe how many calls ran at once.
	active, MaxActive int
	Delay             time.Duration
}

type handler struct {
	contains string
	answer   func(req domain.AgentRequest) (string, error)
}

var _ port.LLMClient = (*Model)(nil)

// On answers any call whose system prompt contains marker.
func (m *Model) On(marker string, answer func(req domain.AgentRequest) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler{contains: marker, answer: answer})
}

// OnFirst is On, tried before every handler added so far.
func (m *Model) OnFirst(marker string, answer func(req domain.AgentRequest) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append([]handler{{contains: marker, answer: answer}}, m.handlers...)
}

// Say answers marker with fixed text.
func (m *Model) Say(marker, text string) {
	m.On(marker, func(domain.AgentRequest) (string, error) { return text, nil })
}

func (m *Model) CallsWith(marker string) []domain.AgentRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.AgentRequest
	for _, c := range m.Calls {
		if len(c.Messages) > 0 && strings.Contains(c.Messages[0].Content, marker) {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, req)
	m.active++
	if m.active > m.MaxActive {
		m.MaxActive = m.active
	}
	handlers := append([]handler{}, m.handlers...)
	delay := m.Delay
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.active--
		m.mu.Unlock()
	}()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return domain.AgentResponse{}, ctx.Err()
		}
	}
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	for _, h := range handlers {
		if strings.Contains(system, h.contains) {
			text, err := h.answer(req)
			if err != nil {
				return domain.AgentResponse{}, err
			}
			return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: text}}, nil
		}
	}
	return domain.AgentResponse{}, context.DeadlineExceeded
}

func (m *Model) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return m.Chat(ctx, req)
}
func (m *Model) Models(context.Context) ([]string, error)                 { return nil, nil }
func (m *Model) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

// Agents is a port-like lister of one enabled API agent.
type Agents struct{ List []domain.Agent }

func (a Agents) ListAgents(context.Context) ([]domain.Agent, error) { return a.List, nil }

// OneAgent is the lister of a single enabled product-manager agent on an API
// provider.
func OneAgent() Agents {
	return Agents{List: []domain.Agent{{ID: uuid.New(), Name: "Product Manager", CatalogSlug: "product-manager", Enabled: true, ProviderType: domain.LLMProviderOpenAI, Model: "test-model"}}}
}

func (s *Store) RuntimeChoice(context.Context) (domain.AudienceRuntimeChoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.choice, nil
}

func (s *Store) SaveRuntimeChoice(_ context.Context, c domain.AudienceRuntimeChoice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.choice = c
	return nil
}
