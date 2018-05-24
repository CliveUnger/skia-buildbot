package gerrit

import (
	"fmt"
	"strings"
)

// EditChange is a helper for creating a new patch set on an existing
// Change. Pass in a function which creates and modifies a ChangeEdit, and the
// result will be automatically published as a new patch set, or in the case of
// failure, reverted.
func EditChange(g GerritInterface, ci *ChangeInfo, fn func(GerritInterface, *ChangeInfo) error) (rvErr error) {
	defer func() {
		if rvErr == nil {
			rvErr = g.PublishChangeEdit(ci)
		}
		if rvErr != nil {
			if err := g.DeleteChangeEdit(ci); err != nil {
				rvErr = fmt.Errorf("%s and failed to delete edit with: %s", rvErr, err)
			}
		}
	}()
	return fn(g, ci)
}

// CreateAndEditChange is a helper which creates a new Change in the given
// project based on the given branch with the given commit message. Pass in a
// function which modifies a ChangeEdit, and the result will be automatically
// published as a new patch set, or in the case of failure, reverted. If an
// error is encountered after the Change is created, the ChangeInfo is returned
// so that the caller can decide whether to abandon the change or try again.
func CreateAndEditChange(g GerritInterface, project, branch, commitMsg string, fn func(GerritInterface, *ChangeInfo) error) (*ChangeInfo, error) {
	ci, err := g.CreateChange(project, branch, strings.Split(commitMsg, "\n")[0])
	if err != nil {
		return nil, err
	}
	return ci, EditChange(g, ci, func(g GerritInterface, ci *ChangeInfo) error {
		if err := g.SetCommitMessage(ci, commitMsg); err != nil {
			return err
		}
		return fn(g, ci)
	})
}
