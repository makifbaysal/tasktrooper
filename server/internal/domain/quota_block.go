package domain

import (
	"errors"
	"fmt"
	"time"
)

// DefaultQuotaParkWindow is how long a run waits when the CLI said the usage
// limit was reached but not when it reopens.
//
// Half an hour rather than the five-hour window a Claude subscription actually
// rolls on: the reset epoch is normally present, so this only covers the case
// where it is missing, and there guessing SHORT is the cheaper mistake. A sweep
// that wakes the task early finds the limit still in force and parks it again
// for another window — one wasted CLI start. A sweep that wakes it hours late
// leaves a task idle for hours after the quota came back, and nothing else in
// the system would notice.
const DefaultQuotaParkWindow = 30 * time.Minute

// QuotaParkWindow escalates the fallback window with consecutive parks that
// found no reset time. A repeated 30m guess on a 5h window re-hits the same
// limit every half hour and burns a CLI start each time; doubling backs off
// toward the window's own length instead of hammering it, and the 5h cap is
// the longest a Claude subscription window actually runs.
func QuotaParkWindow(consecutiveParks int) time.Duration {
	if consecutiveParks < 0 {
		consecutiveParks = 0
	}
	window := DefaultQuotaParkWindow
	for i := 0; i < consecutiveParks; i++ {
		window *= 2
		if window >= 5*time.Hour {
			return 5 * time.Hour
		}
	}
	return window
}

// QuotaQueuedNoticePrefix marks a transcript line as a queued turn — parked,
// not failed, and due to answer itself once SessionQuotaSweeper resumes it —
// rather than a run failure or an ordinary (unqueued) rate-limit notice.
// Clients key a calmer, non-retry styling off it, the same way they key the
// warning bubble off RateLimitNoticePrefix.
const QuotaQueuedNoticePrefix = "**Queued:**"

// QuotaBlock is the CLI saying "this account has nothing left to spend until
// T". It is an ERROR type, unlike ResourceBlock, because it comes back from an
// executor rather than from a tool: the run did not finish and has no response
// to hand over, so there is no AgentResponse to carry a block on.
//
// What it shares with ResourceBlock is the treatment. Neither is a failure of
// the work: there is nothing to fix, nothing to retry now, and a run that fails
// on it would spend one of the task's three consecutive-failure lives on a
// billing window. So the board runner parks the task instead of failing it, and
// a sweeper — not a human, not a retry — releases it once ResumeAt has passed
// (see application/board/quota_sweeper.go).
//
// CLISessionID is what makes the resume a continuation rather than a restart:
// the parked session already read the repository, wrote some of the change and
// knows what it was in the middle of. Handing that id back to `claude -p
// --resume <id>` is the difference between finishing the task and paying for
// the exploration twice.
type QuotaBlock struct {
	// ResumeAt is when the limit is expected to lift. Always set: a caller that
	// could not parse a reset time uses DefaultQuotaParkWindow rather than a
	// zero time, because a zero time would read as "resume immediately" to the
	// sweeper and spin.
	ResumeAt time.Time
	// CLISessionID is the parked Claude Code session. Empty when the limit was
	// hit before the session announced itself (an init event that never
	// arrived), which is survivable: the resumed run starts a fresh session
	// with the same task context instead.
	CLISessionID string
	// Detail is the CLI's own wording, kept for the board card so a human can
	// see which limit was hit rather than a paraphrase of it.
	Detail string
}

func (q *QuotaBlock) Error() string {
	if q == nil {
		return "claude code usage limit reached"
	}
	return fmt.Sprintf("claude code usage limit reached, resuming at %s", q.ResumeAt.UTC().Format(time.RFC3339))
}

// QuotaBlockOf reports the usage-limit block behind err, anywhere in its wrap
// chain. It mirrors RateLimitOf, and for the same reason: the condition has to
// be recognisable at the transport, several wraps away from where it was
// raised.
func QuotaBlockOf(err error) (*QuotaBlock, bool) {
	var q *QuotaBlock
	if !errors.As(err, &q) || q == nil {
		return nil, false
	}
	return q, true
}

