package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/store"
)

type taskDetailModel struct {
	task         store.Task
	comments     []store.Comment
	logs         []store.Timelog
	subtasks     []store.Task
	deps         []store.Dependency
	related      map[string]store.Task
	state        store.OutboxState
	crumb        string
	rendered     string
	renderKey    string
	renderedFrom string // the description the render came from, compared instead of copied into the key
	vp           viewport.Model
	keys         KeyMap
	loaded       bool
}

func (d *taskDetailModel) set(msg taskLoadedMsg) {
	sameTask := d.task.ID == msg.task.ID
	d.task, d.comments, d.logs, d.crumb = msg.task, msg.comments, msg.logs, msg.crumb
	d.subtasks, d.deps, d.related = msg.subtasks, msg.deps, msg.related
	d.state = msg.states[msg.task.ID]
	d.loaded = true
	if !sameTask {
		d.vp.GotoTop()
	}
}

func (d taskDetailModel) title() string {
	if n := taskNumber(d.task.Permalink); n != "" {
		return "#" + n
	}
	return "Task"
}

// layout rebuilds the viewport content. The root calls it from Update, View only reads what it left behind.
// The description render is cached on task id, updated date and width, so a resize or a focus change costs nothing.
// The description itself is compared as well, since a queued edit changes it before the updated date moves.
func (d *taskDetailModel) layout(th Theme, ref refData, now time.Time, width, height int, mode string) {
	d.vp.Width, d.vp.Height = width, height
	if !d.loaded || width <= 0 {
		d.vp.SetContent(lipgloss.NewStyle().Foreground(th.Muted).Render("Select a task."))
		return
	}
	if th.ASCII {
		// A terminal that cannot draw the theme glyphs cannot draw glamour's bullets and rules either.
		mode = "ascii"
	}
	// The theme mode is resolved once at startup and never changes while the process runs, so it stays out of the key.
	renderKey := fmt.Sprintf("%s|%s|%d", d.task.ID, d.task.UpdatedDate, width)
	// Everything below is wrapped or padded to the width before the box strips the joiners,
	// so the text is stripped first, see stableWidth, or the end of a wrapped line is cut.
	if renderKey != d.renderKey || d.task.Description != d.renderedFrom {
		d.rendered = RenderDescription(stableWidth(d.task.Description), stableWidth(d.task.DescriptionPlain), width, mode)
		d.renderKey, d.renderedFrom = renderKey, d.task.Description
	}
	bold := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	sending := lipgloss.NewStyle().Foreground(th.Warn).Render("(sending)")
	label := func(s string) string { return muted.Render(fmt.Sprintf("%-*s", labelWidth, s)) }

	var b strings.Builder
	title := bold.Render(stableWidth(d.task.Title))
	switch d.state {
	case store.StatePending:
		title += " " + sending
	case store.StateFailed:
		title += " " + lipgloss.NewStyle().Foreground(th.Error).Render("(failed, ! to review)")
	}
	b.WriteString(wordWrap(title, width) + "\n")
	b.WriteString(muted.Render(strings.TrimSpace(d.title()+"  "+stableWidth(d.crumb))) + "\n\n")

	cs := ref.statusOf(d.task)
	if cs.Name == "" {
		cs.Name = d.task.Status
	}
	b.WriteString(label("Status") + lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group)+" "+cs.Name) + "\n")
	names := make([]string, 0, len(d.task.ResponsibleIDs))
	for _, id := range d.task.ResponsibleIDs {
		names = append(names, stableWidth(contactName(id, ref)))
	}
	if len(names) == 0 {
		names = []string{"nobody"}
	}
	b.WriteString(label("Assignees") + strings.Join(names, ", ") + "\n")
	if dt := d.task.Dates; dt != nil && (dt.Start != "" || dt.Due != "") {
		b.WriteString(label("Dates") + dateRange(dt.Start, dt.Due) + "\n")
	}
	if d.task.Importance != "" && d.task.Importance != "Normal" {
		b.WriteString(label("Importance") + d.task.Importance + "\n")
	}
	for i, id := range d.task.SuperTaskIDs {
		name := "Subtask of"
		if i > 0 {
			name = ""
		}
		b.WriteString(label(name) + d.relatedLine(id, ref, th, width-labelWidth) + "\n")
	}
	if d.task.AttachmentCount > 0 {
		b.WriteString(label("Attachments") + strconv.Itoa(d.task.AttachmentCount) + "\n")
	}
	b.WriteString(label("Updated") + relTime(d.task.UpdatedDate, now) + "\n\n")

	if strings.TrimSpace(d.rendered) != "" {
		b.WriteString(d.rendered + "\n\n")
	}

	if len(d.subtasks) > 0 {
		b.WriteString(divider(th, fmt.Sprintf("Subtasks (%d)", len(d.subtasks)), width) + "\n")
		for _, s := range d.subtasks {
			b.WriteString(taskLine(s, ref, th, width) + "\n")
		}
		b.WriteString("\n")
	}
	if len(d.deps) > 0 {
		b.WriteString(divider(th, fmt.Sprintf("Dependencies (%d)", len(d.deps)), width) + "\n")
		for _, dep := range d.deps {
			role, other := "successor", dep.SuccessorID
			if dep.SuccessorID == d.task.ID {
				role, other = "predecessor", dep.PredecessorID
			}
			rel := RelationText(dep.RelationType)
			if lag := LagText(dep.LagMinutes); lag != "" {
				rel += ", " + lag
			}
			line := label(role) + d.relatedLine(other, ref, th, width-labelWidth)
			// The pane is often too narrow for the title and the relation side by side, and the frame would cut the relation.
			if ansi.StringWidth(line)+2+len(rel) <= width {
				b.WriteString(line + "  " + muted.Render(rel) + "\n")
			} else {
				b.WriteString(line + "\n" + label("") + muted.Render(rel) + "\n")
			}
		}
		b.WriteString("\n")
	}

	b.WriteString(divider(th, fmt.Sprintf("Comments (%d)", len(d.comments)), width) + "\n")
	for _, c := range d.comments {
		who := bold.Render(contactName(c.AuthorID, ref))
		// A queued comment carries the local clock, not the server's, so it is marked instead of dated.
		if store.IsLocalID(c.ID) {
			who += " " + sending
		} else {
			who += muted.Render(", " + relTime(c.CreatedDate, now))
		}
		b.WriteString(who + "\n")
		b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Width(width).Render(stableWidth(c.Text)) + "\n")
	}
	b.WriteString("\n" + divider(th, fmt.Sprintf("Time (%d)", len(d.logs)), width) + "\n")
	for _, l := range d.logs {
		line := fmt.Sprintf("%s  %s  %s", shortDate(l.TrackedDate), contactName(l.UserID, ref), hoursText(l.Hours))
		if l.Comment != "" {
			line += "  " + stableWidth(l.Comment)
		}
		if store.IsLocalID(l.ID) {
			line += " " + sending
		}
		if l.UserID == ref.meID {
			line = lipgloss.NewStyle().Foreground(th.Text).Render(line)
		} else {
			line = muted.Render(line)
		}
		b.WriteString(line + "\n")
	}
	d.vp.SetContent(b.String())
}

