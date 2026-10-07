package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// registerStoreOpsRoutes exposes the store console credential vault (App
// Store Connect / Google Play) and the per-repository mobile store app
// registry. Same auth/middleware chain as every other route — none of these
// invent a new one.
func (h *Handler) registerStoreOpsRoutes(app fiber.Router) {
	if h.storeOpsSvc == nil {
		return
	}
	app.Put("/v1/store/credentials/:provider", h.SaveStoreCredential)
	app.Get("/v1/store/credentials", h.ListStoreCredentials)
	app.Delete("/v1/store/credentials/:provider", h.DeleteStoreCredential)
	app.Get("/v1/repositories/:id/store/apps", h.ListStoreApps)
	app.Get("/v1/operations/apps", h.ListAllStoreApps)
	// The literal ios/android action routes are registered BEFORE the
	// :platform/verify route below. Both patterns have the same number of
	// path segments with a literal vs. a param at the same position — if
	// the :platform route were registered first it would shadow "ios" and
	// "android" here, and every action route below would 404.
	app.Post("/v1/repositories/:id/store/apps/ios/submit", h.SubmitIOSForReview)
	app.Post("/v1/repositories/:id/store/apps/ios/release", h.ReleaseIOSVersion)
	app.Post("/v1/repositories/:id/store/apps/android/promote", h.PromoteAndroidTrack)
	app.Post("/v1/repositories/:id/store/apps/android/rollout", h.SetAndroidRollout)
	app.Post("/v1/repositories/:id/store/apps/android/halt", h.HaltAndroidRollout)
	app.Post("/v1/repositories/:id/store/apps/android/resume", h.ResumeAndroidRollout)
	app.Post("/v1/repositories/:id/store/apps/:platform/verify", h.VerifyStoreOnboarding)
	app.Put("/v1/repositories/:id/store/apps/:platform/link", h.LinkStoreApp)
	app.Get("/v1/repositories/:id/store/apps/:platform/tracks", h.StoreAppTracks)
	app.Post("/v1/repositories/:id/store/apps/:platform/promote", h.PromoteStoreChannel)
	app.Post("/v1/repositories/:id/store/apps/:platform/build", h.StartStoreBuild)
	// Under the credentials prefix rather than a repository's, because the
	// listing is an account-level question: which apps can this credential
	// see, before any repository has been bound to one of them.
	app.Get("/v1/store/credentials/:provider/apps", h.ListStoreCredentialApps)
	h.registerStoreTestBuildRoutes(app)
}