// UserMessage is the sentence a person sees in a CHAT when the subscription is
// spent and, for whatever reason, the turn could not be queued (see
// QueuedMessage for the normal case). It tells the human to retry by hand,
// because that is the only recourse left once queueing itself has failed. The
// one fact that makes it actionable either way is WHEN, which is why the time
// is always named.
//
// Local time, not UTC: the reader is sitting at this host's clock, and "resumes
// at 14:20Z" is a sentence nobody can act on without doing arithmetic.
//
// Error() is left alone — it is the log line and the board card's detail, where
// UTC and RFC3339 are the right choices.
func (q *QuotaBlock) UserMessage(lang string) string {
	if q == nil {
		switch lang {
		case "tr":
			return "Claude Code kullanım limiti doldu; limit yenilendikten sonra tekrar deneyin."
		default:
			return "The Claude Code usage limit is spent; try again once it renews."
		}
	}
	when := q.resumeLabel()
	switch lang {
	case "tr":
		return fmt.Sprintf("Claude Code kullanım limiti doldu; %s civarında yenilenecek, sonra tekrar deneyin. "+
			"Beklemek istemiyorsanız bu ajanı API üzerinden çalışan bir sağlayıcıya taşıyabilirsiniz.", when)
	default:
		return fmt.Sprintf("The Claude Code usage limit is spent; it renews around %s — try again after that. "+
			"If you would rather not wait, move this agent to an API-backed provider.", when)
	}
}

// QueuedMessage is the sentence a person sees in a CHAT when the subscription
// is spent and the turn HAS been queued: it will rerun itself once ResumeAt
// passes, the same way a parked board task resumes on its own sweeper. No
// action is asked of the reader, unlike UserMessage — the point of queueing is
// that there is nothing left for them to do but wait.
func (q *QuotaBlock) QueuedMessage(lang string) string {
	if q == nil {
		switch lang {
		case "tr":
			return "Claude Code kullanım limiti doldu; limit yenilenince mesajınız otomatik olarak gönderilecek."
		default:
			return "The Claude Code usage limit is spent; your message will send automatically once it renews."
		}
	}
	when := q.resumeLabel()
	switch lang {
	case "tr":
		return fmt.Sprintf("Claude Code kullanım limiti doldu; %s civarında yenilenince bu mesaj otomatik olarak gönderilecek, "+
			"beklemenize gerek yok.", when)
	default:
		return fmt.Sprintf("The Claude Code usage limit is spent; this message will send automatically once it renews around %s — "+
			"no need to wait or resend.", when)
	}
}

// QuotaNotice is a QuotaBlock that has already been turned into the sentence a
// particular reader gets.
//
// It exists because the two things that need it sit on opposite sides of the
// process. The LANGUAGE is known in the session service, which has just loaded
// the user's settings; the TRANSPORT is the SSE writer, which runs after the
// HTTP handler has returned and has no business making a database read on an
// error path to find out what language to apologise in. Localising once, where
// the answer is already in hand, and carrying the finished sentence on the error
// settles that without either layer reaching into the other.
//
// The block itself is still underneath and still findable with QuotaBlockOf, so
// nothing that wants the structured facts (ResumeAt, the CLI session) loses
// them.
type QuotaNotice struct {
	block   *QuotaBlock
	message string
}

// NewQuotaNotice localises block for lang. A nil block still yields a usable
// notice — the generic sentence — because the caller is on an error path and
// must not have to branch.
func NewQuotaNotice(block *QuotaBlock, lang string) *QuotaNotice {
	return &QuotaNotice{block: block, message: block.UserMessage(lang)}
}

// NewQuotaQueuedNotice is NewQuotaNotice's twin for a turn that WAS
// successfully parked — see QueuedMessage.
func NewQuotaQueuedNotice(block *QuotaBlock, lang string) *QuotaNotice {
	return &QuotaNotice{block: block, message: block.QueuedMessage(lang)}
}

// Error is the localised sentence itself, not a description of it. That is
// deliberate: every generic error path in the transport prints err.Error(), so
// the default rendering of this error is already the right one even where
// nothing has been taught to recognise the type.
func (n *QuotaNotice) Error() string { return n.message }

// Unwrap exposes the block so QuotaBlockOf and errors.As keep working through
// the notice.
func (n *QuotaNotice) Unwrap() error { return n.block }

// resumeLabel renders ResumeAt for a human on this host.
//
// The date is included only when the reset is not today: "18:40" is unambiguous
// for the common case (a window that reopens in a few hours) and a bare "18:40"
// for tomorrow morning would be a lie by omission.
func (q *QuotaBlock) resumeLabel() string {
	local := q.ResumeAt.Local()
	now := time.Now().Local()
	if local.YearDay() == now.YearDay() && local.Year() == now.Year() {
		return local.Format("15:04")
	}
	return local.Format("2 Jan 15:04")
}
