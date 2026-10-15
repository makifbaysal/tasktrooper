package main

import "net/http"

// The code index of this computer's checkouts, for the cloud: start a pass
// over a checkout here, read how an index stands, search it, stop a pass.
// Each is one document each way. The pass runs in the local executor under
// its own life, so nothing here holds a session slot or a stream open for its
// length; the code, its chunks and their vectors never leave this computer,
// only counts, states and search hits do.

func (s *runnerServer) handleIndexStart(w http.ResponseWriter, r *http.Request) {
	s.forwardCheckoutDocument(w, r, "/exec/index.start")
}

func (s *runnerServer) handleIndexStatus(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/index.status")
}

func (s *runnerServer) handleIndexSearch(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/index.search")
}

func (s *runnerServer) handleIndexCancel(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/index.cancel")
}
