package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SeedNewTemplates adds every shipped template this install has not been
// given yet, on every boot. That covers a first launch (nothing given) and a
// release that ships a new template, without re-adding a seeded server the
// user deleted: what was given is recorded, not inferred from what is on file.
func (s *Service) SeedNewTemplates(ctx context.Context) error {
	seededIDs, err := s.store.SeededTemplateIDs(ctx)
	if err != nil {
		return err
	}
	seeded := make(map[string]bool, len(seededIDs))
	for _, id := range seededIDs {
		seeded[id] = true
	}
	servers, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	onFile := make(map[string]bool, len(servers))
	for _, server := range servers {
		onFile[server.ID] = true
	}

	var given []string
	created := false
	for _, template := range domain.MCPTemplates() {
		if seeded[template.ID] {
			continue
		}
		given = append(given, template.ID)
		// A server someone added under the same id stays as they made it.
		if onFile[template.ID] {
			continue
		}
		_, err := s.store.Create(ctx, serverFromTemplate(template))
		if errors.Is(err, domain.ErrMCPServerAlreadyExists) {
			continue
		}
		if err != nil {
			return fmt.Errorf("seed mcp server %s: %w", template.ID, err)
		}
		created = true
	}
	if err := s.store.MarkTemplatesSeeded(ctx, given); err != nil {
		return err
	}
	if !created {
		return nil
	}
	return s.reloadStored(ctx)
}

func serverFromTemplate(template domain.MCPTemplate) domain.MCPServer {
	server := domain.MCPServer{
		ID:           template.ID,
		Enabled:      template.Enabled,
		Transport:    template.Transport,
		Command:      template.Command,
		Args:         append([]string(nil), template.Args...),
		Env:          cloneStringMap(template.Env),
		URL:          template.URL,
		Headers:      cloneStringMap(template.Headers),
		AllowedTools: append([]string(nil), template.AllowedTools...),
		// The shipped catalog keeps reaching every agent, exactly as it
		// does on an install upgraded past migration 179; only servers a
		// person adds, and the templates that drive an editor, a kernel or
		// a scanner for one agent, start out "listed".
		Access: template.Access.Effective(),
	}
	return stripSecretsFromStored(server, template.ID)
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
