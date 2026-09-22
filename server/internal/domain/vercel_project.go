package domain

import (
	"time"

	"github.com/google/uuid"
)

type VercelProjectLink struct {
	ID             uuid.UUID `json:"id"`
	RepositoryID   uuid.UUID `json:"repository_id"`
	SubProjectPath string    `json:"sub_project_path"`
	ProjectID      string    `json:"project_id"`
	ProjectName    string    `json:"project_name,omitempty"`
	TeamID         string    `json:"team_id"`
	TeamSlug       string    `json:"team_slug,omitempty"`
	Framework      string    `json:"framework,omitempty"`
	RootDirectory  string    `json:"root_directory,omitempty"`
	ProductionURL  string    `json:"production_url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

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

type VercelProjectDetails struct {
	Link                 VercelProjectLink `json:"link"`
	ProductionURL        string            `json:"production_url,omitempty"`
	Framework            string            `json:"framework,omitempty"`
	RootDirectory        string            `json:"root_directory,omitempty"`
	LatestDeployment     *VercelDeployment `json:"latest_deployment,omitempty"`
	LastFailedDeployment *VercelDeployment `json:"last_failed_deployment,omitempty"`
	Warnings             []string          `json:"warnings,omitempty"`
}
