package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
)

// testBoardList is two lanes over the workflows of testWorkflows:
// Ada holds two New cards and one In Progress, Bartek one On Hold and one on the other workflow, which lands in a bucket.
func testBoardList() taskListModel {
	l := newTaskList(defaultKeyMap())
	l.ref = testWorkflows()
	l.ref.meID = "ME"
	l.ref.contacts = map[string]store.Contact{
		"ME": {FirstName: "Ada", LastName: "Nowak"},
		"B1": {FirstName: "Bartek", LastName: "Lis"},
	}
	tasks := []store.Task{
		{ID: "a1", Title: "Ada new", Status: "Active", CustomStatusID: "S1", ResponsibleIDs: []string{"ME"}},
		{ID: "a2", Title: "Ada also new", Status: "Active", CustomStatusID: "S1", ResponsibleIDs: []string{"ME"}},
		{ID: "a3", Title: "Ada in progress", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"ME"}},
		{ID: "b1", Title: "Bartek on hold", Status: "Active", CustomStatusID: "S4", ResponsibleIDs: []string{"B1"}},
		{ID: "b2", Title: "Bartek planned", Status: "Active", CustomStatusID: "T1", ResponsibleIDs: []string{"B1"}},
	}
	l.setRows("F1", "API", tasks, nil, "", false)
	l.setGroup(groupAssignee)
	return l
}

func boardPress(t *testing.T, b boardModel, l *taskListModel, k string) boardModel {
	t.Helper()
	b, _ = b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}, l, nil)
	return b
}

func selected(l *taskListModel) string {
	cur, _ := l.current()
	return cur.task.ID
}

func TestBoardCursorWalksColumnsLanesAndCards(t *testing.T) {
	l := testBoardList()
	b := boardModel{keys: defaultKeyMap(), width: 100, height: 20}
	b.fit(&l, nil)
	steps := []struct{ key, want, why string }{
		{"j", "a2", "the next card in the cell"},
		{"j", "a2", "Bartek has no New card, so the column ends here"},
		{"l", "a3", "the next column with a card in the lane, at the nearest index"},
		{"l", "b1", "Ada has nothing further right, so the nearest lane with a card in the next column"},
		{"}", "b1", "there is no lane after Bartek"},
		{"{", "a1", "Ada has no On Hold card, so the first card of her lane"},
		{"G", "a2", "the last card of the column over all lanes"},
		{"g", "a1", "and the first"},
		{"l", "a3", "back to In Progress"},
		{"h", "a1", "the previous column at the same index, a3 is the first card of its cell"},
	}
	for _, s := range steps {
		b = boardPress(t, b, &l, s.key)
		if got := selected(&l); got != s.want {
			t.Errorf("%s: on %s, want %s (%s)", s.key, got, s.want, s.why)
		}
	}
	if !l.inBucket("b2") {
		t.Error("b2 sits on the other workflow, its column is a bucket")
	}
	if l.inBucket("a1") || l.inBucket("not-on-the-board") {
		t.Error("a1 sits on the main workflow, and a task off the board is not in a bucket")
	}
}

func TestBoardFitKeepsTheCardAndItsDividerInView(t *testing.T) {
	l := testBoardList()
	b := boardModel{keys: defaultKeyMap(), width: 100, height: 5}
	l.selectByID("a2")
	b.fit(&l, nil)
	// Body lines: Ada's divider, a1 on two lines, the gap, a2 on two lines.
	// Four body lines fit, so the offset is 2.
	if b.offset != 2 {
		t.Errorf("offset = %d, want 2", b.offset)
	}
	l.selectByID("a1")
	b.fit(&l, nil)
	if b.offset != 0 {
		t.Errorf("the first card brings the lane's divider back, offset = %d", b.offset)
	}
	if b.last != (boardCell{lane: 0, col: 0}) || b.lastIdx != 0 {
		t.Errorf("fit remembers the cell, got %+v index %d", b.last, b.lastIdx)
	}
}

