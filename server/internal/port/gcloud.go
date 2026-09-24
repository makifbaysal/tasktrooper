package port

import (
	"errors"
)

// ErrGCloudListingUnavailable marks a Cloud Run/GKE listing call the stored
// credential cannot make (an IAM role it lacks, an API not enabled) — the
// adapter/cloud/gcloud client wraps this rather than a raw HTTP error so a
// caller can tell "cannot list" apart from "the credential itself is bad"
// (port.ErrCloudAuth).
var ErrGCloudListingUnavailable = errors.New("google cloud resource listing is unavailable for this credential")
