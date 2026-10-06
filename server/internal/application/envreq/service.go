// Package envreq keeps a component's deploy target supplied with the
// environment variables its code reads: which ones it needs (declared by the
// agent that introduced them, classified by a human, or found in an env
// example file), which the provider already has, and filling the gap —
// itself where a value is known or random, from a human where it is not.
//
// A variable's value never reaches an agent, a comment or the database: a
// secret goes from the human's request (or the generator) straight to the
// provider. The one stored value is the value kind's, which is not a secret
// by definition.
package envreq

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Environments interface {
	ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error)
}

// Cloud is the slice of *cloud.Service that reads and writes a bound
// environment's variables.
type Cloud interface {
	// EnvCapabilities errs for an environment whose variables cannot be
	// managed; such an environment is skipped.
	EnvCapabilities(ctx context.Context, envID uuid.UUID) (domain.EnvCapabilities, error)
	EnvVars(ctx context.Context, envID uuid.UUID) ([]domain.CloudEnvVar, error)
	WriteEnvVars(ctx context.Context, envID uuid.UUID, writes []domain.CloudEnvWrite) error
	Redeploy(ctx context.Context, envID uuid.UUID) (domain.CloudDeployment, error)
}

type Components interface {
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
}

// Examples reads the repository's env example files; satisfied by
// *repository.Service.
type Examples interface {
	EnvExamples(ctx context.Context, repositoryID uuid.UUID, taskID *uuid.UUID) ([]domain.EnvExample, error)
}

type Deps struct {
	Store        port.EnvRequirementStore
	Environments Environments
	Cloud        Cloud
	Components   Components
	Examples     Examples
	// Secret and Hash default to 32 random bytes and bcrypt; tests pin them.
	Secret func() (string, error)
	Hash   func(password string) (string, error)
}

type Service struct {
	store        port.EnvRequirementStore
	environments Environments
	cloud        Cloud
	components   Components
	examples     Examples
	secret       func() (string, error)
	hash         func(password string) (string, error)
	resume       func(ctx context.Context, repositoryID uuid.UUID)
}

func New(d Deps) *Service {
	s := &Service{
		store:        d.Store,
		environments: d.Environments,
		cloud:        d.Cloud,
		components:   d.Components,
		examples:     d.Examples,
		secret:       d.Secret,
		hash:         d.Hash,
	}
	if s.secret == nil {
		s.secret = randomSecret
	}
	if s.hash == nil {
		s.hash = bcryptHash
	}
	return s
}

// SetResumer wires what runs once a component has every variable it needs:
// waking the tasks whose merge waited on them. Set after the release service
// is built, which needs this service first.
func (s *Service) SetResumer(f func(ctx context.Context, repositoryID uuid.UUID)) { s.resume = f }

const minPasswordLength = 8

