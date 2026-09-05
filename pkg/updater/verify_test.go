package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// The two questions the manifest has to answer, and they are different ones.
//
// The signature answers "did we sign this?". It cannot answer "is this the
// manifest for the release being installed?", because a manifest from an
// EARLIER release is signed just as validly — so anyone able to serve what the
// app fetches can hand back an old manifest and its old binaries, and every
// check here passes while an older, possibly vulnerable, version is installed.
// The version line is the only thing in the chain that refuses that.

func signedManifest(t *testing.T, body string) (pub string, manifest, sig []byte) {
	t.Helper()
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	manifest = []byte(body)
	raw := ed25519.Sign(privKey, manifest)
	return base64.StdEncoding.EncodeToString(pubKey),
		manifest,
		[]byte(base64.StdEncoding.EncodeToString(raw))
}

// withKey swaps the embedded public key for the duration of a test.
func withKey(t *testing.T, key string) {
	t.Helper()
	previous := updaterPublicKey
	updaterPublicKey = key
	t.Cleanup(func() { updaterPublicKey = previous })
}

const manifestBody = "version v1.5.0\n" +
	"aaaa  SnmpLens-linux-amd64\n" +
	"bbbb  SnmpLens-windows-amd64.exe\n"

func TestSignatureIsCheckedAgainstTheEmbeddedKey(t *testing.T) {
	pub, manifest, sig := signedManifest(t, manifestBody)
	withKey(t, pub)

	if !signatureEnforced() {
		t.Fatal("a configured key must make the signature mandatory")
	}
	if err := verifyManifestSignature(manifest, sig); err != nil {
		t.Errorf("a manifest signed with the matching key must verify: %v", err)
	}

	// One byte of the manifest, changed after signing.
	tampered := append([]byte{}, manifest...)
	tampered[0] ^= 0x01
	if err := verifyManifestSignature(tampered, sig); err == nil {
		t.Error("a modified manifest verified against its old signature")
	}

	// The right shape of signature, from the wrong key.
	otherPub, _, otherSig := signedManifest(t, manifestBody)
	if otherPub == pub {
		t.Fatal("two generated keys collided")
	}
	if err := verifyManifestSignature(manifest, otherSig); err == nil {
		t.Error("a signature from another key was accepted")
	}
}

func TestManifestIsBoundToItsRelease(t *testing.T) {
	if err := checkManifestVersion([]byte(manifestBody), "v1.5.0"); err != nil {
		t.Errorf("the manifest for the release being installed must be accepted: %v", err)
	}

	// THE REPLAY. A previous release's manifest, correctly signed, served in
	// place of the current one.
	err := checkManifestVersion([]byte(manifestBody), "v1.6.0")
	if err == nil {
		t.Fatal("a manifest for v1.5.0 was accepted while installing v1.6.0")
	}
	if !strings.Contains(err.Error(), "replayed") {
		t.Errorf("the error should say what happened, got: %v", err)
	}

	// A manifest from before the line existed is refused rather than tolerated:
	// tolerating it IS the replay, since that is exactly what an attacker
	// serving an old manifest would present.
	noLine := "aaaa  SnmpLens-linux-amd64\n"
	if err := checkManifestVersion([]byte(noLine), "v1.6.0"); err == nil {
		t.Error("a manifest with no version line was accepted")
	}

	// Whitespace and a trailing newline must not decide the outcome.
	if err := checkManifestVersion([]byte("  version v1.5.0  \r\naaaa  x\n"), "v1.5.0"); err != nil {
		t.Errorf("surrounding whitespace should not matter: %v", err)
	}
}

func TestParseChecksumFindsTheAssetAndNothingElse(t *testing.T) {
	got, err := parseChecksum([]byte(manifestBody), "SnmpLens-linux-amd64")
	if err != nil || got != "aaaa" {
		t.Errorf("expected aaaa for the linux asset, got %q (%v)", got, err)
	}

	// The version line has two fields and so reaches the same parser as a
	// checksum entry. It must never be mistaken for one.
	if _, err := parseChecksum([]byte(manifestBody), "v1.5.0"); err == nil {
		t.Error("the version line was returned as if it were a checksum entry")
	}

	if _, err := parseChecksum([]byte(manifestBody), "SnmpLens-macos-universal.dmg"); err == nil {
		t.Error("an asset with no entry must be an error, not an empty checksum")
	}
}