func TestBoardFallbackStaysInTheCell(t *testing.T) {
	l := testBoardList()
	b := boardModel{keys: defaultKeyMap(), width: 100, height: 20}
	l.selectByID("a1")
	b.fit(&l, nil)
	// a1 moved on and is gone from the rows, a2 takes its index in the cell.
	l.setRows("F1", "API", []store.Task{
		{ID: "a2", Title: "Ada also new", Status: "Active", CustomStatusID: "S1", ResponsibleIDs: []string{"ME"}},
		{ID: "a3", Title: "Ada in progress", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"ME"}},
	}, nil, "a1", false)
	if p := b.fallback(&l); l.all[l.rows[p]].task.ID != "a2" {
		t.Errorf("fallback picked %s, want a2", l.all[l.rows[p]].task.ID)
	}
	b.last, b.lastIdx = boardCell{lane: 0, col: 0}, 0
	l.setRows("F1", "API", []store.Task{
		{ID: "a3", Title: "Ada in progress", Status: "Active", CustomStatusID: "S2", ResponsibleIDs: []string{"ME"}},
	}, nil, "a1", false)
	if p := b.fallback(&l); l.all[l.rows[p]].task.ID != "a3" {
		t.Errorf("with the cell empty the nearest column of the lane, got %s", l.all[l.rows[p]].task.ID)
	}
}

func TestFitColumnsCollapsesEmptyOnesAndSlides(t *testing.T) {
	cols := []boardColumn{
		{title: "New", rows: []int{0}}, {title: "Doing", rows: []int{1}}, {title: "Hold"},
		{title: "Review", rows: []int{2}}, {title: "QA", rows: []int{3}},
	}
	// Four card columns of 21, "Hold (0)" at 8, four gaps of 2: 100 in all.
	w := fitColumns(cols, 118, 0, 0)
	if w.first != 0 || w.last != 4 || w.widths[2] != 8 || w.widths[0] != 25 || w.left != 0 || w.right != 0 {
		t.Errorf("all fit at 118 and the spare 18 goes to the card columns: %+v", w)
	}
	w = fitColumns(cols, 60, 0, 3)
	if w.first != 1 || w.last != 3 || w.left != 1 || w.right != 1 {
		t.Errorf("at 60 the cards shrink to the floor and the window slides until the cursor column 3 is inside: %+v", w)
	}
	w = fitColumns(cols, 30, 0, 0)
	if w.first != 0 || w.last != 0 || w.right != 4 {
		t.Errorf("at 30 one column at a time: %+v", w)
	}
	if empty := fitColumns(nil, 100, 0, 0); empty.last != -1 {
		t.Errorf("no columns: %+v", empty)
	}
}

