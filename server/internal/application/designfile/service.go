// Package designfile keeps the FigCraft files design tasks work in. Every
// design task draws in its own file, seeded from the design system file it
// builds on; a design-system task's file becomes the project's (or the
// repository's) design system file when the human approves the task. The
// files live in domain.DesignFilesDir, so the human can open them in FigCraft
// and the ArtCraft MCP servers may write them.
package designfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const Extension = ".figcraft"

var ErrNotInstalled = errors.New("FigCraft is not installed on this computer")
var ErrNoFile = errors.New("no design file yet")
var ErrUnknownPage = errors.New("no such page in the design file")

// Tool reads, renders and opens FigCraft files.
type Tool interface {
	Installed() bool
	Info(ctx context.Context, file string) (Info, error)
	RenderPage(ctx context.Context, file, page, out string, scale float64) error
	Open(ctx context.Context, file string) error
	Reveal(ctx context.Context, file string) error
}

type Frame struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Page struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Frames []Frame `json:"frames"`
}

type Info struct {
	Name  string `json:"name"`
	Pages []Page `json:"pages"`
}

// Manifest is what a design file view shows: where the file is, whether it
// exists yet and its pages.
type Manifest struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Installed bool   `json:"installed"`
	Name      string `json:"name,omitempty"`
	Pages     []Page `json:"pages"`
	Modified  int64  `json:"modified,omitempty"`
	Error     string `json:"error,omitempty"`
}

type BaseProjects interface {
	BaseProject(ctx context.Context, repositoryID uuid.UUID) (*uuid.UUID, error)
}

type Service struct {
	root  string
	tool  Tool
	bases BaseProjects
	mu    sync.Mutex
}

func NewService(root string, tool Tool, bases BaseProjects) *Service {
	return &Service{root: filepath.Clean(root), tool: tool, bases: bases}
}

