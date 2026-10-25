package store

import (
	"context"
	"reflect"
	"testing"
)

func TestCustomFieldsReplaceAllAndList(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	fields := []CustomField{{ID: "CF1", Title: "Budget", Type: "Duration"}, {ID: "CF2", Title: "Budget", Type: "Numeric", SpaceID: "SP1"}}
	if err := st.CustomFields().ReplaceAll(ctx, fields); err != nil {
		t.Fatal(err)
	}
	got, err := st.CustomFields().List(ctx)
	if err != nil || !reflect.DeepEqual(got, fields) {
		t.Errorf("list = %+v, %v, want both fields in the API's order", got, err)
	}
	if err := st.CustomFields().ReplaceAll(ctx, fields[:1]); err != nil {
		t.Fatal(err)
	}
	got, err = st.CustomFields().List(ctx)
	if err != nil || !reflect.DeepEqual(got, fields[:1]) {
		t.Errorf("list = %+v, %v, want the one field", got, err)
	}
}
