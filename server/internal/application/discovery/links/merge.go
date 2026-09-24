package links

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// rawSignal is a link before the final sort; family lets a later signal
// (an env var) find and extend an earlier one (a manifest dependency) for the
// same resource in the same component instead of creating a second link.
type rawSignal struct {
	componentPath string
	signalKey     string
	protocol      domain.LinkProtocol
	detail        string
	target        domain.LinkTarget
	envVars       []string
	evidence      []domain.SourceEvidence
	confidence    domain.Confidence
	family        string
}

type collector struct {
	links       map[string]*rawSignal
	order       []string
	families    map[string]string
	deploy      map[string]*domain.DeploySignal
	deployOrder []string
	warnings    []string
}

func newCollector() *collector {
	return &collector{
		links:    map[string]*rawSignal{},
		families: map[string]string{},
		deploy:   map[string]*domain.DeploySignal{},
	}
}

func linkKey(componentPath, signalKey string) string { return componentPath + "\x00" + signalKey }

func familyIndexKey(componentPath, family string) string { return componentPath + "\x00" + family }

// familyOf groups links by the resource they represent so an env var can
// find "the existing link for that resource" regardless of which signal
// created it; a generic/unset vendor collapses onto the bare kind so
// DATABASE_URL can still find a postgres link created from go.mod.
func familyOf(kind domain.ResourceKind, vendor string) string {
	if vendor == "" || vendor == "database" {
		return string(kind)
	}
	return string(kind) + ":" + vendor
}

// addLink creates the link for (componentPath, signalKey) if this is the
// first signal to claim it, otherwise folds the new evidence and env vars
// into what is already there.
func (c *collector) addLink(sig rawSignal) *rawSignal {
	key := linkKey(sig.componentPath, sig.signalKey)
	if existing, ok := c.links[key]; ok {
		existing.mergeEnvVars(sig.envVars)
		existing.mergeEvidence(sig.evidence)
		return existing
	}
	cp := &rawSignal{
		componentPath: sig.componentPath,
		signalKey:     sig.signalKey,
		protocol:      sig.protocol,
		detail:        sig.detail,
		target:        sig.target,
		confidence:    sig.confidence,
		family:        sig.family,
	}
	cp.mergeEnvVars(sig.envVars)
	cp.mergeEvidence(sig.evidence)
	c.links[key] = cp
	c.order = append(c.order, key)
	if cp.family != "" {
		fk := familyIndexKey(cp.componentPath, cp.family)
		if _, taken := c.families[fk]; !taken {
			c.families[fk] = cp.signalKey
		}
	}
	return cp
}

// findByFamily returns the link already collected for (componentPath,
// family), so a caller can extend it instead of creating a duplicate.
func (c *collector) findByFamily(componentPath, family string) (*rawSignal, bool) {
	if signalKey, ok := c.families[familyIndexKey(componentPath, family)]; ok {
		sig, ok := c.links[linkKey(componentPath, signalKey)]
		return sig, ok
	}
	if strings.Contains(family, ":") {
		return nil, false
	}
	// A generic family (a DATABASE_URL with no scheme) belongs to the one
	// specific link of that kind the manifests already found, if exactly one.
	prefix := familyIndexKey(componentPath, family+":")
	var match string
	for key, signalKey := range c.families {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if match != "" && match != signalKey {
			return nil, false
		}
		match = signalKey
	}
	if match == "" {
		return nil, false
	}
	sig, ok := c.links[linkKey(componentPath, match)]
	return sig, ok
}

// link looks up an already-collected link by its exact signal key, for a
// caller that wants to upgrade fields addLink never overwrites (e.g. schema.go
// setting the engine on a generic "dep:prisma" placeholder).
func (c *collector) link(componentPath, signalKey string) (*rawSignal, bool) {
	sig, ok := c.links[linkKey(componentPath, signalKey)]
	return sig, ok
}

func (c *collector) warn(msg string) { c.warnings = append(c.warnings, msg) }

func (c *collector) addDeploySignal(sig domain.DeploySignal) {
	key := strings.Join([]string{
		sig.ComponentPath, sig.Provider, string(sig.Environment), refKey(sig.Ref),
	}, "\x00")
	if existing, ok := c.deploy[key]; ok {
		existing.Evidence = mergeEvidence(existing.Evidence, sig.Evidence)
		return
	}
	cp := sig
	c.deploy[key] = &cp
	c.deployOrder = append(c.deployOrder, key)
}

func refKey(ref map[string]string) string {
	if len(ref) == 0 {
		return ""
	}
	keys := make([]string, 0, len(ref))
	for k := range ref {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+ref[k])
	}
	return strings.Join(parts, "&")
}

func (s *rawSignal) mergeEnvVars(vars []string) {
	for _, v := range vars {
		if !containsStr(s.envVars, v) {
			s.envVars = append(s.envVars, v)
		}
	}
}

func (s *rawSignal) mergeEvidence(ev []domain.SourceEvidence) {
	s.evidence = mergeEvidence(s.evidence, ev)
}

func mergeEvidence(existing, add []domain.SourceEvidence) []domain.SourceEvidence {
	for _, e := range add {
		dup := false
		for _, have := range existing {
			if have == e {
				dup = true
				break
			}
		}
		if !dup {
			existing = append(existing, e)
		}
	}
	return existing
}

func containsStr(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func (c *collector) result() Result {
	links := make([]domain.DetectedLink, 0, len(c.order))
	for _, key := range c.order {
		sig := c.links[key]
		links = append(links, domain.DetectedLink{
			ComponentPath: sig.componentPath,
			SignalKey:     sig.signalKey,
			Protocol:      sig.protocol,
			Detail:        sig.detail,
			Target:        sig.target,
			EnvVars:       sig.envVars,
			Evidence:      sig.evidence,
			Confidence:    sig.confidence,
		})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].ComponentPath != links[j].ComponentPath {
			return links[i].ComponentPath < links[j].ComponentPath
		}
		return links[i].SignalKey < links[j].SignalKey
	})

	deploy := make([]domain.DeploySignal, 0, len(c.deployOrder))
	for _, key := range c.deployOrder {
		deploy = append(deploy, *c.deploy[key])
	}
	sort.Slice(deploy, func(i, j int) bool {
		if deploy[i].ComponentPath != deploy[j].ComponentPath {
			return deploy[i].ComponentPath < deploy[j].ComponentPath
		}
		if deploy[i].Provider != deploy[j].Provider {
			return deploy[i].Provider < deploy[j].Provider
		}
		return deploy[i].Environment < deploy[j].Environment
	})

	return Result{Links: links, DeploySignals: deploy, Warnings: c.warnings}
}
