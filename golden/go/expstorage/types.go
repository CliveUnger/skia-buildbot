package expstorage

import (
	"context"

	"go.skia.org/infra/go/gevent"
	"go.skia.org/infra/go/util"
	"go.skia.org/infra/golden/go/types"
	"go.skia.org/infra/golden/go/types/expectations"
)

// Events emitted by this package.
const (
	// EV_EXPSTORAGE_CHANGED is the event emitted when expectations change.
	// Callback argument: []string with the names of changed tests.
	EV_EXPSTORAGE_CHANGED = "expstorage:changed"
)

func init() {
	// Register the codec for EV_EXPSTORAGE_CHANGED so we can have distributed events.
	gevent.RegisterCodec(EV_EXPSTORAGE_CHANGED, util.JSONCodec(&EventExpectationChange{}))
}

// ExpectationsStore Defines the storage interface for expectations.
type ExpectationsStore interface {
	// Get the current classifications for image digests. The keys of the
	// expectations map are the test names.
	Get() (expectations.Expectations, error)

	// AddChange writes the given classified digests to the database and records the
	// user that made the change.
	// TODO(kjlubick): This interface leads to a potential race condition if two
	// users on the front-end click Positive and Negative for the same testname/digest.
	//  A less racy interface would take an "old value"/"new value" so that if the
	// old value didn't match, we could reject the change.
	AddChange(ctx context.Context, changes expectations.Expectations, userId string) error

	// QueryLog allows to paginate through the changes in the expectations.
	// If details is true the result will include a list of triage operations
	// that were part a change.
	QueryLog(ctx context.Context, offset, size int, details bool) ([]TriageLogEntry, int, error)

	// UndoChange reverts a change by setting all testname/digest pairs of the
	// original change to the label they had before the change was applied.
	// A new entry is added to the log with a reference to the change that was
	// undone. The expectations returned are the expectations that were changed,
	// with the newly reverted values.
	UndoChange(ctx context.Context, changeID, userID string) (expectations.Expectations, error)

	// ForChangeList returns a new ExpectationStore that will deal with the Expectations for a
	// ChangeList with the given id (aka a CLExpectations). Any Expectations added to the returned
	// ExpectationStore will be kept separate from the master branch. Any Expectations
	// returned should be treated as the delta between the MasterBranch and the given issue.
	// The parameter crs is the CodeReviewSystem (e.g. "gerrit", "github") and id is the id
	// of the CL in that CRS. (This allows us to avoid a collision between two CLs with the same
	// id in the event that we transition from one CRS to another).
	ForChangeList(id, crs string) ExpectationsStore
}

// TriageDetail represents one changed digest and the label that was
// assigned as part of the triage operation.
type TriageDetail struct {
	TestName types.TestName `json:"test_name"`
	Digest   types.Digest   `json:"digest"`
	Label    string         `json:"label"`
}

// TriageLogEntry represents one change in the expectation store.
type TriageLogEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	TS          int64          `json:"ts"` // is milliseconds since the epoch
	ChangeCount int            `json:"changeCount"`
	Details     []TriageDetail `json:"details"`
}

// EventExpectationChange is the structure that is sent in expectation change events.
// When the change happened on the master branch, CRSAndCLID will be "", otherwise it will
// be a string unique to the CodeReviewSystem and ChangeList for which the ExpectationDelta belongs.
type EventExpectationChange struct {
	CRSAndCLID       string
	ExpectationDelta expectations.Expectations
}
