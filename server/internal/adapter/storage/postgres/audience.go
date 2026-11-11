package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type AudienceStore struct {
	pool *DB
}

var _ port.AudienceStore = (*AudienceStore)(nil)

func NewAudienceStore(pool *DB) *AudienceStore { return &AudienceStore{pool: pool} }

func jsonOf(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return b, nil
}

// ---- audiences

const audienceColumns = `id, name, repository_id, status, brief, persona_target, product_context, generation, error, created_at, updated_at, built_at`

func scanAudience(row pgx.Row, extra ...any) (domain.Audience, error) {
	var a domain.Audience
	var status string
	var brief []byte
	dest := append([]any{&a.ID, &a.Name, &a.RepositoryID, &status, &brief, &a.PersonaTarget, &a.ProductContext, &a.Generation, &a.Error, &a.CreatedAt, &a.UpdatedAt, &a.BuiltAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.Audience{}, err
	}
	a.Status = domain.AudienceStatus(status)
	if len(brief) > 0 {
		if err := json.Unmarshal(brief, &a.Brief); err != nil {
			return domain.Audience{}, fmt.Errorf("decode audience brief: %w", err)
		}
	}
	return a, nil
}

func (s *AudienceStore) CreateAudience(ctx context.Context, a domain.Audience) (domain.Audience, error) {
	brief, err := jsonOf(a.Brief)
	if err != nil {
		return domain.Audience{}, err
	}
	status := a.Status
	if status == "" {
		status = domain.AudienceDraft
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO audiences (name, repository_id, status, brief, persona_target, product_context)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+audienceColumns, a.Name, a.RepositoryID, string(status), brief, a.PersonaTarget, a.ProductContext)
	out, err := scanAudience(row)
	if err != nil {
		return domain.Audience{}, fmt.Errorf("create audience: %w", err)
	}
	return out, nil
}

func (s *AudienceStore) GetAudience(ctx context.Context, id uuid.UUID) (domain.Audience, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+audienceColumns+`,
		  (SELECT count(*) FROM audience_personas p WHERE p.audience_id = audiences.id),
		  (SELECT count(*) FROM audience_studies st WHERE st.audience_id = audiences.id)
		FROM audiences WHERE id = $1`, id)
	var counts [2]int
	a, err := scanAudience(row, &counts[0], &counts[1])
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Audience{}, domain.ErrAudienceNotFound
	}
	if err != nil {
		return domain.Audience{}, fmt.Errorf("get audience: %w", err)
	}
	a.PersonaCount, a.StudyCount = counts[0], counts[1]
	return a, nil
}

func (s *AudienceStore) ListAudiences(ctx context.Context) ([]domain.Audience, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+audienceColumns+`,
		  (SELECT count(*) FROM audience_personas p WHERE p.audience_id = audiences.id),
		  (SELECT count(*) FROM audience_studies st WHERE st.audience_id = audiences.id)
		FROM audiences ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list audiences: %w", err)
	}
	defer rows.Close()
	out := []domain.Audience{}
	for rows.Next() {
		var counts [2]int
		a, err := scanAudience(rows, &counts[0], &counts[1])
		if err != nil {
			return nil, fmt.Errorf("scan audience: %w", err)
		}
		a.PersonaCount, a.StudyCount = counts[0], counts[1]
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *AudienceStore) SaveAudience(ctx context.Context, a domain.Audience) error {
	brief, err := jsonOf(a.Brief)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE audiences SET name = $2, repository_id = $3, status = $4, brief = $5, persona_target = $6,
		  product_context = $7, generation = $8, error = $9, built_at = $10, updated_at = now()
		WHERE id = $1`, a.ID, a.Name, a.RepositoryID, string(a.Status), brief, a.PersonaTarget, a.ProductContext, a.Generation, a.Error, a.BuiltAt)
	if err != nil {
		return fmt.Errorf("save audience: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAudienceNotFound
	}
	return nil
}

func (s *AudienceStore) DeleteAudience(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audiences WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete audience: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAudienceNotFound
	}
	return nil
}

// ---- personas

const personaColumns = `id, audience_id, ordinal, generation, name, segment, profile, created_at`

func scanPersona(row pgx.Row) (domain.Persona, error) {
	var p domain.Persona
	var profile []byte
	if err := row.Scan(&p.ID, &p.AudienceID, &p.Ordinal, &p.Generation, &p.Name, &p.Segment, &profile, &p.CreatedAt); err != nil {
		return domain.Persona{}, err
	}
	if len(profile) > 0 {
		if err := json.Unmarshal(profile, &p.PersonaProfile); err != nil {
			return domain.Persona{}, fmt.Errorf("decode persona profile: %w", err)
		}
	}
	return p, nil
}

