package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
		if template.Enabled && domain.IsArtCraftApp(template.ID) {
			given = append(given, artCraftOnMark(template.ID))
		}
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

// artCraftOnMark records, in the seeded-templates list, that an ArtCraft
// server was switched on once its CLI was found; after that its on/off is the
// person's and only its path is kept current.
func artCraftOnMark(id string) string { return id + "@on" }

// ReconcileArtCraft points every ArtCraft server at the CLI this computer
// has now — installed after the template was seeded, by the ArtCraft
// Launcher, an app bundle or this app's toolchain installer, or moved — and
// switches each on the first time its CLI is found.
func (s *Service) ReconcileArtCraft(ctx context.Context) error {
	seededIDs, err := s.store.SeededTemplateIDs(ctx)
	if err != nil {
		return err
	}
	marked := make(map[string]bool, len(seededIDs))
	for _, id := range seededIDs {
		marked[id] = true
	}
	servers, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	var newlyOn []string
	changed := false
	for _, server := range servers {
		if !domain.IsArtCraftApp(server.ID) || server.Transport != "stdio" {
			continue
		}
		found := domain.ArtCraftCLI(server.ID)
		if !commandRuns(found) {
			continue
		}
		update := server
		if !commandRuns(server.Command) && server.Command != found {
			update.Command = found
		}
		if !marked[artCraftOnMark(server.ID)] {
			update.Enabled = true
			newlyOn = append(newlyOn, artCraftOnMark(server.ID))
		}
		if update.Command == server.Command && update.Enabled == server.Enabled {
			continue
		}
		if _, err := s.store.Update(ctx, update); err != nil {
			return fmt.Errorf("update mcp server %s: %w", server.ID, err)
		}
		changed = true
	}
	if len(newlyOn) > 0 {
		if err := s.store.MarkTemplatesSeeded(ctx, newlyOn); err != nil {
			return err
		}
	}
	if !changed {
		return nil
	}
	return s.reloadStored(ctx)
}

func commandRuns(command string) bool {
	if command == "" {
		return false
	}
	if filepath.IsAbs(command) {
		info, err := os.Stat(command)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(command)
	return err == nil
}
