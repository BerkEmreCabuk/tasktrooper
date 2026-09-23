package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/gcloudops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerGCloudOpsRoutes exposes the Google Cloud credential vault, the
// account-level resource picker, and the per-repository resource binding. Same
// auth/middleware chain as every other route — none of these invent a new one.
//
// The credential routes sit under /v1/gcloud/credential, which
// middleware_role.go gates as admin-only for mutations (see the "/v1/gcloud/"
// rule there). The binding routes are under /v1/repositories, already gated by
// that prefix's rule.
func (h *Handler) registerGCloudOpsRoutes(app fiber.Router) {
	if h.gcloudOpsSvc == nil {
		return
	}
	app.Put("/v1/gcloud/credential", h.SaveGCloudCredential)
	app.Get("/v1/gcloud/credential", h.GetGCloudCredential)
	app.Delete("/v1/gcloud/credential", h.DeleteGCloudCredential)
	app.Get("/v1/gcloud/resources", h.ListGCloudResources)
	app.Put("/v1/repositories/:id/gcloud/resource", h.BindGCloudResource)
	app.Get("/v1/repositories/:id/gcloud/resource", h.GetGCloudResource)
	app.Delete("/v1/repositories/:id/gcloud/resource", h.UnbindGCloudResource)
}

// saveGCloudCredentialRequest is the SaveGCloudCredential body. project_id is
// optional: the key file names one, and this only overrides it for a service
// account issued in one project and granted roles on another.
type saveGCloudCredentialRequest struct {
	ProjectID string            `json:"project_id"`
	Data      map[string]string `json:"data"`
}