// SaveStoreCredential — PUT /v1/store/credentials/:provider
// Body: {"data": {"key_id":..., "issuer_id":..., "p8":...}} for asc, or
// {"data": {"service_account_json":...}} for google_play. The credential is
// validated against the store console (ValidateAuth) before anything is
// persisted — an invalid credential never reaches the vault.
func (h *Handler) SaveStoreCredential(c *fiber.Ctx) error {
	var req struct {
		Data map[string]string `json:"data"`
	}
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.storeOpsSvc.SaveCredential(h.enrichContext(c), c.Params("provider"), req.Data); err != nil {
		return storeOpsCredentialError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// storeOpsCredentialError distinguishes the caller's own bad input (unknown
// provider, or a credential the store console itself rejected) from an
// infra/config failure (cipher not configured, encrypt/persist error) —
// same errors.Is-dispatch idiom mcpHandlerError already uses for
// domain.ErrMCP*, so a backend failure surfaces as 500 instead of being
// reported to the caller as if their input was wrong.
func storeOpsCredentialError(c *fiber.Ctx, err error) error {
	if errors.Is(err, storeops.ErrInvalidCredential) {
		return badRequest(c, err.Error())
	}
	return internalError(c, err)
}

// ListStoreCredentials — GET /v1/store/credentials
// Returns the storeops.CredentialView projection only — provider, whether
// it's configured, and when it was last written. The payload (key material,
// service account JSON) never leaves the service layer.
func (h *Handler) ListStoreCredentials(c *fiber.Ctx) error {
	views, err := h.storeOpsSvc.Credentials(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(views)
}

// DeleteStoreCredential — DELETE /v1/store/credentials/:provider
func (h *Handler) DeleteStoreCredential(c *fiber.Ctx) error {
	if err := h.storeOpsSvc.DeleteCredential(h.enrichContext(c), c.Params("provider")); err != nil {
		return storeOpsCredentialError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListStoreApps — GET /v1/repositories/:id/store/apps
func (h *Handler) ListStoreApps(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	apps, err := h.storeOpsSvc.AppsByRepository(h.enrichContext(c), id)
	if err != nil {
		return storeOpsAppError(c, err)
	}
	return c.JSON(apps)
}

// storeOpsAppError splits the registry routes' failures the same way
// storeOpsCredentialError splits the vault's: the caller's own bad input (an
// unknown platform) is a 400, a repository/platform with no registry row is a
// 404, and anything else — a database failure, a store API outage, a missing
// cipher — is this server's problem and must report 500 rather than telling
// the caller their request was malformed.
func storeOpsAppError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, storeops.ErrInvalidPlatform), errors.Is(err, storeops.ErrAppNotInStore):
		return badRequest(c, err.Error())
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	default:
		return internalError(c, err)
	}
}

// VerifyStoreOnboarding — POST /v1/repositories/:id/store/apps/:platform/verify
// The "I did the console step, check it now" button: re-runs VerifyOnboarding
// against the store API immediately instead of waiting for the next monitor
// sweep, and returns the resulting row.
func (h *Handler) VerifyStoreOnboarding(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	app, err := h.storeOpsSvc.VerifyOnboarding(h.enrichContext(c), id, c.Params("platform"))
	if err != nil {
		return storeOpsAppError(c, err)
	}
	return c.JSON(app)
}

// ListAllStoreApps — GET /v1/operations/apps
// The cross-repository mobile app registry behind the operations console's
// apps screen: every registered app, repository name already joined in.
func (h *Handler) ListAllStoreApps(c *fiber.Ctx) error {
	views, err := h.storeOpsSvc.AllApps(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(views)
}

// storeOpsActionError maps the store release actions' failures onto status
// codes the console UI branches on: a bad confirm phrase or an
// out-of-range rollout fraction is the caller's own mistake (400); an app
// not yet in the lifecycle state an action requires is a conflict with what
// was asked (409); an absent repository or registry row is 404; a store
// credential that was never saved or fails to load is 424 Failed Dependency
// so the UI can link straight to the credentials section instead of a
// generic error; anything else is this server's problem. This is the
// release-actions sibling of storeOpsCredentialError — the vault management
// routes above keep their own 400-only split, since a missing credential is
// simply the vault being empty there, not a dependency an action failed on.
func storeOpsActionError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, storeops.ErrConfirmMismatch), errors.Is(err, storeops.ErrInvalidRolloutFraction),
		errors.Is(err, storeops.ErrInvalidChannel), errors.Is(err, storeops.ErrInvalidPlatform):
		return badRequest(c, err.Error())
	case errors.Is(err, storeops.ErrAppNotReady):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, storeops.ErrStoreCredentialUnavailable):
		return c.Status(fiber.StatusFailedDependency).JSON(fiber.Map{"error": err.Error()})
	case errors.Is(err, port.ErrNotFound):
		return notFound(c, err.Error())
	default:
		return internalError(c, err)
	}
}

// confirmOnlyRequest is the body shape shared by the store release routes
// that need nothing but the production-class confirm phrase: iOS submit,
// iOS release, Android halt.
type confirmOnlyRequest struct {
	Confirm string `json:"confirm"`
}

