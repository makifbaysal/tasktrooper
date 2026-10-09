package catalogrepo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SkillVectorsFile sits at the catalog root, next to agents/: the vectors
// describe that catalog's skills, so a catalog read from git brings its own.
const SkillVectorsFile = "skills.vectors.json"

const (
	skillVectorsFormat   = 1
	skillVectorsKey      = `sha256(name + "\n" + description + "\n" + content), hex`
	skillVectorsEncoding = "base64 of little-endian float32"
)

type skillVectorsDoc struct {
	Format   int               `json:"format"`
	Model    string            `json:"model"`
	Source   string            `json:"source"`
	Dims     int               `json:"dims"`
	Key      string            `json:"key"`
	Encoding string            `json:"encoding"`
	Vectors  map[string]string `json:"vectors"`
}

func ParseSkillVectors(raw []byte) (*domain.SkillVectorSet, error) {
	var doc skillVectorsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", SkillVectorsFile, err)
	}
	if doc.Format != skillVectorsFormat {
		return nil, fmt.Errorf("%s: format %d, this build reads %d", SkillVectorsFile, doc.Format, skillVectorsFormat)
	}
	if doc.Model == "" || doc.Source == "" || doc.Dims <= 0 {
		return nil, fmt.Errorf("%s: model, source and dims are required", SkillVectorsFile)
	}
	set := domain.NewSkillVectorSet(doc.Model, doc.Source, doc.Dims)
	for key, encoded := range doc.Vectors {
		vec, err := decodeVector(encoded, doc.Dims)
		if err != nil {
			return nil, fmt.Errorf("%s: vector %s: %w", SkillVectorsFile, key, err)
		}
		set.Put(key, vec)
	}
	return set, nil
}

// EncodeSkillVectors writes one vector per line in key order, so a catalog
// change shows up in a diff as the lines of the skills it touched.
func EncodeSkillVectors(set *domain.SkillVectorSet) ([]byte, error) {
	if set == nil || set.Model == "" || set.Source == "" || set.Dims <= 0 {
		return nil, errors.New("skill vectors need a model, a source and dims")
	}
	vectors := make(map[string]string, set.Len())
	for _, key := range set.Keys() {
		vec, _ := set.Vector(key)
		if len(vec) != set.Dims {
			return nil, fmt.Errorf("vector %s has %d dimensions, want %d", key, len(vec), set.Dims)
		}
		vectors[key] = encodeVector(vec)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	err := enc.Encode(skillVectorsDoc{
		Format: skillVectorsFormat, Model: set.Model, Source: set.Source, Dims: set.Dims,
		Key: skillVectorsKey, Encoding: skillVectorsEncoding, Vectors: vectors,
	})
	return buf.Bytes(), err
}

func encodeVector(vec []float32) string {
	raw := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(raw[4*i:], math.Float32bits(v))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func decodeVector(encoded string, dims int) ([]float32, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(raw) != 4*dims {
		return nil, fmt.Errorf("%d bytes, want %d", len(raw), 4*dims)
	}
	vec := make([]float32, dims)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return vec, nil
}

// skillVectorsCache rereads the file only when it changed on disk: the
// catalog service asks once per skill it embeds.
type skillVectorsCache struct {
	mu      sync.Mutex
	path    string
	modTime time.Time
	size    int64
	set     *domain.SkillVectorSet
}

// SkillVectors reads the vectors shipped beside the catalog this reader
// syncs: in place for a directory source, from the last checkout for a git
// one. A catalog without the file has none, which is not an error.
func (r *Reader) SkillVectors(_ context.Context) (*domain.SkillVectorSet, error) {
	dir := r.Source
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = r.CacheDir
	}
	if dir == "" {
		return nil, nil
	}
	return r.vectors.load(filepath.Join(dir, SkillVectorsFile))
}

func (c *skillVectorsCache) load(path string) (*domain.SkillVectorSet, error) {
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == path && c.modTime.Equal(st.ModTime()) && c.size == st.Size() {
		return c.set, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set, err := ParseSkillVectors(raw)
	if err != nil {
		return nil, err
	}
	c.path, c.modTime, c.size, c.set = path, st.ModTime(), st.Size(), set
	return set, nil
}
