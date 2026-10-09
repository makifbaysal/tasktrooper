package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// SkillEmbeddingText is the exact text a skill's vector is computed from.
// The catalog service embeds it and the shipped skill vectors are keyed by its
// hash, so both sides must build it here.
func SkillEmbeddingText(name, description, content string) string {
	return name + "\n" + description + "\n" + content
}

// SkillVectorKey keys a shipped vector by the text it was computed from, not
// by skill name: an edited skill, or one a sync merged, misses and is embedded
// live, and copies of one skill under several agents share a vector.
func SkillVectorKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// SkillVectorSet is skill vectors embedded once, ahead of time, and shipped
// with the catalog. They stand in for a live embedding only for the engine
// that made them: Model and Source must both match what this install embeds
// queries with now, or a search would rank one engine's numbers against
// another's.
type SkillVectorSet struct {
	Model   string
	Source  string
	Dims    int
	vectors map[string][]float32
}

func NewSkillVectorSet(model, source string, dims int) *SkillVectorSet {
	return &SkillVectorSet{
		Model:   strings.TrimSpace(model),
		Source:  strings.TrimSpace(source),
		Dims:    dims,
		vectors: make(map[string][]float32),
	}
}

func (s *SkillVectorSet) Put(key string, vector []float32) {
	s.vectors[key] = append([]float32(nil), vector...)
}

func (s *SkillVectorSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.vectors)
}

func (s *SkillVectorSet) Keys() []string {
	if s == nil {
		return nil
	}
	keys := make([]string, 0, len(s.vectors))
	for k := range s.vectors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *SkillVectorSet) Vector(key string) ([]float32, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.vectors[key]
	return v, ok
}

// Lookup answers the shipped vector of text for an install that embeds with
// model on source. An unknown source never matches: a model name alone does
// not say two vectors are comparable.
func (s *SkillVectorSet) Lookup(model, source, text string) ([]float32, bool) {
	if s == nil || s.Model == "" || s.Source == "" {
		return nil, false
	}
	if strings.TrimSpace(model) != s.Model || strings.TrimSpace(source) != s.Source {
		return nil, false
	}
	v, ok := s.vectors[SkillVectorKey(text)]
	if !ok {
		return nil, false
	}
	return append([]float32(nil), v...), true
}