// SaveGCloudCredential — PUT /v1/gcloud/credential
// Body: {"data": {"service_account_json": "<the whole key file>"}, "project_id": "..."}.
// The credential is validated against Google's token endpoint before anything
// is persisted — an invalid credential never reaches the vault.
func (h *Handler) SaveGCloudCredential(c *fiber.Ctx) error {
	var req saveGCloudCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.gcloudOpsSvc.SaveCredential(h.enrichContext(c), req.ProjectID, req.Data); err != nil {
		return gcloudCredentialError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// gcloudCredentialError splits the caller's own bad input (key material Google
// refuses) from an infra/config failure (cipher not configured, factory not
// wired, persist error) — the same errors.Is dispatch storeOpsCredentialError
// uses, so a backend failure surfaces as 500 instead of telling the caller
// their key file was wrong.
func gcloudCredentialError(c *fiber.Ctx, err error) error {
	if errors.Is(err, gcloudops.ErrInvalidCredential) {
		return badRequest(c, err.Error())
	}
	return internalError(c, err)
}

// GetGCloudCredential — GET /v1/gcloud/credential
// Returns the domain.GCloudCredentialView projection only — whether Google
// Cloud is connected, which project and service account, and when it was last
// written. The key file never leaves the service layer, and there is no route
// that returns it.
func (h *Handler) GetGCloudCredential(c *fiber.Ctx) error {
	view, err := h.gcloudOpsSvc.Credential(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(view)
}

// DeleteGCloudCredential — DELETE /v1/gcloud/credential
func (h *Handler) DeleteGCloudCredential(c *fiber.Ctx) error {
	if err := h.gcloudOpsSvc.DeleteCredential(h.enrichContext(c)); err != nil {
		return internalError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// gcloudResourceListing is the ListGCloudResources response.
//
// listing_available and reason follow the store picker's contract exactly
// (storeAppListing): "not connected" and "connected but cannot enumerate" are
// separate answers reported with 200, never a 5xx, because neither is a
// failure of this server and each sends the operator somewhere different —
// not_connected to the integrations screen, listing_unsupported to their own
// IAM console.
//
// The per-family blocks exist because the two APIs are authorized separately:
// a service account granted roles/run.viewer and nothing else produces a
// perfectly usable Cloud Run picker, and reporting the whole listing as
// unavailable over GKE would hide it.
type gcloudResourceListing struct {
	Available bool   `json:"listing_available"`
	Reason    string `json:"reason,omitempty"`
	// Resources is the flat, type-discriminated array (see
	// domain.GCloudResourceRef.Type). Always present, never null.
	Resources []domain.GCloudResourceRef `json:"resources"`
	CloudRun  gcloudFamilyListing        `json:"cloud_run"`
	GKE       gcloudGKEListing           `json:"gke"`
}

// gcloudFamilyListing is one resource family's verdict, in the same vocabulary
// as the top-level one.
type gcloudFamilyListing struct {
	Available            bool     `json:"listing_available"`
	Reason               string   `json:"reason,omitempty"`
	UnreachableLocations []string `json:"unreachable_locations,omitempty"`
}

// gcloudGKEListing adds the workload verdict to the cluster one. The two are
// separate because they can differ: clusters list fine while the Deployments
// inside them are not reachable at all from this server (see
// domain.GKEClusterDetail). workloads_available is therefore always false, and
// saying so is the point — an absent field would let a console render an empty
// workload list as "these clusters run nothing".
type gcloudGKEListing struct {
	gcloudFamilyListing
	WorkloadsAvailable bool   `json:"workloads_available"`
	WorkloadsReason    string `json:"workloads_reason,omitempty"`
}

// ListGCloudResources — GET /v1/gcloud/resources
// The picker's source: every Cloud Run service and GKE cluster the saved
// credential can see, in one type-discriminated array.
func (h *Handler) ListGCloudResources(c *fiber.Ctx) error {
	listing, err := h.gcloudOpsSvc.Resources(h.enrichContext(c))
	if err != nil {
		if errors.Is(err, gcloudops.ErrNotConnected) {
			return c.JSON(emptyGCloudListing(listingReasonNotConnected))
		}
		// port.ErrGCloudListingUnavailable never reaches here: the service
		// absorbs it per family. A credential refused by BOTH APIs still
		// produces a 200 below with both families unavailable, which is the
		// honest answer — it is connected.
		return internalError(c, err)
	}

	out := gcloudResourceListing{
		Available: listing.CloudRun.Available || listing.GKE.Available,
		Resources: listing.Resources,
		CloudRun:  gcloudFamilyStatus(listing.CloudRun),
		GKE: gcloudGKEListing{
			gcloudFamilyListing: gcloudFamilyStatus(listing.GKE),
			WorkloadsAvailable:  false,
			WorkloadsReason:     listingReasonUnsupported,
		},
	}
	if !out.Available {
		out.Reason = listingReasonUnsupported
	}
	if out.Resources == nil {
		out.Resources = []domain.GCloudResourceRef{}
	}
	return c.JSON(out)
}

// emptyGCloudListing is the "nothing to pick from" answer, with reason saying
// why. Every array is non-nil so a consumer never has to distinguish null from
// empty.
func emptyGCloudListing(reason string) gcloudResourceListing {
	return gcloudResourceListing{
		Available: false,
		Reason:    reason,
		Resources: []domain.GCloudResourceRef{},
		CloudRun:  gcloudFamilyListing{Available: false, Reason: reason},
		GKE: gcloudGKEListing{
			gcloudFamilyListing: gcloudFamilyListing{Available: false, Reason: reason},
			WorkloadsAvailable:  false,
			WorkloadsReason:     reason,
		},
	}
}

// gcloudFamilyStatus projects a service-layer family verdict onto the wire
// shape, filling in the reason word an unavailable family needs.
func gcloudFamilyStatus(status gcloudops.FamilyStatus) gcloudFamilyListing {
	out := gcloudFamilyListing{Available: status.Available, UnreachableLocations: status.UnreachableLocations}
	if !status.Available {
		// A family reached this point only by being refused while a credential
		// IS stored — not_connected is handled above and cannot appear here.
		out.Reason = listingReasonUnsupported
	}
	return out
}

// BindGCloudResource — PUT /v1/repositories/:id/gcloud/resource
// Body: {"sub_project_path": "services/worker", "resource_type": "cloud_run",
// "resource_name": "projects/p/locations/us-central1/services/worker"}.
//
// sub_project_path "" binds the repository itself; any other value must appear
// in the repository's sub_projects list.
func (h *Handler) BindGCloudResource(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req domain.SaveGCloudResourceRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	binding, err := h.gcloudOpsSvc.BindResource(h.enrichContext(c), id, req)
	if err != nil {
		return gcloudResourceError(c, err)
	}
	return c.JSON(binding)
}

// gcloudResourceError maps the binding routes' failures onto status codes the
// console branches on: a malformed resource name, an unknown type, an unknown
// sub-project or a resource the project does not have are all the caller's own
// input (400); a missing binding is 404; a credential that was never saved or
// cannot read the resource is 424 Failed Dependency so the UI can link
// straight to the integrations section; anything else is this server's
// problem. Same shape as storeOpsActionError, and a 500 is never how a missing
// IAM role is reported.
func gcloudResourceError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidGCloudResource), errors.Is(err, gcloudops.ErrResourceNotInProject),
		errors.Is(err, gcloudops.ErrUnknownSubProject):
		return badRequest(c, err.Error())
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	case errors.Is(err, gcloudops.ErrNotConnected), errors.Is(err, port.ErrGCloudListingUnavailable):
		return c.Status(fiber.StatusFailedDependency).JSON(fiber.Map{"error": err.Error()})
	default:
		return internalError(c, err)
	}
}

// boundGCloudResource is the GetGCloudResource response: the binding always,
// the live detail only when the credential could read it.
//
// detail_available/reason repeat the listing's contract for the same reason:
// the binding is a fact about the repository that survives a disconnected or
// under-privileged credential, so hiding it behind an error would lose real
// state. 200 with detail_available:false, never a 5xx.
type boundGCloudResource struct {
	Binding         domain.GCloudResourceBinding `json:"binding"`
	DetailAvailable bool                         `json:"detail_available"`
	Reason          string                       `json:"reason,omitempty"`
	Detail          *domain.GCloudResourceDetail `json:"detail,omitempty"`
}

// GetGCloudResource — GET /v1/repositories/:id/gcloud/resource
// Query: ?sub_project_path=services/worker (absent = the repository itself).
//
// For a Cloud Run binding the detail carries the service URL, the latest ready
// and latest created revisions, the template's image, the traffic split and
// the readiness condition. For a GKE binding it carries the cluster's status,
// control-plane version, node count and node pools — and states that the
// workloads inside it are not listed.
func (h *Handler) GetGCloudResource(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	bound, err := h.gcloudOpsSvc.Resource(h.enrichContext(c), id, c.Query("sub_project_path"))
	switch {
	case err == nil:
		return c.JSON(boundGCloudResource{Binding: bound.Binding, DetailAvailable: true, Detail: bound.Detail})
	case errors.Is(err, gcloudops.ErrNotConnected):
		return c.JSON(boundGCloudResource{Binding: bound.Binding, DetailAvailable: false, Reason: listingReasonNotConnected})
	case errors.Is(err, port.ErrGCloudListingUnavailable):
		return c.JSON(boundGCloudResource{Binding: bound.Binding, DetailAvailable: false, Reason: listingReasonUnsupported})
	default:
		return gcloudResourceError(c, err)
	}
}

// UnbindGCloudResource — DELETE /v1/repositories/:id/gcloud/resource
// Query: ?sub_project_path=services/worker (absent = the repository itself).
//
// Unbinding needs no credential: the binding is this server's own record of
// which resource a scope ships to, and a scope must stay detachable after the
// service account is gone.
func (h *Handler) UnbindGCloudResource(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.gcloudOpsSvc.UnbindResource(h.enrichContext(c), id, c.Query("sub_project_path")); err != nil {
		return gcloudResourceError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
