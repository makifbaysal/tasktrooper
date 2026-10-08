package mcp

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) SeedDefaultsIfEmpty(ctx context.Context) error {
	count, err := s.store.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	for _, template := range domain.MCPTemplates() {
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
		server = stripSecretsFromStored(server, template.ID)
		if _, err := s.store.Create(ctx, server); err != nil {
			return fmt.Errorf("seed mcp server %s: %w", template.ID, err)
		}
	}
	return s.reloadStored(ctx)
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