func TestBoardViewDrawsHeaderLanesAndCards(t *testing.T) {
	l := testBoardList()
	b := boardModel{keys: defaultKeyMap(), width: 100, height: 12}
	b.fit(&l, nil)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	view := b.View(th, l.ref, time.Time{}, &l, true)
	for _, want := range []string{"New (2)", "In Progress (1)", "On Hold (1)", "Task workflow (1)", "-- Ada Nowak (me) (3) ", "-- Bartek Lis (2) ", "> Ada new", "Ada also new", "Bartek planned"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if got := strings.Count(view, "\n") + 1; got != 12 {
		t.Errorf("the board draws exactly its height, got %d lines", got)
	}
	empty := newTaskList(defaultKeyMap())
	empty.ref = testWorkflows()
	empty.setRows("F1", "API", nil, nil, "", false)
	b.fit(&empty, nil)
	view = b.View(th, empty.ref, time.Time{}, &empty, true)
	if !strings.Contains(view, "New (0)") || !strings.Contains(view, "no tasks here") {
		t.Errorf("an empty board still shows the standard columns and says it is empty:\n%s", view)
	}
}

func TestMoveStatusRefusesABucketCard(t *testing.T) {
	m := Model{keys: defaultKeyMap(), shape: shapeBoard}
	m.list = testBoardList()
	m.ref = m.list.ref
	m.list.selectByID("b2")
	m.board.fit(&m.list, nil)
	cur, _ := m.list.current()
	if cmd := m.moveStatus(cur.task, +1); cmd == nil || !strings.Contains(m.status.toast, "another workflow") {
		t.Errorf("a card in a bucket column is refused with a toast, got %q", m.status.toast)
	}
	m.list.selectByID("a1")
	cur, _ = m.list.current()
	cmd := m.moveStatus(cur.task, +1)
	if cmd == nil {
		t.Fatal("a card on the main workflow moves")
	}
	if msg, ok := cmd().(submitStatusMsg); !ok || msg.statusID != "S2" {
		t.Errorf("L on a1 sends %#v, want the In Progress status", cmd())
	}
}

func TestEscOnTheBoardReturnsToTheList(t *testing.T) {
	m := New(Options{Now: time.Now})
	m.shape, m.focus, m.width, m.height = shapeBoard, paneDetail, 160, 40
	m.list = testBoardList()
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g := got.(Model); g.shape != shapeBoard || g.focus != paneBoard {
		t.Errorf("esc on the detail pane gives the board the width back first, got shape %d pane %d", g.shape, g.focus)
	}
	got, _ = got.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g := got.(Model); g.shape != shapeList || g.focus != paneList {
		t.Errorf("esc on the board itself returns to the list, got shape %d pane %d", g.shape, g.focus)
	}
	// With one pane shown the one pane rule would count down from the detail and land on the list, the board branch comes first.
	narrow := New(Options{Now: time.Now})
	narrow.shape, narrow.focus, narrow.width, narrow.height = shapeBoard, paneDetail, 70, 20
	narrow.list = testBoardList()
	got, _ = narrow.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g := got.(Model); g.shape != shapeBoard || g.focus != paneBoard {
		t.Errorf("at 70 esc on the detail lands on the board, got shape %d pane %d", g.shape, g.focus)
	}
	got, _ = got.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if g := got.(Model); g.shape != shapeList || g.focus != paneList {
		t.Errorf("at 70 esc on the board returns to the list, got shape %d pane %d", g.shape, g.focus)
	}
}

func TestTheFirstBoardOfASessionStartsItsWindowAtTheLeft(t *testing.T) {
	// The rows arrive while the list is shown and the board has no width yet.
	// A window placed then must not stick once b gives the board its width.
	m := New(Options{Now: time.Now})
	m.width, m.height = 60, 40
	m.list.ref = testWorkflows()
	m.selectedNode.id = "F1"
	tasks := []store.Task{
		{ID: "a1", Title: "Ada new", Status: "Active", CustomStatusID: "S1"},
		{ID: "a3", Title: "Ada in progress", Status: "Active", CustomStatusID: "S2"},
		{ID: "b1", Title: "Bartek on hold", Status: "Active", CustomStatusID: "S4"},
		{ID: "b2", Title: "Bartek planned", Status: "Active", CustomStatusID: "T1"},
	}
	got, _ := m.Update(tasksLoadedMsg{nodeID: "F1", crumb: "API", tasks: tasks, selectID: "b1"})
	got, _ = got.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	g := got.(Model)
	// Four columns at the floor of 16 and 49 cells next to the markers: the window holds two, and the one with the cursor is the second.
	if g.board.firstCol != 1 || !strings.Contains(g.View(), "< 1 ") {
		t.Errorf("the window starts as far left as the cursor column allows, got first column %d:\n%s", g.board.firstCol, g.View())
	}
}

func TestLaneHeightCountsOnlyTheDrawnColumns(t *testing.T) {
	l := newTaskList(defaultKeyMap())
	l.ref = testWorkflows()
	l.ref.meID = "ME"
	l.ref.contacts = map[string]store.Contact{"ME": {FirstName: "Ada", LastName: "Nowak"}, "B1": {FirstName: "Bartek", LastName: "Lis"}}
	tasks := []store.Task{
		{ID: "a1", Title: "Short", Status: "Active", CustomStatusID: "S1", ResponsibleIDs: []string{"ME"}},
		{ID: "a2", Title: "Investigate a sync", Status: "Active", CustomStatusID: "S4", ResponsibleIDs: []string{"ME"}},
		{ID: "b1", Title: "Bartek planned", Status: "Active", CustomStatusID: "T1", ResponsibleIDs: []string{"B1"}},
	}
	l.setRows("F1", "API", tasks, nil, "", false)
	l.setGroup(groupAssignee)
	b := boardModel{keys: defaultKeyMap(), width: 40, height: 10}
	b.fit(&l, nil)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	lines := strings.Split(b.View(th, l.ref, time.Time{}, &l, true), "\n")
	// At 40 the window holds New and the empty In Progress. On Hold is off screen, where the long title would wrap to a third line.
	// Ada's lane is two lines, Short and its meta line, and Bartek's lane, with its one card off screen, is its divider alone.
	if !strings.Contains(lines[2], "> Short") || !strings.Contains(lines[4], "-- Bartek Lis") || strings.TrimSpace(lines[5]) != "" {
		t.Errorf("a lane is as tall as its drawn columns, not its hidden ones:\n%s", strings.Join(lines, "\n"))
	}
}

func TestMovingUpOntoACardShowsItsTitleFirst(t *testing.T) {
	l := newTaskList(defaultKeyMap())
	l.ref = testWorkflows()
	var tasks []store.Task
	for _, title := range []string{"first", "second", "third"} {
		tasks = append(tasks, store.Task{ID: title, Title: title, Status: "Active", CustomStatusID: "S1"})
	}
	l.setRows("F1", "API", tasks, nil, "", false)
	b := boardModel{keys: defaultKeyMap(), width: 60, height: 5}
	b.fit(&l, nil)
	b = boardPress(t, b, &l, "G")
	b = boardPress(t, b, &l, "k")
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	lines := strings.Split(b.View(th, l.ref, time.Time{}, &l, true), "\n")
	if !strings.Contains(lines[1], "> second") {
		t.Errorf("k onto a card scrolls to its title, not to the blank line above it:\n%s", strings.Join(lines, "\n"))
	}
}

func TestReferenceReloadKeepsTheSelectionWhileGrouped(t *testing.T) {
	m := Model{keys: defaultKeyMap()}
	m.list = testBoardList()
	m.list.selectByID("b1")
	got, _ := m.Update(refLoadedMsg{ref: m.list.ref})
	after := got.(Model)
	if id := selected(&after.list); id != "b1" {
		t.Errorf("a reference reload regrouped the rows and moved the selection to %s", id)
	}
	got, _ = after.Update(treeLoadedMsg{nodes: []treeNode{{id: "me", title: "My tasks", kind: nodeMe}}})
	after = got.(Model)
	if id := selected(&after.list); id != "b1" {
		t.Errorf("a tree reload moved the selection to %s", id)
	}
}

func TestFilteringRefitsTheBoard(t *testing.T) {
	m := Model{keys: defaultKeyMap(), shape: shapeBoard, focus: paneBoard, width: 120, height: 30}
	m.board.keys = m.keys
	m.list = testBoardList()
	m.syncPaneSizes()
	m.list.selectByID("b2")
	m.board.fit(&m.list, nil)
	var model tea.Model = m
	for _, k := range []string{"/", "h", "o", "l", "d"} {
		model, _ = model.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	got := model.(Model)
	if id := selected(&got.list); id != "b1" {
		t.Fatalf("the filter hold leaves Bartek on hold, selected %s", id)
	}
	if got.board.last != (boardCell{lane: 0, col: 2}) {
		t.Errorf("the board was not refit while typing the filter, last cell %+v", got.board.last)
	}
}

func TestMoveStatusJudgesTheTaskItIsGiven(t *testing.T) {
	m := Model{keys: defaultKeyMap(), shape: shapeBoard}
	m.list = testBoardList()
	m.ref = m.list.ref
	byID := func(id string) store.Task {
		m.list.selectByID(id)
		cur, _ := m.list.current()
		return cur.task
	}
	b2, a1 := byID("b2"), byID("a1")
	// The cursor sits on a1 while the detail pane holds b2, the way a task opened from search can differ from the list.
	m.list.selectByID("a1")
	if cmd := m.moveStatus(b2, +1); cmd == nil || !strings.Contains(m.status.toast, "another workflow") {
		t.Errorf("b2 is in a bucket whatever the cursor is on, toast %q", m.status.toast)
	}
	m.status.toast = ""
	m.list.selectByID("b2")
	cmd := m.moveStatus(a1, +1)
	if cmd == nil {
		t.Fatal("a1 moves whatever the cursor is on")
	}
	if _, ok := cmd().(submitStatusMsg); !ok {
		t.Errorf("a1 should be stepped, got %#v", cmd())
	}
	last := store.Task{ID: "done", CustomStatusID: "S5"}
	if cmd := m.moveStatus(last, +1); cmd == nil || !strings.Contains(m.status.toast, "last status") {
		t.Errorf("the end of the workflow says so, toast %q", m.status.toast)
	}
}

func TestFitColumnsNeverDrawsPastTheWidthAndKeepsAHeaderWhileTheBoardFits(t *testing.T) {
	cols := []boardColumn{{title: "New", rows: []int{0}}, {title: "Doing", rows: []int{1}}}
	w := fitColumns(cols, 25, 0, 0)
	if w.widths[0] > 25-4-5 {
		t.Errorf("at 25 columns the one drawn column must fit next to the markers, width %d", w.widths[0])
	}
	wide := []boardColumn{{title: "Waiting for an external review", rows: []int{0}}, {title: "New", rows: []int{1}}}
	w = fitColumns(wide, 120, 0, 0)
	if want := len("Waiting for an external review (1)"); w.widths[0] < want {
		t.Errorf("spare width only ever grows a column, the long header came out at %d, want at least %d", w.widths[0], want)
	}
}

func TestHighImportanceIsFlaggedInRowsAndOnCards(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	l := newTaskList(defaultKeyMap())
	l.ref = testWorkflows()
	tasks := []store.Task{
		{ID: "h", Title: "Hot", Status: "Active", CustomStatusID: "S1", Importance: "High"},
		{ID: "n", Title: "Calm", Status: "Active", CustomStatusID: "S1"},
	}
	l.setRows("F1", "API", tasks, nil, "", false)
	view := l.View(th, l.ref, time.Time{}, 60, 4, true)
	if !strings.Contains(view, "o ! Hot") || !strings.Contains(view, "o   Calm") {
		t.Errorf("list rows should flag High in a fixed column:\n%s", view)
	}
	for i, want := range []bool{true, false} {
		lines := cardLines(th, l.ref, time.Time{}, taskRow{task: tasks[i]}, 30, false, false, nil)
		if got := strings.Contains(lines[len(lines)-1], "!"); got != want {
			t.Errorf("card meta line for %s = %q, flagged %v, want %v", tasks[i].ID, lines[len(lines)-1], got, want)
		}
	}
}

// tenColumns is a workflow of ten statuses, seven with cards and three nobody uses.
// The natural widths are seven cards of 21, headers of 13, 12 and 12, and nine gaps: 202 in all.
func tenColumns() []boardColumn {
	var cols []boardColumn
	for i, name := range []string{"New", "Planned", "Doing", "Review", "QA", "Staging", "Done"} {
		cols = append(cols, boardColumn{title: name, rows: []int{i}})
	}
	for _, name := range []string{"Cancelled", "Deferred", "Rejected"} {
		cols = append(cols, boardColumn{title: name})
	}
	return cols
}

func TestFitColumnsShrinksEvenlyToTheFloorsBeforeItSlides(t *testing.T) {
	cols := tenColumns()
	w := fitColumns(cols, 200, 0, 0)
	if w.last != 9 || w.left != 0 || w.right != 0 {
		t.Fatalf("two cells short at 200, the columns give them up and all ten are drawn: %+v", w)
	}
	for i, c := range cols {
		lo, hi := cardWidthMin, cardWidth
		if len(c.rows) == 0 {
			lo, hi = emptyWidthMin, len(columnHeader(c))
		}
		if w.widths[i] < lo || w.widths[i] > hi {
			t.Errorf("column %d came out at %d, want between %d and %d", i, w.widths[i], lo, hi)
		}
	}
	w = fitColumns(cols, 160, 0, 0)
	if w.last != 9 || w.left != 0 || w.right != 0 {
		t.Fatalf("at 160 the floors fit exactly and all ten are drawn: %+v", w)
	}
	for i, c := range cols {
		want := cardWidthMin
		if len(c.rows) == 0 {
			want = emptyWidthMin
		}
		if w.widths[i] != want {
			t.Errorf("at 160 column %d is %d wide, want %d", i, w.widths[i], want)
		}
	}
	w = fitColumns(cols, 120, 0, 0)
	if w.first != 0 || w.last != 5 || w.right != 4 {
		t.Fatalf("at 120 the window is cut at the floors, six columns of 16 next to the markers: %+v", w)
	}
	for i := w.first; i <= w.last; i++ {
		if w.widths[i] < cardWidthMin || w.widths[i] > cardWidth {
			t.Errorf("at 120 drawn column %d is %d wide, want between %d and %d", i, w.widths[i], cardWidthMin, cardWidth)
		}
	}
}

func TestFitColumnsPullsTheWindowBackFromTheLastColumn(t *testing.T) {
	// A fit before the terminal size arrives leaves the window on the cursor column alone.
	cols := tenColumns()
	w := fitColumns(cols, 120, 9, 9)
	if w.first != 3 || w.last != 9 || w.left != 3 || w.right != 0 {
		t.Errorf("a window at the last column starts as far left as the floors fit, seven columns: %+v", w)
	}
}

func TestColumnHeaderKeepsTheCountWhenCut(t *testing.T) {
	empty := boardColumn{title: "Cancelled"}
	if got := fitHeader(empty, 13); got != "Cancelled (0)" {
		t.Errorf("a header that fits is whole, got %q", got)
	}
	if got := fitHeader(empty, 10); got != "Canc.. (0)" {
		t.Errorf("a cut header keeps the count, got %q", got)
	}
	busy := boardColumn{title: "In Progress", rows: make([]int, 10)}
	if got := fitHeader(busy, 12); got != "In Pr.. (10)" {
		t.Errorf("a card column cut below its header keeps the count too, got %q", got)
	}
}

func TestBoardViewSeparatesTheCardsOfAColumn(t *testing.T) {
	l := testBoardList()
	b := boardModel{keys: defaultKeyMap(), width: 100, height: 14}
	b.fit(&l, nil)
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	lines := strings.Split(b.View(th, l.ref, time.Time{}, &l, true), "\n")
	// Lane 0: the divider, then Ada's two New cards of two lines each with a blank line between them,
	// and nothing after the second one before the next lane's divider.
	if !strings.Contains(lines[2], "> Ada new") || strings.TrimSpace(lines[4]) != "" || !strings.Contains(lines[5], "Ada also new") {
		t.Errorf("a blank line separates two cards in a column:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[7], "-- Bartek Lis") {
		t.Errorf("the last card of a lane is followed by the next divider, not a gap:\n%s", strings.Join(lines, "\n"))
	}
}

func TestBoardFitCountsTheGapsBetweenCards(t *testing.T) {
	l := newTaskList(defaultKeyMap())
	l.ref = testWorkflows()
	var tasks []store.Task
	for _, title := range []string{"first", "second", "third", "fourth", "fifth"} {
		tasks = append(tasks, store.Task{ID: title, Title: title, Status: "Active", CustomStatusID: "S1"})
	}
	l.setRows("F1", "API", tasks, nil, "", false)
	b := boardModel{keys: defaultKeyMap(), width: 60, height: 7}
	b.fit(&l, nil)
	b = boardPress(t, b, &l, "G")
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true})
	if view := b.View(th, l.ref, time.Time{}, &l, true); !strings.Contains(view, "> fifth") {
		t.Errorf("the fit scrolls past the gaps so the last card is drawn:\n%s", view)
	}
}
