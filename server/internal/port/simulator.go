package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SimulatorLaunch is one debug build of a project directory installed and
// opened on one local device.
type SimulatorLaunch struct {
	Platform   string
	DeviceID   string
	ProjectDir string
	Scheme     string // iOS
	Module     string // Android Gradle module
	Identifier string // bundle id / package name when known; the host reads the built app's own when it can
	// CacheDir survives between runs of one repository so a second run is an
	// incremental build (Xcode's derived data).
	CacheDir string
	Status   func(status string)
	Log      func(line string)
}

// SimulatorHost is this machine's simulators and the toolchain that builds for
// them.
type SimulatorHost interface {
	Devices(ctx context.Context) ([]domain.SimulatorDevice, error)
	// BuildAndLaunch blocks until the app is running on the device; its error
	// text is what a person is shown.
	BuildAndLaunch(ctx context.Context, req SimulatorLaunch) error
}
