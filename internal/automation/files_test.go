package automation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDirectoryOrdersProgramsAndRejectsInvalidFile(t *testing.T) {
	directory := t.TempDir()
	for name, source := range map[string]string{
		"b.star": `light = device("1", endpoint=1)
button = device("2")
on(short_press(button, endpoint=9), lambda event: [toggle(light)])`,
		"a.star": `light = device("1", endpoint=1)
button = device("2")
on(short_press(button, endpoint=7), lambda event: [power(light, "on")])`,
		"ignored.txt": `bad`,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	programs, err := LoadDirectory(t.Context(), directory)
	if err != nil || len(programs) != 2 || filepath.Base(programs[0].Filename) != "a.star" || filepath.Base(programs[1].Filename) != "b.star" {
		t.Fatalf("programs = %+v, %v", programs, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "c.star"), []byte(`on(1, 2)`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDirectory(t.Context(), directory); err == nil {
		t.Fatal("invalid program was accepted")
	}
	if absent, err := LoadDirectory(context.Background(), filepath.Join(directory, "missing")); err != nil || len(absent) != 0 {
		t.Fatalf("missing directory: %+v, %v", absent, err)
	}
}
