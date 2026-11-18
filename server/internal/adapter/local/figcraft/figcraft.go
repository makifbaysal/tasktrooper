// Package figcraft runs the FigCraft CLI the ArtCraft Launcher installs, to
// read and render design files, and opens them in the FigCraft app.
package figcraft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/designfile"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type CLI struct {
	// Path is the figcraft-cli binary; empty means domain.ArtCraftCLI.
	Path string
	// ReadRoots and WriteRoots confine what the CLI may touch.
	ReadRoots  []string
	WriteRoots []string
}

var _ designfile.Tool = (*CLI)(nil)

func (c *CLI) bin() string {
	if c.Path != "" {
		return c.Path
	}
	return domain.ArtCraftCLI("figcraft")
}

func (c *CLI) Installed() bool {
	st, err := os.Stat(c.bin())
	return err == nil && !st.IsDir()
}

func (c *CLI) roots() []string {
	var args []string
	for _, r := range c.ReadRoots {
		if r != "" {
			args = append(args, "--automation-read-root", r)
		}
	}
	for _, r := range c.WriteRoots {
		if r != "" {
			args = append(args, "--automation-write-root", r)
		}
	}
	return args
}

func (c *CLI) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return nil, fmt.Errorf("figcraft-cli %s: %w: %s", args[0], err, msg)
	}
	return stdout.Bytes(), nil
}

type infoJSON struct {
	Name  string `json:"name"`
	Pages []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Frames []struct {
			ID     int64   `json:"id"`
			Name   string  `json:"name"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"frames"`
	} `json:"pages"`
}

func (c *CLI) Info(ctx context.Context, file string) (designfile.Info, error) {
	args := append([]string{"info", "--json"}, c.roots()...)
	raw, err := c.run(ctx, 60*time.Second, append(args, file)...)
	if err != nil {
		return designfile.Info{}, err
	}
	var parsed infoJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return designfile.Info{}, fmt.Errorf("figcraft-cli info: %w", err)
	}
	info := designfile.Info{Name: parsed.Name, Pages: make([]designfile.Page, 0, len(parsed.Pages))}
	for _, p := range parsed.Pages {
		page := designfile.Page{ID: p.ID, Name: p.Name, Frames: make([]designfile.Frame, 0, len(p.Frames))}
		for _, f := range p.Frames {
			page.Frames = append(page.Frames, designfile.Frame{ID: f.ID, Name: f.Name, Width: f.Width, Height: f.Height})
		}
		info.Pages = append(info.Pages, page)
	}
	return info, nil
}

func (c *CLI) RenderPage(ctx context.Context, file, page, out string, scale float64) error {
	args := append([]string{"render", "--page", page, "--scale", strconv.FormatFloat(scale, 'f', -1, 64), "-o", out}, c.roots()...)
	_, err := c.run(ctx, 3*time.Minute, append(args, file)...)
	return err
}

// Open shows the file in the FigCraft app, falling back to the system's
// handler for .figcraft files when the app is not where the launcher puts it.
func (c *CLI) Open(ctx context.Context, file string) error {
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat("/Applications/FigCraft.app"); err == nil {
			return exec.CommandContext(ctx, "open", "-a", "FigCraft", file).Run()
		}
		return exec.CommandContext(ctx, "open", file).Run()
	case "windows":
		return exec.CommandContext(ctx, "explorer", file).Start()
	default:
		return exec.CommandContext(ctx, "xdg-open", file).Start()
	}
}

func (c *CLI) Reveal(ctx context.Context, file string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", "-R", file).Run()
	case "windows":
		return exec.CommandContext(ctx, "explorer", "/select,", file).Start()
	default:
		return exec.CommandContext(ctx, "xdg-open", filepath.Dir(file)).Start()
	}
}
