package ghrunner

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Version is the runner a first install downloads. GitHub stops sending jobs
// to a runner that falls too far behind, and the runner updates itself when
// GitHub asks, so the pin decides only where an install starts.
const Version = "2.338.0"

// Distribution is one runner package: where it is and the digest GitHub
// published for it in the release notes.
type Distribution struct {
	URL    string
	SHA256 string
	Zip    bool
}

var checksums = map[string]string{
	"osx-arm64":   "df4cebda25c86a886ed204e49fee63f5c2e7cec5f447b5c98440a826bbdf9df2",
	"osx-x64":     "dea7a58796ce215fc424a8b27c0fdd9b813fa5fc03d109a4a1d38131d6bfa1a3",
	"linux-x64":   "af4b794c1bc41d73d40535e3fe092a39f9679cd8d965954c2aca25a05ca41d32",
	"linux-arm64": "628b4a7258487b80c1d3c221095a7ded349f5ae0175fd3dfab708d07428f041b",
	"win-x64":     "f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f",
	"win-arm64":   "a57a4d29c0dcf02210a16b01906ed76cb8091b26d728c41232736c0f627a557e",
}

func platformOf(goos, goarch string) (string, bool) {
	var osName, arch string
	switch goos {
	case "darwin":
		osName = "osx"
	case "linux":
		osName = "linux"
	case "windows":
		osName = "win"
	default:
		return "", false
	}
	switch goarch {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		return "", false
	}
	p := osName + "-" + arch
	_, ok := checksums[p]
	return p, ok
}

func distributionFor(goos, goarch string) (Distribution, bool) {
	p, ok := platformOf(goos, goarch)
	if !ok {
		return Distribution{}, false
	}
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	name := fmt.Sprintf("actions-runner-%s-%s.%s", p, Version, ext)
	return Distribution{
		URL:    "https://github.com/actions/runner/releases/download/v" + Version + "/" + name,
		SHA256: checksums[p],
		Zip:    goos == "windows",
	}, true
}

// Labels are the ones GitHub gives a runner it registers itself; a JIT
// runner gets only the labels it is created with, so they are spelled out.
func labelsFor(goos, goarch string) []string {
	osLabel := map[string]string{"darwin": "macOS", "linux": "Linux", "windows": "Windows"}[goos]
	archLabel := map[string]string{"amd64": "X64", "arm64": "ARM64"}[goarch]
	return []string{"self-hosted", osLabel, archLabel, "tasktrooper"}
}

// fetch puts d's package in dir, downloading it unless a copy whose digest
// matches is already there, and removes any other package left in dir.
func fetch(ctx context.Context, client *http.Client, d Distribution, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, path.Base(d.URL))
	if sum, err := fileSHA256(dest); err == nil && sum == d.SHA256 {
		return dest, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", d.URL, resp.Status)
	}
	part := dest + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(part)
		return "", errors.Join(copyErr, closeErr)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != d.SHA256 {
		_ = os.Remove(part)
		return "", fmt.Errorf("download %s: sha256 %s, want %s", d.URL, got, d.SHA256)
	}
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.Name() != filepath.Base(dest) {
				_ = os.RemoveAll(filepath.Join(dir, e.Name()))
			}
		}
	}
	return dest, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

const installedMarker = ".tasktrooper-installed"

// install unpacks archive into dir once. It unpacks beside dir and renames,
// so a crash halfway leaves no directory that looks installed.
func install(archive string, isZip bool, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, installedMarker)); err == nil {
		return nil
	}
	tmp := dir + ".unpacking"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	var err error
	if isZip {
		err = unzip(archive, tmp)
	} else {
		err = untar(archive, tmp)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(tmp, installedMarker), []byte(Version+"\n"), 0o600)
	}
	if err == nil {
		_ = os.RemoveAll(dir)
		err = os.Rename(tmp, dir)
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
	}
	return err
}

// within resolves an archive entry's name under root. A name that is not
// local to the archive (absolute, a volume, any ".." component) is refused;
// the runner's packages have none.
func within(root, name string) (string, error) {
	rel := filepath.FromSlash(strings.TrimSuffix(name, "/"))
	rel = strings.TrimPrefix(rel, "."+string(filepath.Separator))
	if strings.Contains(name, "..") || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("archive entry %q escapes the install directory", name)
	}
	return filepath.Join(root, rel), nil
}

type pendingLink struct {
	name, target, link string
}

// untar writes directories and files first and the symlinks last, so nothing
// is ever written through a link. A link must sit under real directories,
// and once made it must resolve to something inside dest; the runner's
// packages carry six, node's bin/npm -> ../lib/... and their kind.
func untar(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var links []pendingLink
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Name == "./" || hdr.Name == "." {
			continue
		}
		target, err := within(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(target, tr, os.FileMode(hdr.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) || filepath.VolumeName(hdr.Linkname) != "" {
				return fmt.Errorf("archive symlink %q points outside the install directory", hdr.Name)
			}
			links = append(links, pendingLink{name: hdr.Name, target: target, link: hdr.Linkname})
		default:
			return fmt.Errorf("archive entry %q is of a kind the runner package does not ship (%c)", hdr.Name, hdr.Typeflag)
		}
	}
	return makeLinks(dest, links)
}

func makeLinks(dest string, links []pendingLink) error {
	root, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	for _, l := range links {
		parent, err := filepath.EvalSymlinks(filepath.Dir(l.target))
		if err != nil {
			if err := os.MkdirAll(filepath.Dir(l.target), 0o755); err != nil {
				return err
			}
			if parent, err = filepath.EvalSymlinks(filepath.Dir(l.target)); err != nil {
				return err
			}
		}
		if parent != filepath.Join(root, mustRel(dest, filepath.Dir(l.target))) {
			return fmt.Errorf("archive symlink %q sits under another symlink", l.name)
		}
		if err := os.Symlink(filepath.FromSlash(l.link), l.target); err != nil {
			return err
		}
	}
	for _, l := range links {
		resolved, err := filepath.EvalSymlinks(l.target)
		if err != nil || !isUnder(root, resolved) {
			return fmt.Errorf("archive symlink %q points outside the install directory", l.name)
		}
	}
	return nil
}

func mustRel(base, p string) string {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return rel
}

func isUnder(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// unzip is the Windows package, which carries files and directories only.
func unzip(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		target, err := within(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !zf.Mode().IsRegular() {
			return fmt.Errorf("archive entry %q is of a kind the runner package does not ship", zf.Name)
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, 0o644)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFile(target string, r io.Reader, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm|0o200)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	return errors.Join(copyErr, f.Close())
}
