package projectmodel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// logProjection re-derives the legacy repository/pipeline fields after an edit
// that touched components or checks; a projection failure never fails the edit
// itself, since the edit already landed in the project model, the source of
// truth going forward.
func (s *Service) logProjection(ctx context.Context, repositoryID uuid.UUID) {
	if err := s.project(ctx, repositoryID); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("project model: legacy projection failed")
	}
}

func (s *Service) AddComponent(ctx context.Context, repoID uuid.UUID, req domain.NewComponentRequest) (domain.Component, error) {
	repo, err := s.repos.Get(ctx, repoID)
	if err != nil {
		return domain.Component{}, fmt.Errorf("add component: %w", err)
	}
	p, err := domain.NormalizeComponentPath(req.Path)
	if err != nil {
		return domain.Component{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if !domain.ValidComponentRole(req.Role) {
		return domain.Component{}, fmt.Errorf("%w: invalid role %q", ErrInvalidInput, req.Role)
	}
	if p != "." {
		info, statErr := os.Stat(filepath.Join(repo.RootPath, filepath.FromSlash(p)))
		if statErr != nil || !info.IsDir() {
			return domain.Component{}, fmt.Errorf("%w: %q is not a directory under the repository", ErrInvalidInput, p)
		}
	}

	existing, err := s.store.ListComponents(ctx, repoID)
	if err != nil {
		return domain.Component{}, fmt.Errorf("add component: %w", err)
	}

	comp := domain.Component{
		ID:            uuid.New(),
		RepositoryID:  repoID,
		Path:          p,
		Status:        domain.ComponentStatusActive,
		ManuallyAdded: true,
	}
	for _, c := range existing {
		if c.Path != p {
			continue
		}
		if c.Status == domain.ComponentStatusActive {
			return domain.Component{}, fmt.Errorf("%w: a component already exists at %q", ErrConflict, p)
		}
		comp = c
		comp.Status = domain.ComponentStatusActive
		comp.ManuallyAdded = true
		break
	}

	role := req.Role
	comp.Role.Override = &role
	if name := strings.TrimSpace(req.Name); name != "" {
		comp.Name.Override = &name
	}

	saved, err := s.store.SaveComponent(ctx, comp)
	if err != nil {
		return domain.Component{}, fmt.Errorf("add component: %w", err)
	}
	s.logProjection(ctx, repoID)
	return saved, nil
}

func (s *Service) UpdateComponent(ctx context.Context, componentID uuid.UUID, patch domain.ComponentPatch) (domain.Component, error) {
	comp, err := s.store.GetComponent(ctx, componentID)
	if err != nil {
		return domain.Component{}, fmt.Errorf("update component: %w", err)
	}

	comp.Name = patch.Name.ApplyTo(comp.Name)

	if patch.Role.Set {
		if patch.Role.Value != nil && !domain.ValidComponentRole(*patch.Role.Value) {
			return domain.Component{}, fmt.Errorf("%w: invalid role %q", ErrInvalidInput, *patch.Role.Value)
		}
		comp.Role = patch.Role.ApplyTo(comp.Role)
	}

	for purpose, p := range patch.Commands {
		if !domain.ValidCommandPurpose(purpose) {
			return domain.Component{}, fmt.Errorf("%w: invalid command purpose %q", ErrInvalidInput, purpose)
		}
		idx := -1
		for i, cmd := range comp.Commands {
			if cmd.Purpose == purpose {
				idx = i
				break
			}
		}
		var fact domain.Fact[string]
		if idx >= 0 {
			fact = comp.Commands[idx].Command
		}
		fact = p.ApplyTo(fact)
		if fact.Detected == nil && fact.Override == nil {
			if idx >= 0 {
				comp.Commands = append(comp.Commands[:idx], comp.Commands[idx+1:]...)
			}
			continue
		}
		if idx >= 0 {
			comp.Commands[idx].Command = fact
		} else {
			comp.Commands = append(comp.Commands, domain.ComponentCommand{Purpose: purpose, Command: fact})
		}
	}

	if patch.Gates != nil {
		comp.Gates = *patch.Gates
	}
	if patch.Docs != nil {
		comp.Docs = *patch.Docs
	}
	if patch.Status != nil {
		if *patch.Status != domain.ComponentStatusActive && *patch.Status != domain.ComponentStatusDismissed {
			return domain.Component{}, fmt.Errorf("%w: invalid status %q", ErrInvalidInput, *patch.Status)
		}
		comp.Status = *patch.Status
		if comp.Status == domain.ComponentStatusDismissed {
			comp.NeedsReview = false
		}
	}
	if patch.Reviewed != nil && *patch.Reviewed {
		comp.NeedsReview = false
	}

	saved, err := s.store.SaveComponent(ctx, comp)
	if err != nil {
		return domain.Component{}, fmt.Errorf("update component: %w", err)
	}
	s.logProjection(ctx, saved.RepositoryID)
	return saved, nil
}

// hasShellMetacharacters is deliberately generous: LocalCommand.Argv is run
// with exec.Command (no shell), so this only needs to catch the characters a
// human would mistake for shell syntax when typing a command into the UI.
func hasShellMetacharacters(s string) bool {
	return strings.ContainsAny(s, " \t\n\r|&;<>()$`\\\"'*?[]{}~!#")
}

func validateLocalCommand(cmd domain.LocalCommand) (domain.LocalCommand, error) {
	dir, err := domain.NormalizeComponentPath(cmd.Dir)
	if err != nil {
		return domain.LocalCommand{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if len(cmd.Argv) == 0 {
		return domain.LocalCommand{}, fmt.Errorf("%w: a local command needs a non-empty argv", ErrInvalidInput)
	}
	if hasShellMetacharacters(cmd.Argv[0]) {
		return domain.LocalCommand{}, fmt.Errorf("%w: argv[0] %q must not contain whitespace or shell metacharacters", ErrInvalidInput, cmd.Argv[0])
	}
	return domain.LocalCommand{Dir: dir, Argv: cmd.Argv}, nil
}

func validateLocalCommands(cmds []domain.LocalCommand) ([]domain.LocalCommand, error) {
	out := make([]domain.LocalCommand, 0, len(cmds))
	for _, c := range cmds {
		v, err := validateLocalCommand(c)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func newManualJobKey() string {
	return "manual:" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
}

func (s *Service) AddCheck(ctx context.Context, componentID uuid.UUID, req domain.NewCheckRequest) (domain.ComponentCheck, error) {
	comp, err := s.store.GetComponent(ctx, componentID)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("add check: %w", err)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return domain.ComponentCheck{}, fmt.Errorf("%w: a check needs a name", ErrInvalidInput)
	}
	if !domain.ValidCheckPurpose(req.Purpose) {
		return domain.ComponentCheck{}, fmt.Errorf("%w: invalid check purpose %q", ErrInvalidInput, req.Purpose)
	}
	commands, err := validateLocalCommands(req.LocalCommands)
	if err != nil {
		return domain.ComponentCheck{}, err
	}

	gate := req.Gate
	switch {
	case gate == "" && len(commands) > 0:
		gate = domain.CheckGateRequired
	case gate == "":
		gate = domain.CheckGateInfo
	case !domain.ValidCheckGate(gate):
		return domain.ComponentCheck{}, fmt.Errorf("%w: invalid check gate %q", ErrInvalidInput, gate)
	}

	purpose := req.Purpose
	check := domain.ComponentCheck{
		ID:            uuid.New(),
		RepositoryID:  comp.RepositoryID,
		ComponentID:   componentID,
		Source:        domain.CheckSourceManual,
		JobKey:        newManualJobKey(),
		JobName:       name,
		Purpose:       domain.Fact[domain.CheckPurpose]{Override: &purpose},
		LocalCommands: domain.Fact[[]domain.LocalCommand]{Override: &commands},
		Gate:          domain.Fact[domain.CheckGate]{Override: &gate},
		Status:        domain.ModelStatusActive,
	}
	saved, err := s.store.SaveCheck(ctx, check)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("add check: %w", err)
	}
	s.logProjection(ctx, saved.RepositoryID)
	return saved, nil
}

func (s *Service) UpdateCheck(ctx context.Context, checkID uuid.UUID, patch domain.CheckPatch) (domain.ComponentCheck, error) {
	chk, err := s.store.GetCheck(ctx, checkID)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("update check: %w", err)
	}

	if patch.Purpose.Set {
		if patch.Purpose.Value != nil && !domain.ValidCheckPurpose(*patch.Purpose.Value) {
			return domain.ComponentCheck{}, fmt.Errorf("%w: invalid check purpose %q", ErrInvalidInput, *patch.Purpose.Value)
		}
		chk.Purpose = patch.Purpose.ApplyTo(chk.Purpose)
	}
	if patch.Gate.Set {
		if patch.Gate.Value != nil && !domain.ValidCheckGate(*patch.Gate.Value) {
			return domain.ComponentCheck{}, fmt.Errorf("%w: invalid check gate %q", ErrInvalidInput, *patch.Gate.Value)
		}
		chk.Gate = patch.Gate.ApplyTo(chk.Gate)
	}
	if patch.LocalCommands.Set {
		if patch.LocalCommands.Value != nil {
			validated, verr := validateLocalCommands(*patch.LocalCommands.Value)
			if verr != nil {
				return domain.ComponentCheck{}, verr
			}
			patch.LocalCommands.Value = &validated
		}
		chk.LocalCommands = patch.LocalCommands.ApplyTo(chk.LocalCommands)
	}
	if patch.Status != nil {
		if *patch.Status != domain.ModelStatusActive && *patch.Status != domain.ModelStatusDismissed {
			return domain.ComponentCheck{}, fmt.Errorf("%w: invalid status %q", ErrInvalidInput, *patch.Status)
		}
		chk.Status = *patch.Status
		if chk.Status == domain.ModelStatusDismissed {
			chk.NeedsReview = false
		}
	}
	if patch.Reviewed != nil && *patch.Reviewed {
		chk.NeedsReview = false
	}

	saved, err := s.store.SaveCheck(ctx, chk)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("update check: %w", err)
	}
	s.logProjection(ctx, saved.RepositoryID)
	return saved, nil
}

func (s *Service) DeleteCheck(ctx context.Context, checkID uuid.UUID) error {
	chk, err := s.store.GetCheck(ctx, checkID)
	if err != nil {
		return fmt.Errorf("delete check: %w", err)
	}
	if chk.Source != domain.CheckSourceManual {
		return fmt.Errorf("%w: dismiss CI checks instead", ErrInvalidInput)
	}
	if err := s.store.ApplyReconcile(ctx, port.ModelReconcile{RepositoryID: chk.RepositoryID, DeleteChecks: []uuid.UUID{checkID}}); err != nil {
		return fmt.Errorf("delete check: %w", err)
	}
	s.logProjection(ctx, chk.RepositoryID)
	return nil
}

func (s *Service) ensureUserResource(ctx context.Context, ref domain.ResourceRef) (domain.SystemResource, error) {
	if !domain.ValidResourceKind(ref.Kind) {
		return domain.SystemResource{}, fmt.Errorf("%w: invalid resource kind %q", ErrInvalidInput, ref.Kind)
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return domain.SystemResource{}, fmt.Errorf("%w: a resource needs a name", ErrInvalidInput)
	}
	vendor := strings.TrimSpace(ref.Vendor)
	part := slugify(vendor)
	if part == "" {
		part = slugify(name)
	}
	resource, err := s.store.EnsureResource(ctx, domain.SystemResource{
		Kind:        ref.Kind,
		Vendor:      vendor,
		Name:        name,
		IdentityKey: fmt.Sprintf("user:%s:%s", ref.Kind, part),
	})
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("ensure resource: %w", err)
	}
	return resource, nil
}

func (s *Service) AddLink(ctx context.Context, req domain.NewLinkRequest) (domain.ComponentLink, error) {
	from, err := s.store.GetComponent(ctx, req.FromComponentID)
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("add link: %w", err)
	}
	hasComponent, hasResource := req.ToComponentID != nil, req.ToResource != nil
	if hasComponent == hasResource {
		return domain.ComponentLink{}, fmt.Errorf("%w: exactly one of to_component_id or to_resource is required", ErrInvalidInput)
	}

	protocol := req.Protocol
	if protocol == "" {
		protocol = domain.LinkOther
	} else if !domain.ValidLinkProtocol(protocol) {
		return domain.ComponentLink{}, fmt.Errorf("%w: invalid protocol %q", ErrInvalidInput, protocol)
	}

	link := domain.ComponentLink{
		ID:              uuid.New(),
		RepositoryID:    from.RepositoryID,
		FromComponentID: from.ID,
		Protocol:        protocol,
		Detail:          req.Detail,
		Confidence:      domain.ConfidenceExact,
		Status:          domain.LinkConfirmed,
		Source:          domain.LinkSourceUser,
	}

	if hasComponent {
		if *req.ToComponentID == from.ID {
			return domain.ComponentLink{}, fmt.Errorf("%w: a link cannot target its own component", ErrInvalidInput)
		}
		if _, err := s.store.GetComponent(ctx, *req.ToComponentID); err != nil {
			return domain.ComponentLink{}, fmt.Errorf("add link: %w", err)
		}
		link.ToComponentID = req.ToComponentID
	} else {
		resource, err := s.ensureUserResource(ctx, *req.ToResource)
		if err != nil {
			return domain.ComponentLink{}, err
		}
		link.ToResourceID = &resource.ID
	}

	saved, err := s.store.SaveLink(ctx, link)
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("add link: %w", err)
	}
	return saved, nil
}