func (d taskDetailModel) Update(msg tea.KeyMsg) (taskDetailModel, tea.Cmd) {
	switch {
	case key.Matches(msg, d.keys.Down):
		d.vp.ScrollDown(1)
	case key.Matches(msg, d.keys.Up):
		d.vp.ScrollUp(1)
	case key.Matches(msg, d.keys.HalfDown):
		d.vp.HalfPageDown()
	case key.Matches(msg, d.keys.HalfUp):
		d.vp.HalfPageUp()
	case key.Matches(msg, d.keys.Top):
		d.vp.GotoTop()
	case key.Matches(msg, d.keys.Bottom):
		d.vp.GotoBottom()
	case key.Matches(msg, d.keys.Left):
		return d, intent(focusMsg{pane: paneList})
	}
	return d, nil
}

func (d taskDetailModel) View() string { return d.vp.View() }

// labelWidth is the column the metadata labels take. Attachments is eleven characters on its own, so two more leave a gap.
const labelWidth = 13

// relatedLine names a super task or the other end of a dependency, cut to the width left of the label.
// The pull only brings the followed scopes, so an end can be a task the cache has never seen.
func (d taskDetailModel) relatedLine(id string, ref refData, th Theme, width int) string {
	t, ok := d.related[id]
	if !ok {
		return lipgloss.NewStyle().Foreground(th.Muted).Render(ansi.Truncate("a task outside the followed spaces", max(width, 4), "..."))
	}
	return taskLine(t, ref, th, width)
}

// taskLine draws a related task the way a list row starts, the status glyph in its color and the title, muted once done.
// A title longer than the width is cut with an ellipsis, the frame would cut it without one.
func taskLine(t store.Task, ref refData, th Theme, width int) string {
	cs := ref.statusOf(t)
	title := ansi.Truncate(stableWidth(t.Title), max(width-2, 4), "...")
	if isDoneGroup(cs.Group) {
		title = lipgloss.NewStyle().Foreground(th.Muted).Render(title)
	}
	return lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group)) + " " + title
}

// RelationText spells a dependency type the way the Gantt chart does, see https://developers.wrike.com/api/v4/dependencies/.
// The command line prints the same words, which is why it is exported.
func RelationText(relation string) string {
	switch relation {
	case "FinishToStart":
		return "finish to start"
	case "StartToStart":
		return "start to start"
	case "FinishToFinish":
		return "finish to finish"
	case "StartToFinish":
		return "start to finish"
	}
	return relation
}

// LagText writes a lag in work days when it is whole ones, the unit the Gantt chart uses, and in hours to one decimal otherwise.
// A negative value is a lead, the successor may start before the predecessor is done.
func LagText(minutes int) string {
	if minutes == 0 {
		return ""
	}
	word := "lag"
	if minutes < 0 {
		word, minutes = "lead", -minutes
	}
	if minutes%480 == 0 {
		if days := minutes / 480; days != 1 {
			return fmt.Sprintf("%s %d days", word, days)
		}
		return word + " 1 day"
	}
	return word + " " + strconv.FormatFloat(math.Round(float64(minutes)/6)/10, 'f', -1, 64) + " h"
}

func contactName(id string, ref refData) string {
	c, ok := ref.contacts[id]
	if !ok {
		return id
	}
	return c.Name()
}

// dateRange writes the arrow only when a task has both ends, so a single date does not trail off into nothing.
func dateRange(start, due string) string {
	switch {
	case start == "":
		return shortDate(due)
	case due == "":
		return shortDate(start)
	}
	return shortDate(start) + " -> " + shortDate(due)
}

func shortDate(s string) string {
	if len(s) < 10 {
		return s
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d %s", t.Day(), t.Month().String()[:3])
}

// relTime compares calendar days rather than elapsed hours.
// Counting hours calls a stamp from two days back "yesterday" as long as it is less than 48 hours old.
func relTime(rfc string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return rfc
	}
	t = t.In(now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case day.Equal(today):
		// A stamp a little ahead of this machine is clock skew against the server, not something that happens later.
		return ago(max(now.Sub(t), 0))
	case day.Equal(today.AddDate(0, 0, -1)):
		return "yesterday"
	}
	return shortDate(t.Format("2006-01-02"))
}
