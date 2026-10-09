package main

import "encoding/json"

// preflight.report — what this Mac can and cannot do, as the desktop app sees
// it.
//
// This program does no probing. Detection lives in the desktop app's
// `services/detect.ts`, which is the one place that knows how this Mac's tools
// are found — the extra PATH prefixes a GUI-launched .app does not inherit, the
// several places the Claude CLI installs itself, how to read `claude auth
// status`. A second implementation here, in another language, would drift into
// describing a different machine than the one the user is looking at on their
// own screen, and the two would disagree in front of them.
//
// So the supervisor pushes its report down this process's stdin whenever it
// runs the checks, and this hands back the latest one, verbatim. The report
// carries its own `generatedAt`, so a caller can see how old it is rather than
// having to trust that it is current.
func (s *runnerServer) preflightReport() (json.RawMessage, *rpcError) {
	report := s.state.getPreflight()
	if len(report) == 0 {
		// Honest rather than empty. "No report yet" and "a report that found
		// nothing" are different facts, and a caller that could not tell them
		// apart would show a healthy Mac as having nothing installed.
		return nil, failure(codeNotReady, "this machine has not reported its environment yet; the desktop app pushes it as soon as the checks finish")
	}
	return report, nil
}
