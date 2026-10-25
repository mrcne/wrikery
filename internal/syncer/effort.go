package syncer

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/pkg/wrike"
)

// effortField is the optional field of a task read that carries the Effort field, see wrike.TaskEffort.
const effortField = "effortAllocation"

// effortReads is the engine's client when the config asks for the Effort field: every task read carries effortAllocation,
// and the first 403 takes it out for good, since an account whose plan has no Effort field refuses every read that asks for it.
// The answer is remembered in a meta row, so later runs and the commands pay no 403 of their own, and a manual cycle asks again.
type effortReads struct {
	Client
	st    *store.Store
	log   *slog.Logger
	off   atomic.Bool
	onOff func() // the engine's hook, run once when the field goes off, so the interface learns the row changed
}

func newEffortReads(ctx context.Context, c Client, st *store.Store, log *slog.Logger) *effortReads {
	r := &effortReads{Client: c, st: st, log: log}
	if v, err := st.GetMeta(ctx, store.MetaKeyEffortUnavailable); err == nil && v == "1" {
		r.off.Store(true)
	}
	return r
}

// reset asks for the field again and clears the row, a manual cycle calls it.
func (r *effortReads) reset(ctx context.Context) error {
	r.off.Store(false)
	return r.st.SetMeta(ctx, store.MetaKeyEffortUnavailable, "")
}

// with adds the field to a read's fields while it is on.
func (r *effortReads) with(fields []string) []string {
	if r.off.Load() || slices.Contains(fields, effortField) {
		return fields
	}
	return append(slices.Clone(fields), effortField)
}

// refused tells the 403 of a read that asked for the field, turns the field off and says whether the read is to be repeated without it.
// Any other error is the caller's.
func (r *effortReads) refused(ctx context.Context, err error, asked []string) bool {
	if !slices.Contains(asked, effortField) || !isNotAllowed(err) {
		return false
	}
	if r.off.CompareAndSwap(false, true) {
		r.log.Warn("the account has no Effort field, budget.source = effort is ignored", "error", err)
		if err := r.st.SetMeta(ctx, store.MetaKeyEffortUnavailable, "1"); err != nil {
			r.log.Warn("remembering the missing Effort field", "error", err)
		}
		if r.onOff != nil {
			r.onOff()
		}
	}
	return true
}

func (r *effortReads) Tasks(ctx context.Context, p wrike.TaskParams) (wrike.TasksPage, error) {
	asked := p
	asked.Fields = r.with(p.Fields)
	page, err := r.Client.Tasks(ctx, asked)
	if err != nil && r.refused(ctx, err, asked.Fields) {
		return r.Client.Tasks(ctx, p)
	}
	if err == nil {
		fillEffort(page.Tasks, asked.Fields)
	}
	return page, err
}

func (r *effortReads) TasksByIDs(ctx context.Context, ids []string, fields []string) ([]wrike.Task, error) {
	asked := r.with(fields)
	tasks, err := r.Client.TasksByIDs(ctx, ids, asked)
	if err != nil && r.refused(ctx, err, asked) {
		return r.Client.TasksByIDs(ctx, ids, fields)
	}
	if err == nil {
		fillEffort(tasks, asked)
	}
	return tasks, err
}

// fillEffort gives a task the read asked the Effort field for and that came without it a None.
// The upsert keeps the columns on a nil Effort, which is right for a read that did not ask, and would keep a cleared budget here.
func fillEffort(tasks []wrike.Task, asked []string) {
	if !slices.Contains(asked, effortField) {
		return
	}
	for i := range tasks {
		if tasks[i].Effort == nil {
			tasks[i].Effort = &wrike.TaskEffort{Mode: "None"}
		}
	}
}
