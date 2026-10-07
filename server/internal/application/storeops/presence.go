package storeops

import (
	"context"
	"fmt"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// storePresence is what the store console itself says about an app.
type storePresence struct {
	live        bool
	liveVersion string
	// testable is true when the console can take test builds today: an App
	// Store Connect record always can; a Play app only after its first upload,
	// which Play accepts from the console alone.
	testable bool
}

func (s *Service) storePresence(ctx context.Context, app domain.MobileStoreApp) (storePresence, error) {
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.ascClient(ctx)
		if err != nil {
			return storePresence{}, err
		}
		info, live, err := client.LiveVersion(ctx, app.StoreAppID)
		if err != nil {
			return storePresence{}, fmt.Errorf("storeops: reading the live App Store version of %s: %w", app.Identifier, err)
		}
		return storePresence{live: live, liveVersion: info.Version, testable: true}, nil
	case domain.MobileStorePlatformAndroid:
		client, err := s.playClient(ctx)
		if err != nil {
			return storePresence{}, err
		}
		version, live, err := client.LiveVersion(ctx, app.Identifier)
		if err != nil {
			return storePresence{}, fmt.Errorf("storeops: reading the Play production track of %s: %w", app.Identifier, err)
		}
		if live {
			return storePresence{live: true, liveVersion: version, testable: true}, nil
		}
		internal, err := client.TrackInfo(ctx, app.Identifier, androidTrackInternal)
		if err != nil {
			return storePresence{}, fmt.Errorf("storeops: reading the Play internal track of %s: %w", app.Identifier, err)
		}
		return storePresence{testable: internal.HasRelease}, nil
	}
	return storePresence{}, fmt.Errorf("storeops: unsupported platform %q: %w", app.Platform, ErrInvalidPlatform)
}

// adoptStorePresence moves an app to where its console says it is. An app
// linked from the console already exists there, so an unregistered row takes
// the observed state outright instead of walking onboarding; every other row
// only ever moves forward to live. It never moves a row backward.
func adoptStorePresence(app domain.MobileStoreApp, p storePresence, now time.Time) (domain.MobileStoreApp, bool) {
	if p.live {
		if app.State == domain.MobileStoreStateLive {
			return app, false
		}
		// FirstPublishedAt records a go-live this system watched happen; an app
		// that was already live when it was linked has no such moment here.
		if app.State != domain.MobileStoreStateUnregistered && app.FirstPublishedAt == nil {
			app.FirstPublishedAt = &now
		}
		app.State = domain.MobileStoreStateLive
		app.ReviewState = ""
		if app.LastReleasedVersion == "" {
			app.LastReleasedVersion = p.liveVersion
		}
		markChecklistDone(app.Checklist, now)
		return app, true
	}
	if app.State != domain.MobileStoreStateUnregistered {
		return app, false
	}
	if p.testable {
		app.State = domain.MobileStoreStateTestReady
		return app, true
	}
	app.State = domain.MobileStoreStateOnboarding
	if len(app.Checklist) == 0 {
		app.Checklist = buildChecklist(app.Platform, app.Identifier, map[string]bool{checklistPlayAppRecord: true})
	}
	return app, true
}

func markChecklistDone(checklist []domain.ChecklistItem, now time.Time) {
	for i := range checklist {
		if checklist[i].Done {
			continue
		}
		at := now
		checklist[i].Done = true
		checklist[i].VerifiedAt = &at
	}
}
