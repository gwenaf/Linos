package network

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// Command runs an OS command and returns its output. Tests replace it: the real one opens a UAC prompt.
var Command = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

var goos = runtime.GOOS

// HotspotPrefix is the subnet Windows Mobile Hotspot gives to its clients.
const HotspotPrefix = "192.168.137."

func powershell(script string) ([]byte, error) {
	return Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
}

// Categories returns the Windows network profile of each connection ("Public", "Private", "DomainAuthenticated").
// A "Public" profile is where the firewall most often blocks phones. Empty outside Windows or on failure.
func Categories() []string {
	if goos != "windows" {
		return nil
	}
	out, err := powershell("(Get-NetConnectionProfile).NetworkCategory")
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// AllowFirewall adds an inbound firewall rule for exe, through a UAC prompt the user must accept on the PC.
func AllowFirewall(exe string) error {
	if goos != "windows" {
		return errors.ErrUnsupported
	}
	// ponytail: each click adds a rule, even if one exists; harmless duplicates.
	rule := `advfirewall firewall add rule name="Linos" dir=in action=allow enable=yes profile=any program="` + exe + `"`
	_, err := powershell("Start-Process netsh -Verb RunAs -Wait -ArgumentList '" + strings.ReplaceAll(rule, "'", "''") + "'")
	return err
}