func (s *AudienceStore) ReplacePersonas(ctx context.Context, audienceID uuid.UUID, personas []domain.Persona) error {
	return s.pool.InTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM audience_personas WHERE audience_id = $1`, audienceID); err != nil {
			return fmt.Errorf("clear personas: %w", err)
		}
		for i, p := range personas {
			profile, err := jsonOf(p.PersonaProfile)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO audience_personas (audience_id, ordinal, generation, name, segment, profile)
				VALUES ($1, $2, $3, $4, $5, $6)`, audienceID, i+1, p.Generation, p.Name, p.Segment, profile); err != nil {
				return fmt.Errorf("insert persona: %w", err)
			}
		}
		return nil
	})
}

func (s *AudienceStore) ListPersonas(ctx context.Context, audienceID uuid.UUID) ([]domain.Persona, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+personaColumns+` FROM audience_personas WHERE audience_id = $1 ORDER BY ordinal`, audienceID)
	if err != nil {
		return nil, fmt.Errorf("list personas: %w", err)
	}
	defer rows.Close()
	out := []domain.Persona{}
	for rows.Next() {
		p, err := scanPersona(rows)
		if err != nil {
			return nil, fmt.Errorf("scan persona: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *AudienceStore) GetPersona(ctx context.Context, id uuid.UUID) (domain.Persona, error) {
	p, err := scanPersona(s.pool.QueryRow(ctx, `SELECT `+personaColumns+` FROM audience_personas WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Persona{}, domain.ErrPersonaNotFound
	}
	if err != nil {
		return domain.Persona{}, fmt.Errorf("get persona: %w", err)
	}
	return p, nil
}

// ---- studies

const studyColumns = `id, audience_id, title, goal, questions, price_points, currency, product_snapshot, status,
	persona_total, persona_done, persona_failed, calls_estimate, error, report, created_at, started_at, finished_at,
	kind, inputs, script, script_state, script_calls,
	script_version, script_approved_version, script_approved_at, script_approved_by`

func scanStudy(row pgx.Row, extra ...any) (domain.Study, error) {
	var s domain.Study
	var status string
	var questions, prices, report, inputs, script []byte
	var kind, scriptState string
	dest := append([]any{&s.ID, &s.AudienceID, &s.Title, &s.Goal, &questions, &prices, &s.Currency, &s.ProductSnapshot, &status,
		&s.PersonaTotal, &s.PersonaDone, &s.PersonaFailed, &s.CallsEstimate, &s.Error, &report, &s.CreatedAt, &s.StartedAt, &s.FinishedAt,
		&kind, &inputs, &script, &scriptState, &s.ScriptCalls,
		&s.ScriptVersion, &s.ScriptApprovedVersion, &s.ScriptApprovedAt, &s.ScriptApprovedBy}, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.Study{}, err
	}
	s.Status = domain.StudyStatus(status)
	s.Kind, s.ScriptState = domain.StudyKind(kind), domain.ScriptState(scriptState)
	if len(inputs) > 0 {
		if err := json.Unmarshal(inputs, &s.Inputs); err != nil {
			return domain.Study{}, fmt.Errorf("decode study inputs: %w", err)
		}
	}
	if len(script) > 0 && string(script) != "null" {
		s.Script = &domain.StudyScript{}
		if err := json.Unmarshal(script, s.Script); err != nil {
			return domain.Study{}, fmt.Errorf("decode study script: %w", err)
		}
	}
	s.Questions, s.PricePoints = []string{}, []float64{}
	if len(questions) > 0 {
		if err := json.Unmarshal(questions, &s.Questions); err != nil {
			return domain.Study{}, fmt.Errorf("decode study questions: %w", err)
		}
	}
	if len(prices) > 0 {
		if err := json.Unmarshal(prices, &s.PricePoints); err != nil {
			return domain.Study{}, fmt.Errorf("decode study prices: %w", err)
		}
	}
	if len(report) > 0 && string(report) != "null" {
		s.Report = &domain.Report{}
		if err := json.Unmarshal(report, s.Report); err != nil {
			return domain.Study{}, fmt.Errorf("decode study report: %w", err)
		}
	}
	return s, nil
}

func (s *AudienceStore) CreateStudy(ctx context.Context, st domain.Study) (domain.Study, error) {
	questions, err := jsonOf(nonNil(st.Questions))
	if err != nil {
		return domain.Study{}, err
	}
	prices, err := jsonOf(nonNilF(st.PricePoints))
	if err != nil {
		return domain.Study{}, err
	}
	status := st.Status
	if status == "" {
		status = domain.StudyDraft
	}
	inputs, script, kind, scriptState, err := studyExtras(st)
	if err != nil {
		return domain.Study{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO audience_studies (audience_id, title, goal, questions, price_points, currency, product_snapshot, status, persona_total, calls_estimate,
		  kind, inputs, script, script_state, script_calls, script_version, script_approved_version, script_approved_at, script_approved_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		RETURNING `+studyColumns, st.AudienceID, st.Title, st.Goal, questions, prices, st.Currency, st.ProductSnapshot, string(status), st.PersonaTotal, st.CallsEstimate,
		kind, inputs, script, scriptState, st.ScriptCalls, st.ScriptVersion, st.ScriptApprovedVersion, st.ScriptApprovedAt, st.ScriptApprovedBy)
	out, err := scanStudy(row)
	if err != nil {
		return domain.Study{}, fmt.Errorf("create study: %w", err)
	}
	return out, nil
}

// studyExtras encodes what a study carries beyond the original columns.
func studyExtras(st domain.Study) (inputs, script []byte, kind, scriptState string, err error) {
	if inputs, err = jsonOf(st.Inputs); err != nil {
		return
	}
	if st.Script != nil {
		if script, err = jsonOf(st.Script); err != nil {
			return
		}
	}
	kind, scriptState = string(st.Kind), string(st.ScriptState)
	if kind == "" {
		kind = string(domain.KindUserInterviews)
	}
	if scriptState == "" {
		scriptState = string(domain.ScriptNone)
	}
	return
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func nonNilF(v []float64) []float64 {
	if v == nil {
		return []float64{}
	}
	return v
}

func (s *AudienceStore) GetStudy(ctx context.Context, id uuid.UUID) (domain.Study, error) {
	st, err := scanStudy(s.pool.QueryRow(ctx, `SELECT `+studyColumns+` FROM audience_studies WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Study{}, domain.ErrStudyNotFound
	}
	if err != nil {
		return domain.Study{}, fmt.Errorf("get study: %w", err)
	}
	return st, nil
}

func (s *AudienceStore) ListStudies(ctx context.Context, audienceID *uuid.UUID) ([]domain.Study, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+studyColumns+`, (SELECT name FROM audiences a WHERE a.id = audience_studies.audience_id)
		FROM audience_studies
		WHERE ($1::uuid IS NULL OR audience_id = $1)
		ORDER BY created_at DESC`, audienceID)
	if err != nil {
		return nil, fmt.Errorf("list studies: %w", err)
	}
	defer rows.Close()
	out := []domain.Study{}
	for rows.Next() {
		var name *string
		st, err := scanStudy(rows, &name)
		if err != nil {
			return nil, fmt.Errorf("scan study: %w", err)
		}
		if name != nil {
			st.AudienceName = *name
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *AudienceStore) SaveStudy(ctx context.Context, st domain.Study) error {
	questions, err := jsonOf(nonNil(st.Questions))
	if err != nil {
		return err
	}
	prices, err := jsonOf(nonNilF(st.PricePoints))
	if err != nil {
		return err
	}
	var report []byte
	if st.Report != nil {
		if report, err = jsonOf(st.Report); err != nil {
			return err
		}
	}
	inputs, script, kind, scriptState, err := studyExtras(st)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE audience_studies SET title = $2, goal = $3, questions = $4, price_points = $5, currency = $6,
		  product_snapshot = $7, status = $8, persona_total = $9, persona_done = $10, persona_failed = $11,
		  calls_estimate = $12, error = $13, report = $14, started_at = $15, finished_at = $16,
		  kind = $17, inputs = $18, script = $19, script_state = $20, script_calls = $21,
		  script_version = $22, script_approved_version = $23, script_approved_at = $24, script_approved_by = $25
		WHERE id = $1`, st.ID, st.Title, st.Goal, questions, prices, st.Currency, st.ProductSnapshot, string(st.Status),
		st.PersonaTotal, st.PersonaDone, st.PersonaFailed, st.CallsEstimate, st.Error, report, st.StartedAt, st.FinishedAt,
		kind, inputs, script, scriptState, st.ScriptCalls, st.ScriptVersion, st.ScriptApprovedVersion, st.ScriptApprovedAt, st.ScriptApprovedBy)
	if err != nil {
		return fmt.Errorf("save study: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrStudyNotFound
	}
	return nil
}

func (s *AudienceStore) DeleteStudy(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audience_studies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete study: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrStudyNotFound
	}
	return nil
}

func (s *AudienceStore) BumpStudyProgress(ctx context.Context, id uuid.UUID, done, failed int) (int, int, error) {
	var d, f int
	err := s.pool.QueryRow(ctx, `
		UPDATE audience_studies SET persona_done = persona_done + $2, persona_failed = persona_failed + $3
		WHERE id = $1 RETURNING persona_done, persona_failed`, id, done, failed).Scan(&d, &f)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, domain.ErrStudyNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("bump study progress: %w", err)
	}
	return d, f, nil
}

func (s *AudienceStore) SaveAnswer(ctx context.Context, a domain.StudyAnswer) error {
	persona, err := jsonOf(a.Persona)
	if err != nil {
		return err
	}
	answers, err := jsonOf(a.Answers)
	if err != nil {
		return err
	}
	status := a.Status
	if status == "" {
		status = "ok"
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO audience_study_answers (study_id, persona_id, persona, status, answers, error)
		VALUES ($1, $2, $3, $4, $5, $6)`, a.StudyID, a.PersonaID, persona, status, answers, a.Error); err != nil {
		return fmt.Errorf("save answer: %w", err)
	}
	return nil
}

func (s *AudienceStore) DeleteFailedAnswers(ctx context.Context, studyID uuid.UUID) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM audience_study_answers WHERE study_id = $1 AND status = 'failed'`, studyID)
	if err != nil {
		return 0, fmt.Errorf("delete failed answers: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s *AudienceStore) ListAnswers(ctx context.Context, studyID uuid.UUID) ([]domain.StudyAnswer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, study_id, persona_id, persona, status, answers, error, created_at
		FROM audience_study_answers WHERE study_id = $1 ORDER BY created_at, id`, studyID)
	if err != nil {
		return nil, fmt.Errorf("list answers: %w", err)
	}
	defer rows.Close()
	out := []domain.StudyAnswer{}
	for rows.Next() {
		var a domain.StudyAnswer
		var persona, answers []byte
		if err := rows.Scan(&a.ID, &a.StudyID, &a.PersonaID, &persona, &a.Status, &answers, &a.Error, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan answer: %w", err)
		}
		if err := json.Unmarshal(persona, &a.Persona); err != nil {
			return nil, fmt.Errorf("decode answer persona: %w", err)
		}
		if err := json.Unmarshal(answers, &a.Answers); err != nil {
			return nil, fmt.Errorf("decode answers: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- messages

const messageColumns = `id, persona_id, study_id, group_id, role, content, created_at`

func (s *AudienceStore) AddMessages(ctx context.Context, msgs []domain.ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	return s.pool.InTx(ctx, func(tx pgx.Tx) error {
		for _, m := range msgs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO audience_messages (persona_id, study_id, group_id, role, content)
				VALUES ($1, $2, $3, $4, $5)`, m.PersonaID, m.StudyID, m.GroupID, m.Role, m.Content); err != nil {
				return fmt.Errorf("add message: %w", err)
			}
		}
		return nil
	})
}

func (s *AudienceStore) queryMessages(ctx context.Context, where string, arg any) ([]domain.ChatMessage, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+messageColumns+` FROM audience_messages WHERE `+where+` ORDER BY created_at, id`, arg)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	out := []domain.ChatMessage{}
	for rows.Next() {
		var m domain.ChatMessage
		if err := rows.Scan(&m.ID, &m.PersonaID, &m.StudyID, &m.GroupID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *AudienceStore) ListMessages(ctx context.Context, personaID uuid.UUID) ([]domain.ChatMessage, error) {
	return s.queryMessages(ctx, `persona_id = $1`, personaID)
}

func (s *AudienceStore) ListStudyMessages(ctx context.Context, studyID uuid.UUID) ([]domain.ChatMessage, error) {
	return s.queryMessages(ctx, `study_id = $1`, studyID)
}

func (s *AudienceStore) ClearMessages(ctx context.Context, personaID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM audience_messages WHERE persona_id = $1`, personaID); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}
	return nil
}

func (s *AudienceStore) FailInterrupted(ctx context.Context, reason string) (int, int, error) {
	a, err := s.pool.Exec(ctx, `UPDATE audiences SET status = 'failed', error = $1, updated_at = now() WHERE status = 'building'`, reason)
	if err != nil {
		return 0, 0, fmt.Errorf("fail interrupted audiences: %w", err)
	}
	st, err := s.pool.Exec(ctx, `
		UPDATE audience_studies SET status = 'failed', error = $1, finished_at = now()
		WHERE status IN ('running', 'synthesizing')`, reason)
	if err != nil {
		return 0, 0, fmt.Errorf("fail interrupted studies: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE audience_studies SET script_state = 'failed', error = $1
		WHERE script_state = 'generating'`, reason); err != nil {
		return 0, 0, fmt.Errorf("fail interrupted scripts: %w", err)
	}
	return int(a.RowsAffected()), int(st.RowsAffected()), nil
}
