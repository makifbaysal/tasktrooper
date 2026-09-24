package domain

import (
	"time"
)

const (
	VercelDeploymentReady = "READY"
	VercelDeploymentError = "ERROR"
)

const VercelTargetProduction = "production"

type VercelDeployment struct {
	ID            string    `json:"id"`
	State         string    `json:"state,omitempty"`
	Target        string    `json:"target,omitempty"`
	URL           string    `json:"url,omitempty"`
	InspectorURL  string    `json:"inspector_url,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	ReadyAt       time.Time `json:"ready_at,omitempty"`
	CommitSHA     string    `json:"commit_sha,omitempty"`
	CommitRef     string    `json:"commit_ref,omitempty"`
	CommitMessage string    `json:"commit_message,omitempty"`
	CommitAuthor  string    `json:"commit_author,omitempty"`
	ErrorCode     string    `json:"error_code,omitempty"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}

func (d VercelDeployment) Failed() bool { return d.State == VercelDeploymentError }
