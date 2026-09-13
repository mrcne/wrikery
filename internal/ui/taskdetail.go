package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mrcne/wrikery/internal/store"
)

type taskDetailModel struct {
	task      store.Task
	comments  []store.Comment
	logs      []store.Timelog
	state     store.OutboxState
	crumb     string
	rendered  string
	renderKey string
	vp        viewport.Model
	keys      KeyMap
	loaded    bool
}

func (d *taskDetailModel) set(msg taskLoadedMsg) {
	sameTask := d.task.ID == msg.task.ID
	d.task, d.comments, d.logs, d.crumb = msg.task, msg.comments, msg.logs, msg.crumb
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
	if renderKey != d.renderKey {
		d.rendered = renderDescription(d.task.Description, d.task.DescriptionPlain, width, mode)
		d.renderKey = renderKey
	}
	bold := lipgloss.NewStyle().Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	sending := lipgloss.NewStyle().Foreground(th.Warn).Render("(sending)")
	// Importance is ten characters on its own, so the column is one wider than the longest label.
	label := func(s string) string { return muted.Render(fmt.Sprintf("%-11s", s)) }

	var b strings.Builder
	title := bold.Render(d.task.Title)
	switch d.state {
	case store.StatePending:
		title += " " + sending
	case store.StateFailed:
		title += " " + lipgloss.NewStyle().Foreground(th.Error).Render("(failed, ! to review)")
	}
	b.WriteString(wordWrap(title, width) + "\n")
	b.WriteString(muted.Render(strings.TrimSpace(d.title()+"  "+d.crumb)) + "\n\n")

	cs := ref.statuses[d.task.CustomStatusID]
	if cs.Name == "" {
		cs.Name, cs.Group = d.task.Status, d.task.Status
	}
	b.WriteString(label("Status") + lipgloss.NewStyle().Foreground(th.StatusColor(cs)).Render(th.StatusGlyph(cs.Group)+" "+cs.Name) + "\n")
	names := make([]string, 0, len(d.task.ResponsibleIDs))
	for _, id := range d.task.ResponsibleIDs {
		names = append(names, contactName(id, ref))
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
	b.WriteString(label("Updated") + relTime(d.task.UpdatedDate, now) + "\n\n")

	if strings.TrimSpace(d.rendered) != "" {
		b.WriteString(d.rendered + "\n\n")
	}

	b.WriteString(divider(th, fmt.Sprintf("Comments (%d)", len(d.comments)), width) + "\n")
	for _, c := range d.comments {
		who := bold.Render(contactName(c.AuthorID, ref))
		// A queued comment carries the local clock, not the server's, so it is marked instead of dated.
		if strings.HasPrefix(c.ID, store.LocalIDPrefix) {
			who += " " + sending
		} else {
			who += muted.Render(", " + relTime(c.CreatedDate, now))
		}
		b.WriteString(who + "\n")
		b.WriteString(lipgloss.NewStyle().PaddingLeft(2).Width(width).Render(c.Text) + "\n")
	}
	b.WriteString("\n" + divider(th, fmt.Sprintf("Time (%d)", len(d.logs)), width) + "\n")
	for _, l := range d.logs {
		line := fmt.Sprintf("%s  %s  %.1f h", shortDate(l.TrackedDate), contactName(l.UserID, ref), l.Hours)
		if l.Comment != "" {
			line += "  " + l.Comment
		}
		if strings.HasPrefix(l.ID, store.LocalIDPrefix) {
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

func contactName(id string, ref refData) string {
	c, ok := ref.contacts[id]
	if !ok {
		return id
	}
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
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