// bcrypt reads at most 72 bytes; a longer password would be silently cut.
const maxPasswordBytes = 72

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func bcryptHash(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Declare records the variables an agent's change introduced. Every entry is
// validated before any is stored, so a bad one rejects the call as a whole.
func (s *Service) Declare(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID, taskID uuid.UUID, reqs []domain.EnvRequirement) ([]domain.EnvRequirement, error) {
	for _, r := range reqs {
		if err := r.Validate(); err != nil {
			return nil, err
		}
	}
	out := make([]domain.EnvRequirement, 0, len(reqs))
	for _, r := range reqs {
		r.RepositoryID, r.ComponentID, r.Source = repositoryID, componentID, domain.EnvSourceAgent
		id := taskID
		r.TaskID = &id
		stored, err := s.store.UpsertEnvRequirement(ctx, r)
		if err != nil {
			return out, err
		}
		out = append(out, stored)
	}
	return out, nil
}

// EnvInput is one variable a human sets on the Deploy tab or the task: Kind
// classifies it (empty keeps what is recorded) and Value is the literal for
// the value kind, the secret for human_secret, the password for human_bcrypt.
// Each one given is written even when the provider already has the variable —
// that is how a value is changed. Regenerate replaces a generated secret.
type EnvInput struct {
	Name       string            `json:"name"`
	Kind       domain.EnvVarKind `json:"kind,omitempty"`
	Value      string            `json:"value,omitempty"`
	Regenerate bool              `json:"regenerate,omitempty"`
}

type target struct {
	env       domain.ComponentEnvironment
	caps      domain.EnvCapabilities
	component domain.Component
	all       []domain.Component
}

func (s *Service) targets(ctx context.Context, repositoryID uuid.UUID) ([]target, error) {
	envs, err := s.environments.ListEnvironments(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	comps, err := s.components.ListComponents(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]domain.Component, len(comps))
	for _, c := range comps {
		byID[c.ID] = c
	}
	var out []target
	for _, e := range envs {
		if e.Environment != domain.EnvironmentProduction || e.Status != domain.LinkConfirmed || !e.Bound() {
			continue
		}
		comp, ok := byID[e.ComponentID]
		if !ok {
			continue
		}
		caps, err := s.cloud.EnvCapabilities(ctx, e.ID)
		if err != nil {
			continue
		}
		out = append(out, target{env: e, caps: caps, component: comp, all: comps})
	}
	return out, nil
}

func (s *Service) targetFor(ctx context.Context, repositoryID, componentID uuid.UUID) (target, bool, error) {
	all, err := s.targets(ctx, repositoryID)
	if err != nil {
		return target{}, false, err
	}
	for _, t := range all {
		if t.component.ID == componentID {
			return t, true, nil
		}
	}
	return target{}, false, nil
}

// views is every variable t's component requires or an example file names,
// sorted by name, without the provider's side filled in yet.
func (s *Service) views(ctx context.Context, repositoryID uuid.UUID, t target, taskID *uuid.UUID) ([]domain.EnvVarView, error) {
	rows, err := s.store.ListEnvRequirements(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	byName := map[string]*domain.EnvVarView{}
	scoped := map[string]bool{}
	for _, r := range rows {
		if r.ComponentID != nil && *r.ComponentID != t.component.ID {
			continue
		}
		// A component's own row overrides the repository-wide one.
		if scoped[r.Name] && r.ComponentID == nil {
			continue
		}
		v := &domain.EnvVarView{Name: r.Name, Kind: r.Kind, Description: r.Description, Source: r.Source}
		if r.Kind == domain.EnvKindValue {
			v.Value = r.Value
		}
		byName[r.Name] = v
		scoped[r.Name] = r.ComponentID != nil
	}
	if s.examples != nil {
		examples, err := s.examples.EnvExamples(ctx, repositoryID, taskID)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("envreq: reading env example files failed")
		}
		for _, ex := range examples {
			if exampleComponent(ex.Path, t.all) != t.component.ID {
				continue
			}
			v, ok := byName[ex.Name]
			if !ok {
				v = &domain.EnvVarView{Name: ex.Name}
				byName[ex.Name] = v
			}
			v.ExampleValue, v.ExamplePath = ex.Value, ex.Path
		}
	}
	out := make([]domain.EnvVarView, 0, len(byName))
	for _, v := range byName {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// exampleComponent is the component whose path is the longest prefix of the
// example file's directory; a single-component repository owns every file.
func exampleComponent(file string, comps []domain.Component) uuid.UUID {
	if len(comps) == 1 {
		return comps[0].ID
	}
	dir := path.Dir(strings.ReplaceAll(file, "\\", "/"))
	best, bestLen := uuid.Nil, -1
	for _, c := range comps {
		p := strings.Trim(c.Path, "/")
		if p == "." {
			p = ""
		}
		matches := p == "" || dir == p || strings.HasPrefix(dir, p+"/")
		if matches && len(p) > bestLen {
			best, bestLen = c.ID, len(p)
		}
	}
	return best
}

func present(vars []domain.CloudEnvVar) map[string]domain.CloudEnvVar {
	out := make(map[string]domain.CloudEnvVar, len(vars))
	for _, v := range vars {
		out[v.Key] = v
	}
	return out
}

func action(v domain.EnvVarView) domain.EnvVarAction {
	switch {
	case v.Kind == domain.EnvKindOptional:
		return domain.EnvActionNone
	case v.Kind.Automatic():
		if v.Production && v.Preview {
			return domain.EnvActionNone
		}
		return domain.EnvActionAuto
	case v.Production:
		return domain.EnvActionNone
	case v.Kind.NeedsHuman():
		return domain.EnvActionHuman
	default:
		return domain.EnvActionClassify
	}
}

// fill reads a target without previews as having its preview wherever it
// has production, so nothing waits on an environment that does not exist.
func fill(views []domain.EnvVarView, have map[string]domain.CloudEnvVar, caps domain.EnvCapabilities) {
	previews := caps.Supports(domain.EnvironmentPreview)
	for i := range views {
		if cv, ok := have[views[i].Name]; ok {
			views[i].Production = cv.SetFor(domain.EnvironmentProduction)
			views[i].Preview = cv.SetFor(domain.EnvironmentPreview)
			if !previews {
				views[i].Preview = views[i].Production
			}
		}
		views[i].Action = action(views[i])
	}
}

func (s *Service) status(ctx context.Context, repositoryID uuid.UUID, t target, taskID *uuid.UUID) (domain.ComponentEnvStatus, error) {
	st := domain.ComponentEnvStatus{
		ComponentID: t.component.ID, ComponentName: t.component.DisplayName(),
		EnvironmentID: t.env.ID, Provider: t.env.Provider, Capabilities: t.caps,
	}
	if t.env.Resource != nil {
		st.ResourceName = t.env.Resource.Name
	}
	views, err := s.views(ctx, repositoryID, t, taskID)
	if err != nil {
		return st, err
	}
	vars, err := s.cloud.EnvVars(ctx, t.env.ID)
	if err != nil {
		st.Error = err.Error()
	}
	fill(views, present(vars), t.caps)
	st.Vars = views
	return st, nil
}

// Status is every env-managed production target of the repository against
// what its component requires.
func (s *Service) Status(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvStatus, error) {
	targets, err := s.targets(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ComponentEnvStatus, 0, len(targets))
	for _, t := range targets {
		st, err := s.status(ctx, repositoryID, t, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// Ensure is the pre-ship check: it writes every variable it can fill itself
// and reports the ones still waiting on a human. A component with no
// env-managed production target has nothing to check. taskID adds the
// variables of the task's own checkout — the code about to ship.
func (s *Service) Ensure(ctx context.Context, repositoryID, componentID uuid.UUID, taskID *uuid.UUID) (domain.EnvEnsureResult, error) {
	t, ok, err := s.targetFor(ctx, repositoryID, componentID)
	if err != nil {
		return domain.EnvEnsureResult{}, fmt.Errorf("%w: %v", domain.ErrDeployEnvUnchecked, err)
	}
	if !ok {
		return domain.EnvEnsureResult{}, nil
	}
	return s.ensure(ctx, repositoryID, t, taskID)
}

func (s *Service) ensure(ctx context.Context, repositoryID uuid.UUID, t target, taskID *uuid.UUID) (domain.EnvEnsureResult, error) {
	views, err := s.views(ctx, repositoryID, t, taskID)
	if err != nil {
		return domain.EnvEnsureResult{}, fmt.Errorf("%w: %v", domain.ErrDeployEnvUnchecked, err)
	}
	vars, err := s.cloud.EnvVars(ctx, t.env.ID)
	if err != nil {
		return domain.EnvEnsureResult{}, fmt.Errorf("%w: %v", domain.ErrDeployEnvUnchecked, err)
	}
	fill(views, present(vars), t.caps)

	res := domain.EnvEnsureResult{OverwrittenOnDeploy: t.caps.OverwrittenOnDeploy}
	var writes []domain.CloudEnvWrite
	for _, v := range views {
		switch v.Action {
		case domain.EnvActionAuto:
			w, err := s.autoWrite(v, t.caps)
			if err != nil {
				return res, fmt.Errorf("%w: %v", domain.ErrDeployEnvUnchecked, err)
			}
			writes = append(writes, w)
		case domain.EnvActionHuman, domain.EnvActionClassify:
			res.Missing = append(res.Missing, v.Name)
		}
	}
	if len(writes) > 0 {
		if err := s.cloud.WriteEnvVars(ctx, t.env.ID, writes); err != nil {
			return res, fmt.Errorf("%w: %v", domain.ErrDeployEnvUnchecked, err)
		}
		for _, w := range writes {
			res.Created = append(res.Created, w.Key)
		}
	}
	return res, nil
}

func (s *Service) autoWrite(v domain.EnvVarView, caps domain.EnvCapabilities) (domain.CloudEnvWrite, error) {
	var targets []domain.DeployEnvironment
	if !v.Production {
		targets = append(targets, domain.EnvironmentProduction)
	}
	if !v.Preview && caps.Supports(domain.EnvironmentPreview) {
		targets = append(targets, domain.EnvironmentPreview)
	}
	if v.Kind == domain.EnvKindValue {
		return domain.CloudEnvWrite{Key: v.Name, Value: v.Value, Targets: targets}, nil
	}
	secret, err := s.secret()
	if err != nil {
		return domain.CloudEnvWrite{}, fmt.Errorf("generate %s: %w", v.Name, err)
	}
	return domain.CloudEnvWrite{Key: v.Name, Value: secret, Sensitive: true, Targets: targets}, nil
}

// Apply records a human's classifications, writes the secrets they entered,
// fills everything automatic, and — once nothing is missing — resumes the
// merges that waited on this component.
func (s *Service) Apply(ctx context.Context, repositoryID, componentID uuid.UUID, inputs []EnvInput) (domain.ComponentEnvStatus, error) {
	t, ok, err := s.targetFor(ctx, repositoryID, componentID)
	if err != nil {
		return domain.ComponentEnvStatus{}, err
	}
	if !ok {
		return domain.ComponentEnvStatus{}, fmt.Errorf("%w: this component has no production deploy target whose environment variables TaskTrooper can manage", domain.ErrEnvRequirementInvalid)
	}
	views, err := s.views(ctx, repositoryID, t, nil)
	if err != nil {
		return domain.ComponentEnvStatus{}, err
	}
	kinds := make(map[string]domain.EnvVarKind, len(views))
	for _, v := range views {
		kinds[v.Name] = v.Kind
	}

	var writes []domain.CloudEnvWrite
	for _, in := range inputs {
		kind := in.Kind
		if kind == "" {
			kind = kinds[in.Name]
		}
		// A changed literal is a reclassification too: the stored value is
		// what every later check writes.
		if in.Kind != "" || (kind == domain.EnvKindValue && in.Value != "") {
			req := domain.EnvRequirement{RepositoryID: repositoryID, ComponentID: &componentID, Name: in.Name, Kind: kind, Source: domain.EnvSourceHuman}
			if kind == domain.EnvKindValue {
				req.Value = in.Value
			}
			if err := req.Validate(); err != nil {
				return domain.ComponentEnvStatus{}, err
			}
			if _, err := s.store.UpsertEnvRequirement(ctx, req); err != nil {
				return domain.ComponentEnvStatus{}, err
			}
		}
		var w domain.CloudEnvWrite
		switch {
		case kind.NeedsHuman() && in.Value != "":
			if w, err = s.humanWrite(in.Name, kind, in.Value, t.caps); err != nil {
				return domain.ComponentEnvStatus{}, err
			}
		case kind == domain.EnvKindValue && in.Value != "":
			w = domain.CloudEnvWrite{Key: in.Name, Value: in.Value, Targets: t.caps.Targets}
		case kind == domain.EnvKindGenerated && in.Regenerate:
			secret, err := s.secret()
			if err != nil {
				return domain.ComponentEnvStatus{}, fmt.Errorf("generate %s: %w", in.Name, err)
			}
			w = domain.CloudEnvWrite{Key: in.Name, Value: secret, Sensitive: true, Targets: t.caps.Targets}
		default:
			continue
		}
		writes = append(writes, w)
	}
	if len(writes) > 0 {
		if err := s.cloud.WriteEnvVars(ctx, t.env.ID, writes); err != nil {
			return domain.ComponentEnvStatus{}, err
		}
	}
	if _, err := s.ensure(ctx, repositoryID, t, nil); err != nil && !errors.Is(err, domain.ErrDeployEnvUnchecked) {
		return domain.ComponentEnvStatus{}, err
	}
	st, err := s.status(ctx, repositoryID, t, nil)
	if err != nil {
		return domain.ComponentEnvStatus{}, err
	}
	if st.Error == "" && len(st.Missing()) == 0 && s.resume != nil {
		s.resume(ctx, repositoryID)
	}
	return st, nil
}

func (s *Service) humanWrite(name string, kind domain.EnvVarKind, value string, caps domain.EnvCapabilities) (domain.CloudEnvWrite, error) {
	w := domain.CloudEnvWrite{Key: name, Sensitive: true, Targets: caps.Targets}
	switch kind {
	case domain.EnvKindHumanSecret:
		w.Value = strings.TrimSpace(value)
	case domain.EnvKindHumanBcrypt:
		if len(value) < minPasswordLength {
			return w, fmt.Errorf("%w: the password for %s must be at least %d characters", domain.ErrEnvRequirementInvalid, name, minPasswordLength)
		}
		if len(value) > maxPasswordBytes {
			return w, fmt.Errorf("%w: the password for %s must be at most %d bytes", domain.ErrEnvRequirementInvalid, name, maxPasswordBytes)
		}
		hash, err := s.hash(value)
		if err != nil {
			return w, fmt.Errorf("hash %s: %w", name, err)
		}
		w.Value = hash
	}
	return w, nil
}

// Redeploy rebuilds the component's production deployment so variables
// written since its last deploy reach the running site.
func (s *Service) Redeploy(ctx context.Context, repositoryID, componentID uuid.UUID) (domain.CloudDeployment, error) {
	t, ok, err := s.targetFor(ctx, repositoryID, componentID)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	if !ok {
		return domain.CloudDeployment{}, fmt.Errorf("%w: this component has no production deploy target TaskTrooper can redeploy", domain.ErrEnvRequirementInvalid)
	}
	if t.caps.WritesRollOut {
		return domain.CloudDeployment{}, fmt.Errorf("%w: %s rolls a variable out as soon as it is written — there is nothing to redeploy", domain.ErrEnvRequirementInvalid, t.env.Provider)
	}
	return s.cloud.Redeploy(ctx, t.env.ID)
}
