package github

import (
	"context"
	"fmt"
	"net/url"
	"sort"
)

type repoHook struct {
	ID     int64    `json:"id"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

// "issues" lets a labelled issue become a task the moment it is labelled; the
// issue-sync poller covers installs whose webhooks cannot reach this server.
var WebhookEvents = []string{"push", "workflow_run", "check_suite", "issues"}

func EnsureRepoWebhook(ctx context.Context, token, owner, repo, targetURL, secret string) (int64, error) {
	return ensureRepoWebhookAt(ctx, "", token, owner, repo, targetURL, secret)
}

func EnsureRepoWebhookAt(ctx context.Context, base, token, owner, repo, targetURL, secret string) (int64, error) {
	return ensureRepoWebhookAt(ctx, base, token, owner, repo, targetURL, secret)
}

func ensureRepoWebhookAt(ctx context.Context, base, token, owner, repo, targetURL, secret string) (int64, error) {
	hooksPath := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/hooks"

	var existing []repoHook
	if err := doJSONAt(ctx, base, token, "GET", hooksPath+"?per_page=100", nil, &existing); err != nil {
		return 0, fmt.Errorf("list repo hooks: %w", err)
	}

	config := map[string]any{
		"url":          targetURL,
		"content_type": "json",
		"secret":       secret,
		"insecure_ssl": "0",
	}
	for _, h := range existing {
		if h.Config.URL != targetURL {
			continue
		}
		var updated repoHook
		body := map[string]any{"active": true, "events": WebhookEvents, "config": config}
		if err := doJSONAt(ctx, base, token, "PATCH", fmt.Sprintf("%s/%d", hooksPath, h.ID), body, &updated); err != nil {
			return 0, fmt.Errorf("update repo hook: %w", err)
		}
		return updated.ID, nil
	}

	var created repoHook
	body := map[string]any{
		"name":   "web",
		"active": true,
		"events": WebhookEvents,
		"config": config,
	}
	if err := doJSONAt(ctx, base, token, "POST", hooksPath, body, &created); err != nil {
		return 0, fmt.Errorf("create repo hook: %w", err)
	}
	return created.ID, nil
}

func ReconcileRepoWebhookEvents(ctx context.Context, token, owner, repo, targetURL string) (int64, bool, error) {
	return reconcileRepoWebhookEventsAt(ctx, "", token, owner, repo, targetURL)
}

func reconcileRepoWebhookEventsAt(ctx context.Context, base, token, owner, repo, targetURL string) (int64, bool, error) {
	hooksPath := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/hooks"

	var existing []repoHook
	if err := doJSONAt(ctx, base, token, "GET", hooksPath+"?per_page=100", nil, &existing); err != nil {
		return 0, false, fmt.Errorf("list repo hooks: %w", err)
	}
	for _, h := range existing {
		if h.Config.URL != targetURL {
			continue
		}
		if h.Active && hasAllEvents(h.Events, WebhookEvents) {
			return h.ID, false, nil
		}
		var updated repoHook

		body := map[string]any{"active": true, "events": unionEvents(h.Events, WebhookEvents)}
		if err := doJSONAt(ctx, base, token, "PATCH", fmt.Sprintf("%s/%d", hooksPath, h.ID), body, &updated); err != nil {
			return h.ID, false, fmt.Errorf("update repo hook events: %w", err)
		}
		return updated.ID, true, nil
	}
	return 0, false, nil
}

func hasAllEvents(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, e := range have {
		set[e] = true
	}

	if set["*"] {
		return true
	}
	for _, e := range want {
		if !set[e] {
			return false
		}
	}
	return true
}

func unionEvents(have, want []string) []string {
	set := make(map[string]bool, len(have)+len(want))
	for _, e := range have {
		set[e] = true
	}
	for _, e := range want {
		set[e] = true
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}
