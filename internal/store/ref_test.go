package store

import "testing"

func TestParseTaskRef(t *testing.T) {
	for in, want := range map[string]TaskRef{
		"MAAAAAEPXpuV":     {Kind: RefID, Value: "MAAAAAEPXpuV"},
		"IEAG3VSGJMAAAAAA": {Kind: RefID, Value: "IEAG3VSGJMAAAAAA"},
		"ReleaseNotes":     {Kind: RefID, Value: "ReleaseNotes"},
		"4552825749":       {Kind: RefNumber, Value: "4552825749"},
		"https://app-eu.wrike.com/open.htm?id=4552825749":         {Kind: RefNumber, Value: "4552825749"},
		" https://www.wrike.com/open.htm?id=4552825749#comments ": {Kind: RefNumber, Value: "4552825749"},
	} {
		got, ok := ParseTaskRef(in)
		if !ok || got != want {
			t.Errorf("ParseTaskRef(%q) = %+v, %v, want %+v", in, got, ok, want)
		}
	}
	for _, in := range []string{"Architecture", "fix auth", "", "local:3", "https://app-eu.wrike.com/open.htm", "architecture!", "MAAAAAEPXpu"} {
		if got, ok := ParseTaskRef(in); ok {
			t.Errorf("ParseTaskRef(%q) = %+v, want no reference", in, got)
		}
	}
}
