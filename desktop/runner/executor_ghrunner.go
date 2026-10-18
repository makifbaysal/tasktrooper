package main

import "net/http"

// This computer's GitHub Actions runners, for a cloud that holds the GitHub
// connection: install a repository's runner, start it with a one-job config
// the cloud minted, read how the runners are doing, stop one. The runners
// live in the local executor under their own life, so every call here is one
// document each way and none holds a session slot open. The config is the
// one secret in the exchange; it travels only down the tunnel into the
// runner's environment and is never answered back.

func (s *runnerServer) handleGitHubRunnerPrepare(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/ghrunner.prepare")
}

func (s *runnerServer) handleGitHubRunnerStart(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/ghrunner.start")
}

func (s *runnerServer) handleGitHubRunnerStatus(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/ghrunner.status")
}

func (s *runnerServer) handleGitHubRunnerStop(w http.ResponseWriter, r *http.Request) {
	s.forwardExecutorDocument(w, r, "/exec/ghrunner.stop")
}
