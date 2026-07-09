package updater

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// updaterPublicKey is the base64-encoded Ed25519 public key whose private
// counterpart signs SnmpLens-checksums.txt during the release workflow.
//
// Generate a keypair with `go run ./tools/updatersign keygen`, paste the public
// key here, and store the private key in the GitHub secret UPDATER_PRIVATE_KEY.
//
// While this is empty, downloads are still integrity-checked with SHA-256 but
// NOT authenticated. Set it to enforce that updates were signed by the release
// pipeline. (A var rather than a const so the empty-default branch is not
// flagged as dead code before a key is configured.)
var updaterPublicKey = "svOWbkIuFQTiebt+DKzaohFFdeANV8NjcdX3cQiybPw="

// signatureEnforced reports whether a public key is configured, in which case a
// valid signature on the checksums manifest is mandatory.
func signatureEnforced() bool {
	return updaterPublicKey != ""
}

// verifyManifestSignature checks that sigBase64 is a valid Ed25519 signature of
// manifest under the embedded public key.
func verifyManifestSignature(manifest, sigBase64 []byte) error {
	pub, err := base64.StdEncoding.DecodeString(updaterPublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid embedded updater public key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigBase64)))
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), manifest, sig) {
		return errors.New("checksums signature verification failed")
	}
	return nil
}
