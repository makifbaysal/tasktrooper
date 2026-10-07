package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// SimulatorDevice is one iOS simulator or Android emulator/device on this
// machine a task can be run on.
type SimulatorDevice struct {
	ID       string `json:"id"` // simulator UDID, AVD name, or adb serial
	Name     string `json:"name"`
	Platform string `json:"platform"` // MobileStorePlatformIOS | MobileStorePlatformAndroid
	Runtime  string `json:"runtime,omitempty"`
	State    string `json:"state"`
}

const (
	SimulatorRunPreparing  = "preparing"
	SimulatorRunBuilding   = "building"
	SimulatorRunInstalling = "installing"
	SimulatorRunLaunching  = "launching"
	SimulatorRunRunning    = "running"
	SimulatorRunFailed     = "failed"
)

func SimulatorRunTerminal(status string) bool {
	return status == SimulatorRunRunning || status == SimulatorRunFailed
}

// SimulatorRun is one "build this task and open it on that simulator". It is
// held in memory only: it describes this machine's simulator right now, which
// a restart does not preserve either.
type SimulatorRun struct {
	ID           uuid.UUID  `json:"id"`
	RepositoryID uuid.UUID  `json:"repository_id"`
	TaskID       uuid.UUID  `json:"task_id"`
	Platform     string     `json:"platform"`
	DeviceID     string     `json:"device_id"`
	DeviceName   string     `json:"device_name,omitempty"`
	CommitSHA    string     `json:"commit_sha,omitempty"`
	Status       string     `json:"status"`
	Failure      string     `json:"failure,omitempty"`
	LogTail      string     `json:"log_tail,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

var (
	ErrNoSimulators         = errors.New("this machine has no iOS simulator or Android emulator to run on")
	ErrSimulatorRunNotFound = errors.New("this task has not been run on a simulator")
	ErrSimulatorRunBusy     = errors.New("a simulator run is already in progress on this machine")
	ErrSimulatorNotMobile   = errors.New("this repository has no iOS or Android app to run")
)
