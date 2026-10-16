package domain

import "github.com/google/uuid"

// NotificationChannelKind names a delivery channel. Only in_app and desktop are
// v1 names; the rest are reserved so a new adapter registers under a name the
// sources never have to learn.
type NotificationChannelKind string

const (
	NotificationChannelInApp   NotificationChannelKind = "in_app"
	NotificationChannelDesktop NotificationChannelKind = "desktop"
	NotificationChannelIOSPush NotificationChannelKind = "ios_push"
	NotificationChannelSlack   NotificationChannelKind = "slack"
	NotificationChannelEmail   NotificationChannelKind = "email"
	NotificationChannelWebhook NotificationChannelKind = "webhook"
)

type NotificationSource string

const (
	NotificationSourceIncident NotificationSource = "incident"
	NotificationSourceAlert    NotificationSource = "alert"
	NotificationSourceRelease  NotificationSource = "release"
)

// NotificationKind is what happened, so a channel can word, group or route a
// notification without parsing its title.
type NotificationKind string

const (
	NotificationIncidentOpened    NotificationKind = "incident_opened"
	NotificationIncidentRecovered NotificationKind = "incident_recovered"
	NotificationRemedyProposed    NotificationKind = "remedy_proposed"
	NotificationAlertFiring       NotificationKind = "alert_firing"
	NotificationAlertResolved     NotificationKind = "alert_resolved"
	NotificationRollbackSuggested NotificationKind = "rollback_suggested"
	NotificationReleaseRolledBack NotificationKind = "release_rolled_back"
)

// NotificationActionApproveRollback confirms a suggested rollback of the
// release the notification names.
const NotificationActionApproveRollback = "approve_rollback"

// NotificationAction: Label is a translation key, not text — each channel
// words it in the reader's language.
type NotificationAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Notification is channel-neutral: Link is an app route (not a URL), so each
// channel decides how to open it.
type Notification struct {
	Kind     NotificationKind     `json:"kind"`
	Title    string               `json:"title"`
	Body     string               `json:"body"`
	Severity IncidentSeverity     `json:"severity,omitempty"`
	Link     string               `json:"link,omitempty"`
	Source   NotificationSource   `json:"source"`
	SourceID string               `json:"source_id,omitempty"`
	Actions  []NotificationAction `json:"actions,omitempty"`
}

func IncidentLink(id uuid.UUID) string {
	return "/operations/issues?id=" + id.String()
}

func ReleaseLink(id uuid.UUID) string {
	return "/operations/changes?release=" + id.String()
}
