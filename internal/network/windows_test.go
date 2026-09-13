package network

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeCommand records the last command and answers with out and err.
func fakeCommand(t *testing.T, out string, err error) *[]string {
	t.Helper()
	var got []string
	prevCommand, prevGOOS := Command, goos
	t.Cleanup(func() { Command, goos = prevCommand, prevGOOS })
	goos = "windows"
	Command = func(name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte(out), err
	}
	return &got
}

func TestCategories(t *testing.T) {
	fakeCommand(t, "Public\r\nPrivate\r\n", nil)
	if got := Categories(); !reflect.DeepEqual(got, []string{"Public", "Private"}) {
		t.Fatalf("categories = %v", got)
	}

	fakeCommand(t, "", errors.New("powershell missing"))
	if got := Categories(); got != nil {
		t.Fatalf("categories on failure = %v, want none", got)
	}

	goos = "linux"
	if got := Categories(); got != nil {
		t.Fatalf("categories outside Windows = %v, want none", got)
	}
}

func TestAllowFirewall(t *testing.T) {
	cmd := fakeCommand(t, "", nil)
	if err := AllowFirewall(`C:\Jeux\L'inos\linos.exe`); err != nil {
		t.Fatal(err)
	}
	script := (*cmd)[len(*cmd)-1]
	if !strings.Contains(script, "-Verb RunAs") || !strings.Contains(script, `program="C:\Jeux\L''inos\linos.exe"`) {
		t.Fatalf("script = %s, want an elevated netsh with the quote escaped", script)
	}

	fakeCommand(t, "", errors.New("UAC refused"))
	if err := AllowFirewall("linos.exe"); err == nil {
		t.Fatal("a refused UAC prompt must be reported")
	}

	goos = "darwin"
	if err := AllowFirewall("linos"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("outside Windows = %v, want unsupported", err)
	}
}

func TestCommandRunsProgram(t *testing.T) {
	out, err := Command("go", "env", "GOVERSION")
	if err != nil || !strings.HasPrefix(string(out), "go") {
		t.Fatalf("Command = %q, %v", out, err)
	}
}
