package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GCloudCredential is the decrypted view of the Google Cloud service-account
// credential handed to the adapter — the mirror of StoreCredential, and the
// only shape in which key material travels inside this process.
//
// ProjectID rides alongside Data rather than being re-parsed out of it on
// every call: an operator may point the credential at a project other than the
// one the key file was issued in (a shared "deploy" service account granted on
// several projects is the normal case), so the stored project id is the
// authority and the key file's own is only the default.
type GCloudCredential struct {
	ProjectID string
	Data      map[string]string
}

// GCloudCredentialView is the safe-to-serve projection of the stored
// credential: never the payload, only whether Google Cloud is connected, the
// two identifiers needed to name the connection, and when it was last written.
// Same rule as storeops.CredentialView — nothing here can be replayed as an
// authentication.
type GCloudCredentialView struct {
	Connected bool `json:"connected"`
	// ClientEmail is the service account's own address. It is an identifier,
	// not a secret: it appears in every IAM binding in the customer's own
	// console, and the operator needs it to grant the roles this integration
	// asks for.
	ClientEmail string    `json:"client_email,omitempty"`
	ProjectID   string    `json:"project_id,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// The Google Cloud resource kinds this integration can bind a repository to.
// Both are things a sub-project IS deployed as, not things it deploys with —
// which is why a Cloud Build trigger or an Artifact Registry repo is not here.
const (
	GCloudResourceCloudRun   = "cloud_run"
	GCloudResourceGKECluster = "gke_cluster"
)

// GCloudResourceRef is one Google Cloud resource as the API lists it — the
// account-level listing a person picks from. Deliberately not
// GCloudResourceBinding: that row is OUR statement about a repository, while
// this is a remote record we neither own nor persist wholesale (the same split
// port.StoreAppRef draws against domain.MobileStoreApp).
//
// The json tags are load-bearing: the listing endpoint serialises this type
// straight onto the wire, and the rest of the API is snake_case.
type GCloudResourceRef struct {
	// Type discriminates the union — GCloudResourceCloudRun or
	// GCloudResourceGKECluster. The listing endpoint returns both kinds in one
	// array, so a consumer that ignores this field cannot tell a service from
	// a cluster.
	Type string `json:"type"`
	// Name is the fully qualified GCP resource name
	// (projects/<p>/locations/<loc>/services/<svc>). It is the binding key and
	// the address every detail read is issued against; the short name is
	// ambiguous across regions and is carried separately in DisplayName.
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	ProjectID   string `json:"project_id"`
	Location    string `json:"location"`
	// URI is the resource's own address — a Cloud Run service URL, or a GKE
	// control-plane endpoint. "" when the API reports none (a service that has
	// never had a ready revision, a cluster with no public endpoint).
	URI string `json:"uri,omitempty"`
	// State is Google's own word for the resource's condition, passed through
	// for display rather than mapped onto a vocabulary of ours: Cloud Run
	// reports a terminal condition state, GKE reports RUNNING/PROVISIONING/…
	// and inventing a shared enum would only lose the distinction.
	State string `json:"state,omitempty"`
}

// GCloudResourceList is one API family's listing plus the locations that
// listing could not read.
//
// UnreachableLocations exists because a Cloud Run listing is a fan-out across
// regions whenever the aggregated form is refused (see the gcloud adapter):
// one region failing must not present itself as "the project has no services
// there". An empty slice means every location answered.
type GCloudResourceList struct {
	Resources            []GCloudResourceRef `json:"resources"`
	UnreachableLocations []string            `json:"unreachable_locations,omitempty"`
}

// CloudRunTrafficTarget is one slice of a Cloud Run service's traffic split.
type CloudRunTrafficTarget struct {
	Revision string `json:"revision"`
	Percent  int    `json:"percent"`
	// Tag and URI are the named-revision addressing Cloud Run offers for
	// canaries; empty on an untagged target.
	Tag string `json:"tag,omitempty"`
	URI string `json:"uri,omitempty"`
}

// CloudRunServiceDetail is everything the console shows for one bound Cloud
// Run service.
type CloudRunServiceDetail struct {
	Ref GCloudResourceRef `json:"ref"`
	// LatestReadyRevision and LatestCreatedRevision differ exactly when a
	// deploy is in flight or has failed, which is the single most useful thing
	// this panel can say — so neither is dropped in favour of "the current
	// revision".
	LatestReadyRevision   string `json:"latest_ready_revision"`
	LatestCreatedRevision string `json:"latest_created_revision"`
	// Image is the container image the service TEMPLATE carries, i.e. what the
	// next revision would run. A revision already serving traffic may run an
	// older one; the traffic split below names the revisions, and resolving
	// each one's image would be a request per revision.
	Image   string                  `json:"image"`
	Traffic []CloudRunTrafficTarget `json:"traffic"`
	// Ready is the terminal condition's state, ReadyReason/ReadyMessage the
	// explanation Google attaches when it is not success. The message is
	// Google's own text about the customer's own service; it carries no
	// credential material.
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

// GKEClusterDetail is everything the Container API alone can say about a
// cluster.
//
// It stops at the cluster boundary on purpose. Listing the Deployments running
// INSIDE a cluster is a second authorization layer — a call to that cluster's
// own Kubernetes API server, authenticated with the same OAuth token but
// authorized by in-cluster RBAC and reachable only over the cluster's own
// endpoint and CA — and this integration does not make it. WorkloadsAvailable
// is therefore always false and WorkloadsNote says which wall was hit, so the
// console states the limit instead of rendering an empty list that reads as
// "this cluster runs nothing".
type GKEClusterDetail struct {
	Ref           GCloudResourceRef `json:"ref"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	MasterVersion string            `json:"master_version,omitempty"`
	NodeCount     int               `json:"node_count"`
	NodePools     []GKENodePool     `json:"node_pools,omitempty"`
	// Autopilot clusters have no node pools to report; the field is what tells
	// an empty NodePools apart from a failed read.
	Autopilot bool `json:"autopilot"`
	// PrivateEndpoint is true when the control plane has no public address. It
	// is the harder of the two walls in front of a workload listing: a shared
	// agent-server sitting outside the customer's VPC cannot route to it at
	// all, whatever RBAC would have allowed.
	PrivateEndpoint    bool `json:"private_endpoint"`
	WorkloadsAvailable bool `json:"workloads_available"`
	// WorkloadsNote is prose, not a code: the machine-readable reason belongs
	// to the HTTP layer's two-word dictionary (not_connected /
	// listing_unsupported) and must not be spelled a second way here.
	WorkloadsNote string `json:"workloads_note,omitempty"`
}

