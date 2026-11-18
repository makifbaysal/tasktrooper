package domain

import (
	"os"
	"path/filepath"
	"runtime"
)

// ArtCraft (getartcraft.com) is a suite of native creative apps — FigCraft for
// interface design, VectorCraft, PhotoCraft, and the motion, audio, video and
// CAD apps. The ArtCraft Launcher installs each app's CLI under
// ~/artCraft/<app>-cli/<app>-cli, and every CLI serves MCP on stdio with
// `<app>-cli mcp`. A template starts switched on when that CLI is installed
// on this computer, and every one is "listed": it reaches only the agents that
// name it (the ui-designer names figcraft, vectorcraft and photocraft).

// ArtCraftDir is where the ArtCraft Launcher installs the apps' CLIs.
func ArtCraftDir() string {
	if dir := os.Getenv("TASKTROOPER_ARTCRAFT_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "artCraft"
	}
	return filepath.Join(home, "artCraft")
}

// ArtCraftCLI is the path of one ArtCraft app's CLI, e.g. "figcraft".
func ArtCraftCLI(app string) string {
	name := app + "-cli"
	if runtime.GOOS == "windows" {
		return filepath.Join(ArtCraftDir(), name, name+".exe")
	}
	return filepath.Join(ArtCraftDir(), name, name)
}

// ArtCraftInstalled reports whether an ArtCraft app's CLI is on this computer.
func ArtCraftInstalled(app string) bool {
	info, err := os.Stat(ArtCraftCLI(app))
	return err == nil && !info.IsDir()
}

// DesignFilesDir is where design tasks keep their design files (.figcraft and
// the other ArtCraft documents): a folder the human can find in Finder, that
// survives the task's workspace, and the one folder the ArtCraft servers may
// write to.
func DesignFilesDir() string {
	if dir := os.Getenv("TASKTROOPER_DESIGNS_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "TaskTrooper Designs")
	}
	return filepath.Join(home, "Documents", "TaskTrooper Designs")
}

// EnsureDesignFilesDir creates DesignFilesDir: the ArtCraft servers refuse a
// root that does not exist.
func EnsureDesignFilesDir() error {
	return os.MkdirAll(DesignFilesDir(), 0o755)
}

func homeOrTemp() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return os.TempDir()
}

// artCraftRoots confines an ArtCraft server's file access: it may read the
// home folder (a repository's checkout and its icons and images live there)
// and write only the design files folder.
func artCraftRoots() []string {
	return []string{
		"--automation-read-root", homeOrTemp(),
		"--automation-write-root", DesignFilesDir(),
	}
}

func artCraftArgs(extra ...string) []string {
	return append([]string{"mcp"}, extra...)
}

func artCraftTemplates() []MCPTemplate {
	roots := artCraftRoots()
	withRoots := func(extra ...string) []string {
		return artCraftArgs(append(extra, roots...)...)
	}
	return []MCPTemplate{
		{
			ID: "figcraft", Access: MCPAccessListed, Label: "FigCraft (ArtCraft)", Description: "Interface design: frames, auto layout, components, variables, prototypes",
			Enabled: ArtCraftInstalled("figcraft"), Transport: "stdio", Command: ArtCraftCLI("figcraft"),
			Args: withRoots(),
		},
		{
			ID: "vectorcraft", Access: MCPAccessListed, Label: "VectorCraft (ArtCraft)", Description: "Vector illustration: icons, logos, illustrations",
			Enabled: ArtCraftInstalled("vectorcraft"), Transport: "stdio", Command: ArtCraftCLI("vectorcraft"),
			Args: withRoots("--headless"),
		},
		{
			ID: "photocraft", Access: MCPAccessListed, Label: "PhotoCraft (ArtCraft)", Description: "Image editing: photos, mockups, raster assets",
			Enabled: ArtCraftInstalled("photocraft"), Transport: "stdio", Command: ArtCraftCLI("photocraft"),
			Args: withRoots(),
		},
		{
			ID: "anicraft", Access: MCPAccessListed, Label: "AniCraft (ArtCraft)", Description: "2D animation: scenes, tweens, frame export",
			Enabled: ArtCraftInstalled("anicraft"), Transport: "stdio", Command: ArtCraftCLI("anicraft"),
			Args: artCraftArgs(),
		},
		{
			ID: "effectcraft", Access: MCPAccessListed, Label: "EffectCraft (ArtCraft)", Description: "Motion graphics compositing: comps, layers, keyframes, effects",
			Enabled: ArtCraftInstalled("effectcraft"), Transport: "stdio", Command: ArtCraftCLI("effectcraft"),
			Args: withRoots(),
		},
		{
			ID: "filmcraft", Access: MCPAccessListed, Label: "FilmCraft (ArtCraft)", Description: "Video editing: sequences, clips, renders",
			Enabled: ArtCraftInstalled("filmcraft"), Transport: "stdio", Command: ArtCraftCLI("filmcraft"),
			Args: artCraftArgs(),
		},
		{
			ID: "lightcraft", Access: MCPAccessListed, Label: "LightCraft (ArtCraft)", Description: "Photo library and raw development",
			Enabled: ArtCraftInstalled("lightcraft"), Transport: "stdio", Command: ArtCraftCLI("lightcraft"),
			Args: artCraftArgs("--library", filepath.Join(homeOrTemp(), "Pictures", "LightCraftLibrary")),
			ConfigFields: []MCPConfigField{
				{Key: "2", Location: "args", Required: true, Description: "LightCraft library folder"},
			},
		},
		{
			ID: "solidcraft", Access: MCPAccessListed, Label: "SolidCraft (ArtCraft)", Description: "Parametric 3D CAD: parts, sketches, exports",
			Enabled: ArtCraftInstalled("solidcraft"), Transport: "stdio", Command: ArtCraftCLI("solidcraft"),
			Args: artCraftArgs("--headless", "--allow-path", homeOrTemp()),
		},
		{
			ID: "soundcraft", Access: MCPAccessListed, Label: "SoundCraft (ArtCraft)", Description: "Audio workstation: tracks, clips, mixing, bounces",
			Enabled: ArtCraftInstalled("soundcraft"), Transport: "stdio", Command: ArtCraftCLI("soundcraft"),
			Args: withRoots(),
		},
	}
}
