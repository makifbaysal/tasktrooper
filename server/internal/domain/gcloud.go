package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// GCloudCredential is the decrypted view of the Google Cloud service-account
// credential handed to the adapter — the mirror of StoreCredential, and the
// only shape in which key material travels inside this process. ProjectID rides
// alongside Data because an operator may point the credential at a project
// other than the one the key file was issued in (a shared "deploy" service
// account is the normal case).
type GCloudCredential struct {
	ProjectID string
	Data      map[string]string
}

// The Google Cloud resource kinds this integration binds: both are things a
// sub-project IS deployed AS, which is why a Cloud Build trigger is not here.
const (
	GCloudResourceCloudRun   = "cloud_run"
	GCloudResourceGKECluster = "gke_cluster"
)

// GCloudResourceRef is one Google Cloud resource as the API lists it — a
// remote record we neither own nor persist wholesale.
type GCloudResourceRef struct {
	// Type discriminates the union; the consumer that ignores it cannot tell a
	// service from a cluster.
	Type string `json:"type"`
	// Name is the fully qualified GCP resource name, the binding key and the
	// address every detail read is issued against; the short name is ambiguous
	// across regions and is carried separately in DisplayName.
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	// URI is the resource's own address; "" when the API reports none.
	URI string `json:"uri,omitempty"`
	// State is Google's own word for the condition, passed through rather than
	// mapped onto our vocabulary (Cloud Run terminal condition vs GKE
	// RUNNING/PROVISIONING/…).
	State string `json:"state,omitempty"`
}

// GCloudResourceList is one API family's listing plus the locations that
// listing could not read — a Cloud Run listing fans out across regions when
// the aggregated form is refused, so one failing region must not present as
// "no services there". Empty slice = every location answered.
type GCloudResourceList struct {
	Resources            []GCloudResourceRef `json:"resources"`
	UnreachableLocations []string            `json:"unreachable_locations,omitempty"`
}

// CloudRunTrafficTarget is one slice of a Cloud Run service's traffic split.
type CloudRunTrafficTarget struct {
	Revision string `json:"revision"`
	Percent  int    `json:"percent"`
	// Tag and URI are the named-revision addressing for canaries.
	Tag string `json:"tag,omitempty"`
	URI string `json:"uri,omitempty"`
}

// CloudRunServiceDetail is everything the console shows for one bound Cloud
// Run service.
type CloudRunServiceDetail struct {
	Ref GCloudResourceRef `json:"ref"`
	// The two revisions differ exactly when a deploy is in flight or has
	// failed, which is the most useful thing this panel can say.
	LatestReadyRevision   string `json:"latest_ready_revision"`
	LatestCreatedRevision string `json:"latest_created_revision"`
	// Image is the container image the TEMPLATE carries — what the next
	// revision would run; the traffic split names what is serving traffic.
	Image   string                  `json:"image"`
	Traffic []CloudRunTrafficTarget `json:"traffic"`
	// Ready/ReadyReason/ReadyMessage are Google's terminal-condition words.
	Ready        string    `json:"ready"`
	ReadyReason  string    `json:"ready_reason,omitempty"`
	ReadyMessage string    `json:"ready_message,omitempty"`
	UpdateTime   time.Time `json:"update_time,omitempty"`
}

// GKENodePool is one node pool of a cluster, as the Container API reports it.
type GKENodePool struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	NodeCount   int    `json:"node_count"`
	Version     string `json:"version,omitempty"`
	MachineType string `json:"machine_type,omitempty"`
}

// GKEClusterDetail stops at the cluster boundary on purpose: listing the
// Deployments INSIDE would need a second authorization layer (the cluster's own
// kube API, RBAC, endpoint and CA), which this integration does not make.
// WorkloadsAvailable is therefore always false and WorkloadsNote says which
// wall was hit, so the console states the limit instead of rendering an empty
// list that reads as "this cluster runs nothing".
type GKEClusterDetail struct {
	Ref           GCloudResourceRef `json:"ref"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	MasterVersion string            `json:"master_version,omitempty"`
	NodeCount     int               `json:"node_count"`
	NodePools     []GKENodePool     `json:"node_pools,omitempty"`
	// Autopilot clusters have no node pools; the field tells an empty list
	// apart from a failed read.
	Autopilot bool `json:"autopilot"`
	// PrivateEndpoint is true when the control plane has no public address —
	// the harder of the two walls in front of a workload listing.
	PrivateEndpoint    bool `json:"private_endpoint"`
	WorkloadsAvailable bool `json:"workloads_available"`
	// WorkloadsNote is prose, not a code: the machine-readable reason belongs
	// to the HTTP layer's two-word dictionary (not_connected /
	// listing_unsupported).
	WorkloadsNote string `json:"workloads_note,omitempty"`
}

// GCloudIdentity is who a client acts as — the two identifiers the console
// shows and the vault stores in the clear; neither can be replayed as an
// authentication.
type GCloudIdentity struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
}

// ErrInvalidGCloudResource marks a resource name the caller got wrong; the
// gcloud adapter turns it into port.ErrCloudAuth/a 4xx, never a backend
// failure.
var ErrInvalidGCloudResource = errors.New("invalid google cloud resource")

// ParsedGCloudResourceName is the four parts of a fully qualified GCP resource
// name this integration understands.
type ParsedGCloudResourceName struct {
	Type      string
	ProjectID string
	Location  string
	ShortName string
}

// ParseGCloudResourceName splits projects/<p>/locations/<loc>/{services,clusters}/<name>.
// It refuses anything else rather than accepting a short name and guessing the
// rest: the project and location decide which API host a detail read reaches,
// and guessing wrong would read a DIFFERENT service sharing a name in another
// region.
func ParseGCloudResourceName(name string) (ParsedGCloudResourceName, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "projects" || parts[2] != "locations" {
		return ParsedGCloudResourceName{}, fmt.Errorf("%w: %q is not projects/<project>/locations/<location>/<collection>/<name>", ErrInvalidGCloudResource, name)
	}
	var kind string
	switch parts[4] {
	case "services":
		kind = GCloudResourceCloudRun
	case "clusters":
		kind = GCloudResourceGKECluster
	default:
		return ParsedGCloudResourceName{}, fmt.Errorf("%w: unknown collection %q", ErrInvalidGCloudResource, parts[4])
	}
	for _, p := range []string{parts[1], parts[3], parts[5]} {
		if p == "" {
			return ParsedGCloudResourceName{}, fmt.Errorf("%w: %q has an empty segment", ErrInvalidGCloudResource, name)
		}
	}
	return ParsedGCloudResourceName{Type: kind, ProjectID: parts[1], Location: parts[3], ShortName: parts[5]}, nil
}

// GCloudResourceName builds the fully qualified name, the inverse of
// ParseGCloudResourceName and the only place the two spellings ("services" for
// Cloud Run, "clusters" for GKE) are chosen.
func GCloudResourceName(projectID, location, collection, shortName string) string {
	return "projects/" + projectID + "/locations/" + location + "/" + collection + "/" + shortName
}
