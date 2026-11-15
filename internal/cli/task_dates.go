package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

var cmdTaskDates = Command{
	Name:     "task dates",
	Synopsis: []string{"task dates T [--start DAY] [--due DAY] | --clear"},
	Short:    "set the start, the due date or both, or clear them",
	Notes: `Sets the start, the due date or both, the one not named keeps its day, and --clear takes both off.
A DAY is what the dates box of the interface reads: 2026-09-12, today, tomorrow, fri, next fri, +3d or "12 sep".
One date on a task without dates makes a one day task, as the box does, and a milestone moved by its due date stays a milestone.`,
	Flags: func() *flag.FlagSet { return new(datesOpts).flagSet() },
}

type datesOpts struct {
	start, due  string
	clear, json bool
}

func (o *datesOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("task dates", &o.json)
	fs.StringVar(&o.start, "start", "", "the start `DAY`")
	fs.StringVar(&o.due, "due", "", "the due `DAY`")
	fs.BoolVar(&o.clear, "clear", false, "take both dates off")
	return fs
}

func runTaskDates(ctx context.Context, env Env, args []string) int {
	var o datesOpts
	fs := o.flagSet()
	usage := cmdTaskDates.usage()
	positional, code, done := parse(env, fs, usage, args)
	if done {
		return code
	}
	if len(positional) != 1 {
		return usageError(env, fs, usage, "task dates takes one task, an id or a part of its title")
	}
	hasStart, hasDue := given(fs, "start"), given(fs, "due")
	switch {
	case o.clear && (hasStart || hasDue):
		return usageError(env, fs, usage, "--clear goes alone")
	case !o.clear && !hasStart && !hasDue:
		return usageError(env, fs, usage, "task dates needs --start, --due or --clear")
	}
	// The words are read before the task is looked for, a typo must not cost the fetch of a task outside the cache.
	now := env.Store.Now()
	var start, due string
	if hasStart {
		day, err := dayFlag("start", "start", o.start, now)
		if err != nil {
			return usageError(env, fs, usage, err.Error())
		}
		start = day
	}
	if hasDue {
		day, err := dayFlag("due", "due date", o.due, now)
		if err != nil {
			return usageError(env, fs, usage, err.Error())
		}
		due = day
	}
	if hasStart && hasDue && due < start {
		return usageError(env, fs, usage, "due is before start")
	}
	t, code := resolveOrReach(ctx, env, positional[0])
	if code != exitOK {
		return code
	}
	ref, err := loadRef(ctx, env.Store)
	if err != nil {
		return fail(env, err)
	}
	milestone := false
	if !o.clear && t.Dates != nil {
		// The box opens on the task's two days and the user changes one, here the flag not given is the field left alone.
		milestone = t.Dates.Type == store.DatesMilestone
		if !hasStart {
			start = shortDate(t.Dates.Start)
		}
		if !hasDue {
			due = shortDate(t.Dates.Due)
		}
		// Both given was checked above, so one of the two days is the task's own, and moving it without being asked is not this command's call.
		switch {
		case start == "" || due == "" || due >= start:
		case hasDue:
			return fail(env, fmt.Errorf("due is before the start the task has, %s, give --start as well", start))
		default:
			return fail(env, fmt.Errorf("start is after the due date the task has, %s, give --due as well", due))
		}
	}
	dates := ui.DatesFrom(start, due, milestone)
	line := func(_ context.Context, x store.Task) string {
		if x.Dates == nil || x.Dates.Start == "" && x.Dates.Due == "" {
			return "[no dates]"
		}
		return "[" + dateRange(x.Dates.Start, x.Dates.Due) + "]"
	}
	return finishUpdate(ctx, env, t, &ref, store.TaskUpdatePayload{Dates: &dates}, o.json, line, "")
}

// dayFlag reads the word of --start or --due into a day, with the field named in front of what is wrong with it, as the box names it.
// An empty word clears a field of the box, here it is a variable that was not set, and --clear is the way to clear.
func dayFlag(name, what, word string, now time.Time) (string, error) {
	if strings.TrimSpace(word) == "" {
		return "", fmt.Errorf("the %s is empty", what)
	}
	day, err := ui.ParseDay(word, now)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return day, nil
}
