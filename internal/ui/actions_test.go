package ui

import "testing"

func TestTaskActionTableFollowsTheTaskKeys(t *testing.T) {
	k := defaultKeyMap()
	table := taskActionTable(k)
	keys := k.task()
	if len(table) != len(keys) {
		t.Fatalf("%d actions, want one per task key, %d", len(table), len(keys))
	}
	for i, a := range table {
		want := keys[i].Help()
		if !a.hasKey() || a.key.Help().Key != want.Key || a.label() != want.Desc || a.run == nil {
			t.Errorf("action %d is %q/%q, want %q/%q with a run", i, a.key.Help().Key, a.label(), want.Key, want.Desc)
		}
	}
	if a := (taskAction{name: "budget"}); a.hasKey() || a.label() != "budget" {
		t.Errorf("an action without a key: hasKey %v, label %q", a.hasKey(), a.label())
	}
}
