package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// EnvVarKind says how a deploy target's environment variable gets its value.
// Only the first two are filled without a human; a value never reaches an
// agent in any of them.
type EnvVarKind string

const (
	// EnvKindValue is a non-secret literal known up front (owner/repo, a
	// branch, a public URL); EnvRequirement.Value carries it.
	EnvKindValue EnvVarKind = "value"
	// EnvKindGenerated is a random secret nobody has to know (a session or
	// signing key).
	EnvKindGenerated EnvVarKind = "generated"
	// EnvKindHumanSecret is a credential only a human can obtain (an API key,
	// a personal access token).
	EnvKindHumanSecret EnvVarKind = "human_secret"
	// EnvKindHumanBcrypt is the bcrypt hash of a password a human chooses; the
	// password is hashed on the server and never stored.
	EnvKindHumanBcrypt EnvVarKind = "human_bcrypt"
	// EnvKindOptional may stay unset in the deploy target.
	EnvKindOptional EnvVarKind = "optional"
)

func (k EnvVarKind) Valid() bool {
	switch k {
	case EnvKindValue, EnvKindGenerated, EnvKindHumanSecret, EnvKindHumanBcrypt, EnvKindOptional:
		return true
	}
	return false
}

func (k EnvVarKind) Automatic() bool { return k == EnvKindValue || k == EnvKindGenerated }

func (k EnvVarKind) NeedsHuman() bool { return k == EnvKindHumanSecret || k == EnvKindHumanBcrypt }

const (
	EnvSourceAgent = "agent"
	EnvSourceHuman = "human"
)

var envVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func ValidEnvVarName(name string) bool { return envVarNamePattern.MatchString(name) }

var (
	ErrEnvRequirementInvalid = errors.New("invalid environment variable requirement")
	// ErrDeployEnvMissing: the deploy target lacks variables only a human can
	// provide (or classify), so shipping now would run the code without them.
	ErrDeployEnvMissing = errors.New("the deploy target is missing environment variables")
	// ErrDeployEnvUnchecked: the deploy target's variables could not be read,
	// so whether they are all set is unknown.
	ErrDeployEnvUnchecked = errors.New("the deploy target's environment variables could not be checked")
)

