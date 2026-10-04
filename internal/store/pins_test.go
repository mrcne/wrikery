package store

import (
	"context"
	"reflect"
	"testing"
)

func TestPinsSetAndList(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	pins := st.Pins()
	for _, id := range []string{"F2", "F1", "F2"} {
		if err := pins.Set(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	got, err := pins.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F1", "F2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pinned = %v, want %v", got, want)
	}
	if err := pins.Set(ctx, "F1", false); err != nil {
		t.Fatal(err)
	}
	// Unpinning something that was never pinned is not an error, the pin may have gone with a reload in between.
	if err := pins.Set(ctx, "F9", false); err != nil {
		t.Fatal(err)
	}
	got, err = pins.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"F2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pinned after unpin = %v, want %v", got, want)
	}
}