// SubmitIOSForReview — POST /v1/repositories/:id/store/apps/ios/submit
// Body: {"confirm": "<repository name>"}.
func (h *Handler) SubmitIOSForReview(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req confirmOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.storeOpsSvc.SubmitIOS(h.enrichContext(c), id, req.Confirm, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ReleaseIOSVersion — POST /v1/repositories/:id/store/apps/ios/release
// Body: {"confirm": "<repository name>"}.
func (h *Handler) ReleaseIOSVersion(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req confirmOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.storeOpsSvc.ReleaseIOS(h.enrichContext(c), id, req.Confirm, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// promoteAndroidRequest is the PromoteAndroidTrack body. Confirm is required
// only when ToTrack is "production" — enforced server-side by the service,
// not duplicated here.
type promoteAndroidRequest struct {
	ToTrack      string  `json:"to_track"`
	UserFraction float64 `json:"user_fraction"`
	Confirm      string  `json:"confirm"`
	// From/To are the channel vocabulary the generic promote route speaks.
	// They ride on this struct because the literal Android route shadows that
	// one — see PromoteAndroidTrack.
	From string `json:"from"`
	To   string `json:"to"`
}

// PromoteAndroidTrack — POST /v1/repositories/:id/store/apps/android/promote
// Body: {"to_track": "internal"|"production", "user_fraction": 1, "confirm": "..."}.
//
// This literal route is registered before the generic :platform/promote one
// (it has to be — see registerStoreOpsRoutes), so it is the ONLY promote route
// Android can reach. A body in the channel vocabulary ({from, to}) is therefore
// handed on to PromoteStoreChannel here rather than being answered with "no
// to_track": routing an Android channel promote anywhere else is impossible,
// and leaving it unreachable would make the documented route a lie on one of
// the two platforms.
func (h *Handler) PromoteAndroidTrack(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req promoteAndroidRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.From != "" || req.To != "" {
		return h.promoteChannel(c, id, domain.MobileStorePlatformAndroid, req.From, req.To, req.Confirm)
	}
	if err := h.storeOpsSvc.PromoteAndroid(h.enrichContext(c), id, req.ToTrack, req.UserFraction, req.Confirm, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// setAndroidRolloutRequest is the SetAndroidRollout body.
type setAndroidRolloutRequest struct {
	UserFraction float64 `json:"user_fraction"`
}

// SetAndroidRollout — POST /v1/repositories/:id/store/apps/android/rollout
// Body: {"user_fraction": 0.5}. The fraction is range-checked server-side
// (svc.SetAndroidRollout) — it is user input heading for a production API,
// and the browser is not the enforcement point.
func (h *Handler) SetAndroidRollout(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req setAndroidRolloutRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.storeOpsSvc.SetAndroidRollout(h.enrichContext(c), id, req.UserFraction, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// HaltAndroidRollout — POST /v1/repositories/:id/store/apps/android/halt
// Body: {"confirm": "<repository name>"}.
func (h *Handler) HaltAndroidRollout(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req confirmOnlyRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if err := h.storeOpsSvc.HaltAndroid(h.enrichContext(c), id, req.Confirm, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ResumeAndroidRollout — POST /v1/repositories/:id/store/apps/android/resume
// No body: resume is not production-class, it only restores what was
// already rolling out before HaltAndroidRollout stopped it.
func (h *Handler) ResumeAndroidRollout(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	if err := h.storeOpsSvc.ResumeAndroid(h.enrichContext(c), id, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// storeAppListing is the ListStoreCredentialApps response. Available is
// stated rather than inferred from an empty Apps array, because "this
// credential cannot enumerate" and "this account has no apps" are different
// answers and the UI reacts to them differently: the first one opens the
// manual identifier field, the second one does not.
type storeAppListing struct {
	Available bool `json:"listing_available"`
	// Reason says WHY the list is empty when Available is false, because the
	// console reacts to the two cases differently: "not_connected" sends the
	// operator to Integrations to save a credential, "listing_unsupported"
	// opens the manual identifier field. Typing an identifier by hand is
	// useless with no credential to verify it against, so the two must not
	// collapse into one "listing failed" screen. Empty when Available is true.
	Reason string             `json:"reason,omitempty"`
	Apps   []port.StoreAppRef `json:"apps"`
}

const (
	listingReasonNotConnected = "not_connected"
	listingReasonUnsupported  = "listing_unsupported"
)

// ListStoreCredentialApps — GET /v1/store/credentials/:provider/apps
// The picker's source: every app the saved credential can see.
//
// port.ErrAppListingUnavailable answers 200 with listing_available:false, not
// a 5xx. The Play Developer API genuinely has no listing endpoint and a
// service account may not reach the Reporting API that does; that is a stable
// property of the credential, not a failure, and the operator's next step is
// to type the package name rather than to retry.
func (h *Handler) ListStoreCredentialApps(c *fiber.Ctx) error {
	apps, err := h.storeOpsSvc.ListStoreApps(h.enrichContext(c), c.Params("provider"))
	if err != nil {
		if errors.Is(err, storeops.ErrProviderNotConnected) {
			return c.JSON(storeAppListing{Available: false, Reason: listingReasonNotConnected, Apps: []port.StoreAppRef{}})
		}
		if errors.Is(err, port.ErrAppListingUnavailable) {
			return c.JSON(storeAppListing{Available: false, Reason: listingReasonUnsupported, Apps: []port.StoreAppRef{}})
		}
		return storeOpsCredentialError(c, err)
	}
	if apps == nil {
		apps = []port.StoreAppRef{}
	}
	return c.JSON(storeAppListing{Available: true, Apps: apps})
}

// linkStoreAppRequest is the LinkStoreApp body: one app as the picker listed
// it. store_app_id is optional — App Store Connect's own answer overrides it,
// and Play has no second identifier at all.
type linkStoreAppRequest struct {
	Identifier string `json:"identifier"`
	StoreAppID string `json:"store_app_id"`
	Name       string `json:"name"`
}

// LinkStoreApp — PUT /v1/repositories/:id/store/apps/:platform/link
// Body: {"identifier": "com.example.app", "store_app_id": "...", "name": "..."}.
func (h *Handler) LinkStoreApp(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req linkStoreAppRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	app, err := h.storeOpsSvc.LinkStoreApp(h.enrichContext(c), id, c.Params("platform"), port.StoreAppRef{
		StoreAppID: req.StoreAppID,
		Identifier: req.Identifier,
		Name:       req.Name,
	})
	if err != nil {
		return storeOpsAppError(c, err)
	}
	return c.JSON(app)
}

// StoreAppTracks — GET /v1/repositories/:id/store/apps/:platform/tracks
// Reads the three channels live and refreshes the row's cache with them.
func (h *Handler) StoreAppTracks(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	tracks, err := h.storeOpsSvc.Tracks(h.enrichContext(c), id, c.Params("platform"))
	if err != nil {
		return storeOpsActionError(c, err)
	}
	return c.JSON(tracks)
}

// promoteChannelRequest is the PromoteStoreChannel body. Confirm is required
// only when To is "production" — enforced server-side by the service, not
// duplicated here.
type promoteChannelRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Confirm string `json:"confirm"`
}

// PromoteStoreChannel — POST /v1/repositories/:id/store/apps/:platform/promote
// Body: {"from": "internal", "to": "external", "confirm": "..."}.
func (h *Handler) PromoteStoreChannel(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req promoteChannelRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	return h.promoteChannel(c, id, c.Params("platform"), req.From, req.To, req.Confirm)
}

// promoteChannel is the shared tail of the two routes that can carry a channel
// promotion: the generic one, and the literal Android one that shadows it.
func (h *Handler) promoteChannel(c *fiber.Ctx, id uuid.UUID, platform, from, to, confirm string) error {
	if err := h.storeOpsSvc.PromoteChannel(h.enrichContext(c), id, platform, from, to, confirm, consoleActor); err != nil {
		return storeOpsActionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// startStoreBuildRequest is the StartStoreBuild body. An empty engine means
// "whatever the repository is set to", which is the normal case — the field
// exists for a one-off run on the other machine.
type startStoreBuildRequest struct {
	Engine string `json:"engine"`
}

// StartStoreBuild — POST /v1/repositories/:id/store/apps/:platform/build
// Body: {"engine": "github_actions"|"local"|"auto"} or {}.
//
// domain.ErrNoReleaseEngine answers 409: the request is well-formed and the
// app is fine, but neither machine can run it right now — and the service has
// already parked the card on human_decision, so the console's job is to say
// what happened, not to offer a retry.
func (h *Handler) StartStoreBuild(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid repository id")
	}
	var req startStoreBuildRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	start, err := h.storeOpsSvc.StartBuild(h.enrichContext(c), id, c.Params("platform"), req.Engine, consoleActor)
	if err != nil {
		return storeOpsBuildError(c, err)
	}
	return c.JSON(start)
}

// storeOpsBuildError adds the engine-selection verdicts to the release
// actions' mapping: an unknown engine is the caller's own input (400), and
// "no engine can run this" — whether the repository pinned one that is
// unavailable or auto found neither — is a conflict with the state of the
// world (409), never a 500. A 500 would tell an operator to open a bug when
// the fix is to settle a bill or open a laptop.
func storeOpsBuildError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, storeops.ErrInvalidEngine):
		return badRequest(c, err.Error())
	case errors.Is(err, domain.ErrNoReleaseEngine), errors.Is(err, storeops.ErrEngineUnavailable),
		errors.Is(err, storeops.ErrBuildTargetUnknown):
		// The third one is a 409 for the same reason as the first two and not
		// a 400: the request was fine, the repository is not — nothing the
		// caller could have sent would have made this build startable.
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	default:
		return storeOpsActionError(c, err)
	}
}
