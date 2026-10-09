package main

import (
	"encoding/json"
	"strings"
	"time"
)

// models.list — this Mac's live model catalog for one host-executed CLI.
//
// Detection lives here for the same reason preflight does: the binary is on
// this machine. Unlike preflight it is a genuine live query rather than a read
// of something already pushed — cursor-agent and opencode each answer
// `<bin> models` in seconds with no session and no account probing, so there is
// nothing to cache and nothing gained by asking ahead of being asked.
//
// The RAW stdout is returned rather than parsed here. Each CLI's output shape
// — cursor-agent's "id - Label" lines, opencode's bare "provider/model-id"
// lines — is already parsed once, in Go, on the cloud side (agent-server's
// adapter/agentcli/{cursor,opencode} packages) for the path where agent-server
// runs the binary itself. A second parser here, in the same language even, is
// exactly the drift this app's detection philosophy exists to avoid — so this
// hands back bytes and lets the one parser that already exists read them,
// whichever host asked.
//
// Antigravity is not a flavor here; this runner does not drive it.
const modelsListTimeout = 45 * time.Second

// modelsOutputLimit bounds one answer. A model catalog is a few hundred short
// lines at most; a limit is what stops it from being an allocation primitive
// if a CLI's output format changes underneath this Mac.
const modelsOutputLimit = 512 * 1024

type modelsListParams struct {
	Flavor string `json:"flavor"`
}

type modelsListResult struct {
	V      int    `json:"v"`
	Flavor string `json:"flavor"`
	Output string `json:"output"`
}

// binAndLabelForFlavor resolves which binary answers a flavor's models.list,
// and the name to put in front of a person when it cannot. cursor and opencode
// are the only flavors this Mac is ever asked about: claude_code's list is the
// curated constant application/agentcli.RemoteModels answers with before this
// method is ever reached, and antigravity is not a flavor this runner drives.
func binAndLabelForFlavor(cfg config, flavor string) (bin, label string, ferr *rpcError) {
	switch flavor {
	case "cursor":
		return cfg.cursorAgentBin, "the Cursor CLI", nil
	case "opencode":
		return cfg.opencodeBin, "the OpenCode CLI", nil
	default:
		return "", "", failure(codeBadRequest, "flavor %q is not one this machine can list models for", flavor)
	}
}

func listModels(c *call) (any, *rpcError) {
	var p modelsListParams
	if err := json.Unmarshal(c.params, &p); err != nil {
		return nil, failure(codeBadRequest, "models.list params are not the expected object: %v", err)
	}
	flavor := strings.TrimSpace(p.Flavor)
	if flavor == "" {
		return nil, failure(codeBadRequest, "flavor is required")
	}
	bin, label, ferr := binAndLabelForFlavor(c.cfg, flavor)
	if ferr != nil {
		return nil, ferr
	}
	if bin != "" {
		if _, err := launcherFor(bin); err != nil {
			return nil, failure(codeNotReady, "%v", err)
		}
	}

	// mobileRun is not mobile-specific despite its name and its file: it is
	// the shared "run one short helper, turn its failure into an answer"
	// pattern (see its own comment in mobile.go), and reusing it here means
	// this method gets the same not_ready/upstream/cancelled mapping as every
	// other short CLI invocation on this Mac rather than a second copy of it.
	output, rerr := mobileRun(c, bin, label, modelsListTimeout, "models")
	if rerr != nil {
		return nil, rerr
	}
	if len(output) > modelsOutputLimit {
		output = output[:modelsOutputLimit]
	}
	return modelsListResult{V: protocolVersion, Flavor: flavor, Output: output}, nil
}
