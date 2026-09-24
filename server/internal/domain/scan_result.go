package domain

// ScanResult is the pure output of application/discovery: everything read off
// one working copy, before it is reconciled with what the human set. It is
// stored verbatim on the scan row so a reconcile can be replayed and audited.
type ScanResult struct {
	Shape         RepoShape           `json:"shape"`
	ShapeEvidence []SourceEvidence    `json:"shape_evidence,omitempty"`
	FileCount     int                 `json:"file_count"`
	Truncated     bool                `json:"truncated,omitempty"`
	Languages     []LanguageShare     `json:"languages,omitempty"`
	Components    []DetectedComponent `json:"components"`
	Checks        []DetectedCheck     `json:"checks,omitempty"`
	Links         []DetectedLink      `json:"links,omitempty"`
	DeploySignals []DeploySignal      `json:"deploy_signals,omitempty"`
	Git           ScanGit             `json:"git"`
	Warnings      []string            `json:"warnings,omitempty"`
}

type LanguageShare struct {
	Language string `json:"language"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

type ScanGit struct {
	DefaultBranch string `json:"default_branch,omitempty"`
	HeadSHA       string `json:"head_sha,omitempty"`
	RemoteSlug    string `json:"remote_slug,omitempty"`
}

type DetectedCommand struct {
	Purpose CommandPurpose `json:"purpose"`
	Command string         `json:"command"`
	Source  SourceEvidence `json:"source"`
}

type DetectedComponent struct {
	Path           string            `json:"path"`
	Name           string            `json:"name"`
	Role           ComponentRole     `json:"role"`
	RoleConfidence Confidence        `json:"role_confidence"`
	RoleEvidence   []SourceEvidence  `json:"role_evidence,omitempty"`
	Stack          ComponentStack    `json:"stack"`
	Commands       []DetectedCommand `json:"commands,omitempty"`
	Mobile         *MobileFacts      `json:"mobile,omitempty"`
	// PackageName is what other components import this one by (npm name, Go
	// module path); the matcher resolves workspace and cross-repo package links
	// through it.
	PackageName string `json:"package_name,omitempty"`
	// DevPort is the port the component's dev server or listener binds, when
	// the tree states one; a localhost URL on that port elsewhere is a link.
	DevPort int `json:"dev_port,omitempty"`
}

type DetectedCheck struct {
	ComponentPath string            `json:"component_path"`
	Workflow      string            `json:"workflow"`
	WorkflowName  string            `json:"workflow_name,omitempty"`
	JobKey        string            `json:"job_key"`
	JobName       string            `json:"job_name,omitempty"`
	Purpose       CheckPurpose      `json:"purpose"`
	Environment   DeployEnvironment `json:"environment,omitempty"`
	Triggers      []string          `json:"triggers,omitempty"`
	PathFilters   []string          `json:"path_filters,omitempty"`
	Steps         []CheckStep       `json:"steps,omitempty"`
	LocalCommands []LocalCommand    `json:"local_commands,omitempty"`
	Dispatchable  bool              `json:"dispatchable"`
	Confidence    Confidence        `json:"confidence"`
}

type LinkTargetKind string

const (
	LinkTargetResource   LinkTargetKind = "resource"
	LinkTargetComponent  LinkTargetKind = "component"
	LinkTargetUnresolved LinkTargetKind = "unresolved"
)

// LinkTarget is what a signal points at before matching. A component target
// is resolved inside the same repository by path; an unresolved one carries
// hints (host, port, service name) for the cross-repository matcher.
type LinkTarget struct {
	Kind          LinkTargetKind `json:"kind"`
	ResourceKind  ResourceKind   `json:"resource_kind,omitempty"`
	Vendor        string         `json:"vendor,omitempty"`
	Name          string         `json:"name,omitempty"`
	ComponentPath string         `json:"component_path,omitempty"`
	PackageName   string         `json:"package_name,omitempty"`
	URLHost       string         `json:"url_host,omitempty"`
	Port          int            `json:"port,omitempty"`
	ServiceHint   string         `json:"service_hint,omitempty"`
}

type DetectedLink struct {
	ComponentPath string           `json:"component_path"`
	SignalKey     string           `json:"signal_key"`
	Protocol      LinkProtocol     `json:"protocol"`
	Detail        string           `json:"detail,omitempty"`
	Target        LinkTarget       `json:"target"`
	EnvVars       []string         `json:"env_vars,omitempty"`
	Evidence      []SourceEvidence `json:"evidence,omitempty"`
	Confidence    Confidence       `json:"confidence"`
}

// DeploySignal is a marker that a component ships to a provider. Ref holds
// the provider-specific identifiers the tree states (service, region,
// project id…) for the cloud matcher.
type DeploySignal struct {
	ComponentPath string            `json:"component_path"`
	Provider      string            `json:"provider"`
	Environment   DeployEnvironment `json:"environment,omitempty"`
	Ref           map[string]string `json:"ref,omitempty"`
	Evidence      []SourceEvidence  `json:"evidence,omitempty"`
	Confidence    Confidence        `json:"confidence"`
}

func (r ScanResult) ComponentPaths() []string {
	out := make([]string, 0, len(r.Components))
	for _, c := range r.Components {
		out = append(out, c.Path)
	}
	return out
}
