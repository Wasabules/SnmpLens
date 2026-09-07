package secrets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The one exec site in this application that puts a credential on a command
// line must not be resolved through $PATH.
//
// `exec.Command("security", …)` searches $PATH, and this call hands the
// data-encryption key to whatever answers to the name. Reaching a GUI
// application's PATH on macOS is not trivial, which is why this is cheap to
// close rather than urgent to close.
//
// Checked over the SOURCE rather than by calling it, so it runs on all three
// platforms CI builds. A darwin-only assertion would be verified on one runner
// and the file is edited from any of them.
func TestTheKeychainToolIsNotResolvedThroughPath(t *testing.T) {
	src, err := os.ReadFile("protect_darwin.go")
	if err != nil {
		t.Fatalf("the macOS protector is gone or renamed; this check is out of date: %v", err)
	}

	// Any exec.Command whose first argument is a bare name — no slash — is the
	// defect. The pattern deliberately does not name `security`: a second tool
	// invoked the same way would have the same problem.
	bare := regexp.MustCompile(`exec\.Command\(\s*"([^"/\\]+)"`)
	if m := bare.FindAllStringSubmatch(string(src), -1); len(m) > 0 {
		var names []string
		for _, hit := range m {
			names = append(names, hit[1])
		}
		t.Errorf("resolved through $PATH: %s", strings.Join(names, ", "))
	}

	// And the constant it does use is absolute.
	if !strings.Contains(string(src), `securityTool = "/usr/bin/security"`) {
		t.Error("securityTool is no longer the absolute path to the bundled tool")
	}

	// The detector: the pattern above must actually match the shape it is
	// looking for, or this test passes on a file it never understood.
	if !bare.MatchString(`exec.Command("security", "find-generic-password")`) {
		t.Fatal("the detector does not match a bare-name exec; it proves nothing")
	}
	if bare.MatchString(`exec.Command("/usr/bin/security", "x")`) {
		t.Fatal("the detector flags an absolute path; it would fail on the fix")
	}
}