// GCloudIdentity is who a client acts as — the two identifiers the console
// shows and the vault stores in the clear. Neither can be replayed as an
// authentication: the service account's address appears in every IAM binding
// in the customer's own console, and the project id is the thing they typed.
type GCloudIdentity struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
}

// GCloudResourceDetail is the discriminated union the bound-resource read
// returns: exactly one of the two pointers is set, matching Ref.Type.
type GCloudResourceDetail struct {
	Ref        GCloudResourceRef      `json:"ref"`
	CloudRun   *CloudRunServiceDetail `json:"cloud_run,omitempty"`
	GKECluster *GKEClusterDetail      `json:"gke_cluster,omitempty"`
}

// GCloudResourceBinding is one (repository, sub-project) → Google Cloud
// resource statement. Source records who made it; only 'user' exists today,
// but the column is there for the detection path repository_hosting_links
// already has.
type GCloudResourceBinding struct {
	ID             uuid.UUID `json:"id"`
	RepositoryID   uuid.UUID `json:"repository_id"`
	SubProjectPath string    `json:"sub_project_path"`
	ResourceType   string    `json:"resource_type"`
	ResourceName   string    `json:"resource_name"`
	DisplayName    string    `json:"display_name,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Location       string    `json:"location,omitempty"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SaveGCloudResourceRequest binds one scope of a repository to one resource as
// the picker listed it.
type SaveGCloudResourceRequest struct {
	// SubProjectPath is "" for the repository itself, or a path that must
	// appear in the repository's sub_projects list.
	SubProjectPath string `json:"sub_project_path"`
	ResourceType   string `json:"resource_type"`
	ResourceName   string `json:"resource_name"`
	DisplayName    string `json:"display_name"`
	ProjectID      string `json:"project_id"`
	Location       string `json:"location"`
}

// ErrInvalidGCloudResource marks a binding request the caller got wrong — an
// unknown resource type, or a resource name that is not a fully qualified GCP
// name. The HTTP layer turns it into a 400; it never means the credential or
// the backend failed.
var ErrInvalidGCloudResource = errors.New("invalid google cloud resource")

// ValidGCloudResourceType reports whether t is one of the two kinds this
// integration binds.
func ValidGCloudResourceType(t string) bool {
	return t == GCloudResourceCloudRun || t == GCloudResourceGKECluster
}

// maxGCloudResourceName bounds the stored name. GCP's own limit is far lower
// (a project id is 30 chars, a service name 63, a location ~20), so this only
// stops a hostile body from filling a column.
const maxGCloudResourceName = 512

// ValidateSaveGCloudResource normalises and checks a binding request, filling
// ProjectID and Location from ResourceName when the caller left them out —
// the fully qualified name already carries both, and a request that states
// them inconsistently is a request whose detail read would go somewhere the
// listing never showed.
func ValidateSaveGCloudResource(req SaveGCloudResourceRequest) (SaveGCloudResourceRequest, error) {
	req.SubProjectPath = strings.TrimSpace(req.SubProjectPath)
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	req.ResourceName = strings.TrimSpace(req.ResourceName)
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	if !ValidGCloudResourceType(req.ResourceType) {
		return SaveGCloudResourceRequest{}, fmt.Errorf("%w: unknown resource type %q", ErrInvalidGCloudResource, req.ResourceType)
	}
	if req.ResourceName == "" {
		return SaveGCloudResourceRequest{}, fmt.Errorf("%w: resource_name is required", ErrInvalidGCloudResource)
	}
	if len(req.ResourceName) > maxGCloudResourceName {
		return SaveGCloudResourceRequest{}, fmt.Errorf("%w: resource_name is too long", ErrInvalidGCloudResource)
	}

	parsed, err := ParseGCloudResourceName(req.ResourceName)
	if err != nil {
		return SaveGCloudResourceRequest{}, err
	}
	if parsed.Type != req.ResourceType {
		return SaveGCloudResourceRequest{}, fmt.Errorf("%w: resource_name is a %s, not a %s", ErrInvalidGCloudResource, parsed.Type, req.ResourceType)
	}
	if req.ProjectID == "" {
		req.ProjectID = parsed.ProjectID
	}
	if req.Location == "" {
		req.Location = parsed.Location
	}
	if req.DisplayName == "" {
		req.DisplayName = parsed.ShortName
	}
	return req, nil
}

// ParsedGCloudResourceName is the four parts of a fully qualified GCP resource
// name this integration understands.
type ParsedGCloudResourceName struct {
	Type      string
	ProjectID string
	Location  string
	ShortName string
}

// ParseGCloudResourceName splits projects/<p>/locations/<loc>/{services,clusters}/<name>.
//
// It refuses anything else rather than accepting a short name and guessing the
// rest: the project and location a binding points at decide which API host a
// later detail read reaches, and guessing them wrong reads a DIFFERENT
// service that happens to share a name in another region.
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

// GCloudResourceName builds the fully qualified name for a collection. It is
// the inverse of ParseGCloudResourceName and the only place the two spellings
// ("services" for Cloud Run, "clusters" for GKE) are chosen.
func GCloudResourceName(projectID, location, collection, shortName string) string {
	return "projects/" + projectID + "/locations/" + location + "/" + collection + "/" + shortName
}
