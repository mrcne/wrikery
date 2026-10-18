package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/store"
)

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func pendingWrites(t *testing.T, st *store.Store) int {
	t.Helper()
	pending, _, err := st.Outbox().Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

// moveModel is the demo API folder with the cursor on the task, the action keys read that row.
func moveModel(t *testing.T, taskID string) Model {
	t.Helper()
	m := demoModel(t)
	if !m.list.selectByID(taskID) {
		t.Fatalf("%s is not in the API list", taskID)
	}
	return m
}

// The first H or L is the one a newcomer presses to see what the key does, and on the board L sits next to l.
// It asks once and names the move, after the yes both keys move at once.
func TestFirstStatusMoveAsksOnce(t *testing.T) {
	m := moveModel(t, "IEAATASK12") // In review, a status with a neighbour on each side
	st := m.opts.Store
	before := pendingWrites(t, st)
	row, _ := m.list.current()

	m = pump(m, keyRunes("L"))
	d, ok := m.dialog.(confirmDialog)
	if !ok {
		t.Fatalf("the first L opened %T, want the confirm box", m.dialog)
	}
	for _, want := range []string{"L moves", row.task.Title, "In review", "Blocked"} {
		if !strings.Contains(d.prompt, want) {
			t.Errorf("the prompt does not name %q:\n%s", want, d.prompt)
		}
	}
	if !strings.Contains(squeeze(m.View()), "comes only once") {
		t.Errorf("the box is not drawn:\n%s", m.View())
	}
	if n := pendingWrites(t, st); n != before {
		t.Fatalf("the first L queued a write before the yes, pending %d -> %d", before, n)
	}

	m = pump(m, keyRunes("y"))
	if m.dialog != nil {
		t.Errorf("the box stayed open after y")
	}
	if n := pendingWrites(t, st); n != before+1 {
		t.Errorf("y should queue the move, pending %d -> %d", before, n)
	}
	if v, _ := st.GetMeta(context.Background(), store.MetaKeyStatusMoveConfirmed); v != "1" {
		t.Errorf("the yes was not saved, meta %q", v)
	}
	if !strings.Contains(m.status.toast, "Status set to Blocked") {
		t.Errorf("toast = %q", m.status.toast)
	}

	// The engine's reload lands before the next key, the row says Blocked now, so H steps back to In review.
	m = pump(m, OutboxChangedMsg{Pending: 1})
	m = pump(m, keyRunes("H"))
	if m.dialog != nil {
		t.Errorf("H asked again after the yes to L, one flag covers both keys")
	}
	if n := pendingWrites(t, st); n != before+2 {
		t.Errorf("H after the yes should move at once, pending %d -> %d", before, n)
	}
	if got := lastQueuedStatus(t, st); got != "IEAAST13" {
		t.Errorf("H after the reload queued the status %s, want In review (IEAAST13)", got)
	}
}

// lastQueuedStatus is the status id of the newest outbox row, the one the last key queued.
func lastQueuedStatus(t *testing.T, st *store.Store) string {
	t.Helper()
	rows, err := st.Outbox().ListChanges(context.Background())
	if err != nil || len(rows) == 0 {
		t.Fatalf("outbox rows: %v, %d rows", err, len(rows))
	}
	last := rows[0]
	for _, r := range rows[1:] {
		if r.ID > last.ID {
			last = r
		}
	}
	var p struct {
		CustomStatusID string `json:"customStatusId"`
	}
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p.CustomStatusID
}

// The box is read slowly and a pull can move the task meanwhile, the yes was to the move the box named.
func TestYesAfterAPullRefusesAStaleMove(t *testing.T) {
	m := moveModel(t, "IEAATASK12")
	st := m.opts.Store
	before := pendingWrites(t, st)
	m = pump(m, keyRunes("L"))
	row, _ := m.list.current()
	moved := row.task
	moved.CustomStatusID, moved.Status = "IEAAST15", "Completed" // a colleague finished it
	if err := st.Tasks().Upsert(context.Background(), []store.Task{moved}); err != nil {
		t.Fatal(err)
	}
	m = pump(m, keyRunes("y"))
	if n := pendingWrites(t, st); n != before {
		t.Errorf("the yes moved a task that was not where the box said, pending %d -> %d", before, n)
	}
	if !strings.Contains(m.status.toast, "Done") || !strings.Contains(m.status.toast, "again") {
		t.Errorf("toast = %q, want the status the task has now and a word to press again", m.status.toast)
	}
	if v, _ := st.GetMeta(context.Background(), store.MetaKeyStatusMoveConfirmed); v != "1" {
		t.Errorf("the yes was given, meta %q", v)
	}
}

// The settings screen shows the saved answer and flips it, the way back after a yes given by mistake.
func TestSettingsBringTheQuestionBack(t *testing.T) {
	m := moveModel(t, "IEAATASK12")
	st := m.opts.Store
	askRow := func() string {
		t.Helper()
		for _, r := range m.settings.rows() {
			if r.label == "Ask before H and L" {
				return r.value
			}
		}
		t.Fatal("the settings have no row for the question")
		return ""
	}
	if got := askRow(); got != "yes" {
		t.Errorf("before the yes the row says %q", got)
	}
	m = pump(m, keyRunes("L"), keyRunes("y"))
	if got := askRow(); got != "no" {
		t.Errorf("after the yes the row says %q", got)
	}
	m = pump(m, toggleMoveAskMsg{})
	if got := askRow(); got != "yes" {
		t.Errorf("after the flip the row says %q", got)
	}
	if v, _ := st.GetMeta(context.Background(), store.MetaKeyStatusMoveConfirmed); v != "0" {
		t.Errorf("the flip was not saved, meta %q", v)
	}
	m = pump(m, keyRunes("L"))
	if _, ok := m.dialog.(confirmDialog); !ok {
		t.Errorf("after the flip L opened %T, want the box again", m.dialog)
	}
}

// No or esc leaves the move unmade and the question unanswered, it returns at the next press.
func TestCancelledStatusMoveAsksAgain(t *testing.T) {
	m := moveModel(t, "IEAATASK12")
	st := m.opts.Store
	before := pendingWrites(t, st)
	for _, cancel := range []tea.KeyMsg{{Type: tea.KeyEsc}, keyRunes("n")} {
		m = pump(m, keyRunes("L"))
		if _, ok := m.dialog.(confirmDialog); !ok {
			t.Fatalf("L opened %T before %s, want the confirm box", m.dialog, cancel)
		}
		m = pump(m, cancel)
		if m.dialog != nil {
			t.Errorf("%s left the box open", cancel)
		}
	}
	if n := pendingWrites(t, st); n != before {
		t.Errorf("a cancelled move queued a write, pending %d -> %d", before, n)
	}
	if v, _ := st.GetMeta(context.Background(), store.MetaKeyStatusMoveConfirmed); v != "" {
		t.Errorf("a cancel saved the flag, meta %q", v)
	}
}

// A press the workflow refuses shows its line and asks nothing, the box is for a move that would happen.
func TestRefusedStatusMoveDoesNotAsk(t *testing.T) {
	m := moveModel(t, "IEAATASK06") // Backlog, the first status of the Engineering workflow
	next, _ := m.handleKey(keyRunes("H"))
	m = next.(Model)
	if m.dialog != nil {
		t.Errorf("a refused H opened %T", m.dialog)
	}
	if !strings.Contains(m.status.toast, "already at the first status") {
		t.Errorf("toast = %q", m.status.toast)
	}
}
