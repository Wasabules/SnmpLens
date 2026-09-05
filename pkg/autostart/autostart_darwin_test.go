package autostart

import "testing"

// What the settings screen shows as "runs at login".
//
// This reads the ProgramArguments array and not simply the first <string> in
// the document, and the difference is the whole test: <key>Label</key> comes
// first in every plist we write, so the naive version returned
// `com.wasabules.snmplens` — the entry's NAME — where the screen promises the
// command. It survived because this file is not compiled on Linux, which is
// where the package's tests used to run.
func TestProgramArgumentIgnoresTheLabel(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.wasabules.snmplens</string>
	<key>ProgramArguments</key>
	<array>
		<string>/Applications/SnmpLens.app/Contents/MacOS/SnmpLens</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>`

	got := programArgument(plist)
	want := "/Applications/SnmpLens.app/Contents/MacOS/SnmpLens"
	if got != want {
		t.Errorf("programArgument = %q, want %q", got, want)
	}
	if got == "com.wasabules.snmplens" {
		t.Error("the label was returned as the command")
	}
}

func TestProgramArgumentRoundTripsAnEscapedPath(t *testing.T) {
	// A path that has to be escaped on the way in. `escapeXML` replaces & first
	// so the entities it writes are not re-escaped; `unescapeXML` has to put
	// &amp; back LAST for the same reason in reverse, or `&amp;lt;` comes back
	// as `<` rather than as `&lt;`.
	path := `/Users/a&b/<Apps>/SnmpLens "beta"`
	plist := "<key>ProgramArguments</key><array><string>" +
		escapeXML(path) + "</string></array>"

	if got := programArgument(plist); got != path {
		t.Errorf("round trip gave %q, want %q", got, path)
	}
}

func TestProgramArgumentOnAPlistThatHasNone(t *testing.T) {
	for name, plist := range map[string]string{
		"empty":            "",
		"no key":           "<dict><key>Label</key><string>x</string></dict>",
		"key but no value": "<key>ProgramArguments</key><array></array>",
		"unterminated":     "<key>ProgramArguments</key><array><string>/bin/x",
	} {
		if got := programArgument(plist); got != "" {
			t.Errorf("%s: expected no command, got %q", name, got)
		}
	}
}