// contain returns file cleaned when it lies under the service's root, and
// false otherwise. Every path the service reads, writes or hands to FigCraft
// goes through it, so a crafted id or path never reaches outside the design
// files folder.
func (s *Service) contain(file string) (string, bool) {
	clean := filepath.Clean(file)
	if strings.Contains(clean, "..") || !strings.HasPrefix(clean, s.root+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

var errOutsideRoot = errors.New("design file path is outside the design files folder")

func (s *Service) Root() string { return s.root }

func (s *Service) Installed() bool { return s.tool != nil && s.tool.Installed() }

func (s *Service) ProjectFile(projectID uuid.UUID) string {
	return filepath.Join(s.root, "projects", projectID.String(), "design-system"+Extension)
}

func (s *Service) RepositoryFile(repositoryID uuid.UUID) string {
	return filepath.Join(s.root, "repositories", repositoryID.String(), "design-system"+Extension)
}

func (s *Service) TaskFile(taskID uuid.UUID) string {
	return filepath.Join(s.root, "tasks", taskID.String()+Extension)
}

// TaskFiles is what a design task works with.
type TaskFiles struct {
	Installed bool `json:"figcraft_installed"`
	// TaskFile is the file this task draws in; it is reviewed with the task.
	TaskFile string `json:"task_file"`
	// SeededFrom is the design system file TaskFile was copied from, "" when
	// the task started from an empty file.
	SeededFrom string `json:"seeded_from,omitempty"`
	// ProjectFile and RepositoryFile are the approved design system files; a
	// design-system task's TaskFile replaces the one its proposal targets when
	// the task is approved.
	ProjectFile    string `json:"project_design_system_file,omitempty"`
	ProjectExists  bool   `json:"project_design_system_file_exists"`
	RepositoryFile string `json:"repository_design_system_file"`
	RepoExists     bool   `json:"repository_design_system_file_exists"`
	TaskExists     bool   `json:"task_file_exists"`
}

// PrepareTask returns a design task's files, copying the design system file
// it builds on into the task's own file the first time.
func (s *Service) PrepareTask(ctx context.Context, repositoryID, taskID uuid.UUID) (TaskFiles, error) {
	out := TaskFiles{
		Installed:      s.Installed(),
		TaskFile:       s.TaskFile(taskID),
		RepositoryFile: s.RepositoryFile(repositoryID),
	}
	taskFile, ok := s.contain(out.TaskFile)
	repoFile, ok2 := s.contain(out.RepositoryFile)
	if !ok || !ok2 {
		return out, errOutsideRoot
	}
	out.RepoExists = fileExists(repoFile)
	if s.bases != nil {
		base, err := s.bases.BaseProject(ctx, repositoryID)
		if err != nil {
			return out, err
		}
		if base != nil {
			out.ProjectFile = s.ProjectFile(*base)
			if projectFile, ok := s.contain(out.ProjectFile); ok {
				out.ProjectFile = projectFile
				out.ProjectExists = fileExists(projectFile)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(taskFile), 0o755); err != nil {
		return out, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if fileExists(taskFile) {
		out.TaskExists = true
		return out, nil
	}
	seed := ""
	switch {
	case out.RepoExists:
		seed = repoFile
	case out.ProjectExists:
		seed = out.ProjectFile
	}
	if seed != "" {
		if err := copyFile(seed, taskFile); err != nil {
			return out, fmt.Errorf("seed the task's design file: %w", err)
		}
		out.SeededFrom = seed
		out.TaskExists = true
	}
	return out, nil
}

// Promote makes an approved design-system task's file the design system file
// of what each approved version targets.
func (s *Service) Promote(taskID uuid.UUID, approved []domain.DesignSystem) {
	src, ok := s.contain(s.TaskFile(taskID))
	if !ok || len(approved) == 0 || !fileExists(src) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ds := range approved {
		var dst string
		switch {
		case ds.Scope == domain.DesignSystemScopeProject && ds.ProjectID != nil:
			dst = s.ProjectFile(*ds.ProjectID)
		case ds.Scope == domain.DesignSystemScopeRepository && ds.RepositoryID != nil:
			dst = s.RepositoryFile(*ds.RepositoryID)
		default:
			continue
		}
		dst, ok := s.contain(dst)
		if !ok {
			continue
		}
		err := os.MkdirAll(filepath.Dir(dst), 0o755)
		if err == nil {
			err = copyFile(src, dst)
		}
		if err != nil {
			log.Warn().Err(err).Str("task_id", taskID.String()).Str("file", dst).Msg("design file: promote failed")
		}
	}
}

func (s *Service) Manifest(ctx context.Context, file string) Manifest {
	m := Manifest{Path: file, Installed: s.Installed(), Pages: []Page{}}
	file, ok := s.contain(file)
	if !ok {
		return m
	}
	st, err := os.Stat(file)
	if err != nil {
		return m
	}
	m.Exists = true
	m.Modified = st.ModTime().UnixMilli()
	if !m.Installed {
		return m
	}
	info, err := s.tool.Info(ctx, file)
	if err != nil {
		m.Error = err.Error()
		return m
	}
	m.Name = info.Name
	if info.Pages != nil {
		m.Pages = info.Pages
	}
	return m
}

// Preview renders one page of a file to PNG and returns the image's path. A
// render is kept until the file changes.
func (s *Service) Preview(ctx context.Context, file string, pageID int64, scale float64) (string, error) {
	if !s.Installed() {
		return "", ErrNotInstalled
	}
	file, ok := s.contain(file)
	if !ok {
		return "", ErrNoFile
	}
	st, err := os.Stat(file)
	if err != nil {
		return "", ErrNoFile
	}
	info, err := s.tool.Info(ctx, file)
	if err != nil {
		return "", err
	}
	pageName := ""
	for _, p := range info.Pages {
		if p.ID == pageID {
			pageName = p.Name
			break
		}
	}
	if pageName == "" {
		return "", ErrUnknownPage
	}
	if scale <= 0 || scale > 2 {
		scale = 1
	}
	sum := sha256.Sum256([]byte(file))
	key := fmt.Sprintf("%s-%d-%d-%s.png", hex.EncodeToString(sum[:8]), pageID, st.ModTime().UnixNano(), strconv.FormatFloat(scale, 'f', -1, 64))
	dir := filepath.Join(s.root, ".previews")
	out := filepath.Join(dir, key)
	if fileExists(out) {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	s.prunePreviews(dir, hex.EncodeToString(sum[:8])+"-"+strconv.FormatInt(pageID, 10)+"-")
	tmp := out + ".tmp.png"
	if err := s.tool.RenderPage(ctx, file, pageName, tmp, scale); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, out); err != nil {
		return "", err
	}
	return out, nil
}

func (s *Service) prunePreviews(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func (s *Service) Open(ctx context.Context, file string) error {
	file, ok := s.contain(file)
	if !ok || !fileExists(file) {
		return ErrNoFile
	}
	if s.tool == nil {
		return ErrNotInstalled
	}
	return s.tool.Open(ctx, file)
}

func (s *Service) Reveal(ctx context.Context, file string) error {
	file, ok := s.contain(file)
	if !ok || !fileExists(file) {
		return ErrNoFile
	}
	if s.tool == nil {
		return ErrNotInstalled
	}
	return s.tool.Reveal(ctx, file)
}

// Approver approves a design task's design system versions and then promotes
// its design file.
type Approver struct {
	Inner interface {
		ApproveForTask(ctx context.Context, taskID uuid.UUID) []domain.DesignSystem
	}
	Files *Service
}

func (a Approver) ApproveForTask(ctx context.Context, taskID uuid.UUID) []domain.DesignSystem {
	approved := a.Inner.ApproveForTask(ctx, taskID)
	if a.Files != nil {
		a.Files.Promote(taskID, approved)
	}
	return approved
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