// EnvRequirement is one variable a component's deploy target must have.
type EnvRequirement struct {
	ID           uuid.UUID  `json:"id"`
	RepositoryID uuid.UUID  `json:"repository_id"`
	ComponentID  *uuid.UUID `json:"component_id,omitempty"`
	Name         string     `json:"name"`
	Kind         EnvVarKind `json:"kind"`
	Value        string     `json:"value,omitempty"`
	Description  string     `json:"description,omitempty"`
	Source       string     `json:"source"`
	TaskID       *uuid.UUID `json:"task_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Validate checks a requirement as it is declared: a value belongs to the
// value kind alone, so a secret can never be written into this table.
func (r EnvRequirement) Validate() error {
	switch {
	case !ValidEnvVarName(r.Name):
		return fmt.Errorf("%w: %q is not an environment variable name", ErrEnvRequirementInvalid, r.Name)
	case !r.Kind.Valid():
		return fmt.Errorf("%w: %s has unknown kind %q", ErrEnvRequirementInvalid, r.Name, r.Kind)
	case r.Kind == EnvKindValue && r.Value == "":
		return fmt.Errorf("%w: %s is a value kind without a value", ErrEnvRequirementInvalid, r.Name)
	case r.Kind != EnvKindValue && r.Value != "":
		return fmt.Errorf("%w: %s is %s, and only the value kind may carry a value", ErrEnvRequirementInvalid, r.Name, r.Kind)
	}
	return nil
}

// EnvExample is one variable an env example file (.env.example and friends)
// declares — the safety net for a variable nobody declared.
type EnvExample struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	Path  string `json:"path"`
}

// CloudEnvVar is a variable the provider already has: its name and the
// environments it is set for, never its value.
type CloudEnvVar struct {
	Key     string              `json:"key"`
	Targets []DeployEnvironment `json:"targets"`
}

func (v CloudEnvVar) SetFor(env DeployEnvironment) bool {
	for _, t := range v.Targets {
		if t == env {
			return true
		}
	}
	return false
}

// CloudEnvWrite is one variable to create or overwrite at the provider.
type CloudEnvWrite struct {
	Key       string
	Value     string
	Sensitive bool
	Targets   []DeployEnvironment
}

// EnvTargets are the provider environments TaskTrooper writes a variable to
// on a provider with per-branch previews: production for the release, preview
// so a task's own preview runs it too.
var EnvTargets = []DeployEnvironment{EnvironmentProduction, EnvironmentPreview}

// EnvCapabilities is how a provider's resource keeps its variables.
type EnvCapabilities struct {
	// Targets are the environments a variable can be set for; a resource that
	// is one environment answers production alone.
	Targets []DeployEnvironment `json:"targets"`
	// WritesRollOut: writing a variable starts a new deployment by itself
	// (a Cloud Run revision, an ECS service update), so there is nothing to
	// redeploy afterwards.
	WritesRollOut bool `json:"writes_roll_out"`
	// OverwrittenOnDeploy: the resource's configuration is usually rendered
	// from a file in the repository on every deploy (an ECS task
	// definition), so a variable written only at the provider is lost on the
	// next deploy unless that file carries it too.
	OverwrittenOnDeploy bool `json:"overwritten_on_deploy"`
}

func (c EnvCapabilities) Supports(env DeployEnvironment) bool {
	for _, t := range c.Targets {
		if t == env {
			return true
		}
	}
	return false
}

// EnvVarAction is what a variable still needs.
type EnvVarAction string

const (
	EnvActionNone EnvVarAction = "none"
	// EnvActionAuto: TaskTrooper fills it on the next check.
	EnvActionAuto EnvVarAction = "auto"
	// EnvActionHuman: a human has to enter it.
	EnvActionHuman EnvVarAction = "human"
	// EnvActionClassify: nobody declared it; a human has to say what it is.
	EnvActionClassify EnvVarAction = "classify"
)

// EnvVarView is one variable of a component's deploy target, as the UI shows
// it. Kind is empty for a variable only an env example file knows about.
type EnvVarView struct {
	Name         string       `json:"name"`
	Kind         EnvVarKind   `json:"kind,omitempty"`
	Value        string       `json:"value,omitempty"`
	Description  string       `json:"description,omitempty"`
	Source       string       `json:"source,omitempty"`
	ExampleValue string       `json:"example_value,omitempty"`
	ExamplePath  string       `json:"example_path,omitempty"`
	Production   bool         `json:"production"`
	Preview      bool         `json:"preview"`
	Action       EnvVarAction `json:"action"`
}

// ComponentEnvStatus is one component's deploy-target variables against what
// it requires. Error is set when the provider could not be read; Vars then
// still lists the requirements, with Production/Preview unknown (false).
type ComponentEnvStatus struct {
	ComponentID   uuid.UUID         `json:"component_id"`
	ComponentName string            `json:"component_name"`
	EnvironmentID uuid.UUID         `json:"environment_id"`
	Provider      CloudProviderKind `json:"provider"`
	ResourceName  string            `json:"resource_name,omitempty"`
	Capabilities  EnvCapabilities   `json:"capabilities"`
	Vars          []EnvVarView      `json:"vars"`
	Error         string            `json:"error,omitempty"`
}

// Missing are the names still waiting on a human (to enter or to classify).
func (s ComponentEnvStatus) Missing() []string {
	var out []string
	for _, v := range s.Vars {
		if v.Action == EnvActionHuman || v.Action == EnvActionClassify {
			out = append(out, v.Name)
		}
	}
	return out
}

// EnvEnsureResult is what a pre-ship check did and what it could not do.
// OverwrittenOnDeploy carries the target's capability of that name, so a
// caller that just created variables can warn they must reach the
// repository's deploy configuration too.
type EnvEnsureResult struct {
	Created             []string
	Missing             []string
	OverwrittenOnDeploy bool
}
