package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDirectorySuggestions(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"Project One/nested", "Project Two", ".hidden", "skip\nline"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "Project file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Project Two", filepath.Join(home, "Project alias")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{"", []string{"~/", "Project One", "Project Two", "Project alias"}},
		{"~/proj", []string{"~/Project One", "~/Project Two", "~/Project alias"}},
		{"~/Project One/", []string{"~/Project One/", "~/Project One/nested"}},
		{"~/Project One", []string{"~/Project One"}},
		{"~/.h", []string{"~/.hidden"}},
		{home + "/Project One/n", []string{home + "/Project One/nested"}},
		{"no such directory/", nil},
		{"Project file", nil},
		{"skip\n", nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := directorySuggestions(home, tc.input); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
