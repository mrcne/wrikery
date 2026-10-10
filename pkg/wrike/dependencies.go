package wrike

import (
	"context"
	"errors"
	"net/http"
)

// Dependency is a scheduling relation between two tasks, see https://developers.wrike.com/api/v4/dependencies/.
// RelationType is one of FinishToStart, StartToStart, FinishToFinish and StartToFinish.
// LagTime is in minutes, negative for a lead. The reference keeps a project dependency's lag in whole work days of 480 minutes,
// a task dependency can hold any number of minutes.
type Dependency struct {
	ID            string `json:"id"`
	PredecessorID string `json:"predecessorId"`
	SuccessorID   string `json:"successorId"`
	RelationType  string `json:"relationType"`
	LagTime       int    `json:"lagTime"`
}

// TaskDependencies lists the dependencies of a task, the ones where it is the predecessor and the ones where it is the successor.
// Both ends get the same edge, with the same id.
func (c *Client) TaskDependencies(ctx context.Context, taskID string) ([]Dependency, error) {
	if taskID == "" {
		return nil, errors.New("wrike: task id is required")
	}
	var out []Dependency
	if _, err := c.do(ctx, http.MethodGet, "/tasks/"+taskID+"/dependencies", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
