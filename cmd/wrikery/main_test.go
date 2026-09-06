package main

import "testing"

func TestVersionLine(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		goVer   string
		want    string
	}{
		{
			name:    "full commit is shortened to seven characters",
			version: "v0.1.0",
			commit:  "abc1234def",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0 (abc1234) go1.26",
		},
		{
			name:    "empty commit drops the parentheses",
			version: "v0.1.0",
			commit:  "",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0 go1.26",
		},
		{
			name:    "a commit shorter than seven characters is printed whole",
			version: "v0.1.0",
			commit:  "abc12",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0 (abc12) go1.26",
		},
		{
			name:    "a git describe version that already holds the short hash drops the parentheses",
			version: "v0.1.0-3-gabc1234-dirty",
			commit:  "abc1234def",
			goVer:   "go1.26",
			want:    "wrikery v0.1.0-3-gabc1234-dirty go1.26",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionLine(tt.version, tt.commit, tt.goVer); got != tt.want {
				t.Errorf("versionLine(%q, %q, %q) = %q, want %q",
					tt.version, tt.commit, tt.goVer, got, tt.want)
			}
		})
	}
}