func (s *Service) UpdateLink(ctx context.Context, linkID uuid.UUID, patch domain.LinkPatch) (domain.ComponentLink, error) {
	link, err := s.store.GetLink(ctx, linkID)
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("update link: %w", err)
	}

	retargeted := false
	switch {
	case patch.ToComponentID != nil:
		if *patch.ToComponentID == link.FromComponentID {
			return domain.ComponentLink{}, fmt.Errorf("%w: a link cannot target its own component", ErrInvalidInput)
		}
		if _, err := s.store.GetComponent(ctx, *patch.ToComponentID); err != nil {
			return domain.ComponentLink{}, fmt.Errorf("update link: %w", err)
		}
		link.ToComponentID = patch.ToComponentID
		link.ToResourceID = nil
		link.AutoConfirmed = false
		link.Reason = ""
		retargeted = true
	case patch.ToResource != nil:
		resource, err := s.ensureUserResource(ctx, *patch.ToResource)
		if err != nil {
			return domain.ComponentLink{}, err
		}
		link.ToResourceID = &resource.ID
		link.ToComponentID = nil
		link.AutoConfirmed = false
		link.Reason = ""
		retargeted = true
	}

	if patch.Status != nil {
		if *patch.Status != domain.LinkSuggested && *patch.Status != domain.LinkConfirmed && *patch.Status != domain.LinkDismissed {
			return domain.ComponentLink{}, fmt.Errorf("%w: invalid link status %q", ErrInvalidInput, *patch.Status)
		}
		link.Status = *patch.Status
	} else if retargeted {
		link.Status = domain.LinkConfirmed
	}

	if patch.Protocol != nil {
		if !domain.ValidLinkProtocol(*patch.Protocol) {
			return domain.ComponentLink{}, fmt.Errorf("%w: invalid protocol %q", ErrInvalidInput, *patch.Protocol)
		}
		link.Protocol = *patch.Protocol
	}

	saved, err := s.store.SaveLink(ctx, link)
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("update link: %w", err)
	}
	return saved, nil
}

func (s *Service) DeleteLink(ctx context.Context, linkID uuid.UUID) error {
	link, err := s.store.GetLink(ctx, linkID)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if link.Source != domain.LinkSourceUser {
		return fmt.Errorf("%w: dismiss detected links instead", ErrInvalidInput)
	}
	if err := s.store.DeleteLink(ctx, linkID); err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	return nil
}
