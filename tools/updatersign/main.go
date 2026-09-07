// Command updatersign manages the Ed25519 signing key used to authenticate
// auto-updates, and signs release artifacts in CI.
//
// Generate a keypair (run once, locally):
//
//	go run ./tools/updatersign keygen
//
// Paste the printed public key into pkg/updater/verify.go (updaterPublicKey) and
// store the private key in the GitHub Actions secret UPDATER_PRIVATE_KEY.
//
// Sign a file (done automatically by .github/workflows/release.yml):
//
//	UPDATER_PRIVATE_KEY=<hex> go run ./tools/updatersign sign path/to/file
//
// which writes path/to/file.sig (base64-encoded Ed25519 signature). With no
// key set, signing is skipped so unsigned builds keep working.
//
// Ask which public key the configured secret corresponds to:
//
//	UPDATER_PRIVATE_KEY=<hex> go run ./tools/updatersign pubkey
//
// An Ed25519 private key CONTAINS its public half, so this needs no network and
// no other input. It exists because a GitHub Actions secret is write-only once
// stored — you cannot read it back to check it is the key the shipped binaries
// trust. This answers that question without ever printing the private half.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "keygen":
		keygen()
	case "sign":
		if len(os.Args) < 3 {
			usage()
		}
		sign(os.Args[2])
	case "pubkey":
		pubkey()
	default:
		usage()
	}
}

func keygen() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal(err)
	}
	fmt.Println("Public key — paste into pkg/updater/verify.go (updaterPublicKey):")
	fmt.Println("  " + base64.StdEncoding.EncodeToString(pub))
	fmt.Println()
	fmt.Println("Private key — add as GitHub secret UPDATER_PRIVATE_KEY (keep secret!):")
	fmt.Println("  " + hex.EncodeToString(priv))
}

// pubkey prints the public half of the configured private key.
//
// Compare it with updaterPublicKey in pkg/updater/verify.go: if they differ,
// the secret is not the key installed copies trust, and every signature it
// makes will be refused by the updater it is meant to satisfy.
func pubkey() {
	key := loadKey()
	fmt.Println(base64.StdEncoding.EncodeToString([]byte(ed25519.PrivateKey(key).Public().(ed25519.PublicKey))))
}

// loadKey reads and validates UPDATER_PRIVATE_KEY, or exits.
//
// The error says the LENGTH it found and never the value: this runs in CI, and
// a private key in a log line is the failure this whole mechanism exists to
// prevent.
func loadKey() []byte {
	keyHex := strings.TrimSpace(os.Getenv("UPDATER_PRIVATE_KEY"))
	if keyHex == "" {
		fatal(fmt.Errorf("UPDATER_PRIVATE_KEY is not set"))
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		fatal(fmt.Errorf("UPDATER_PRIVATE_KEY is not hexadecimal (%d characters)", len(keyHex)))
	}
	if len(key) != ed25519.PrivateKeySize {
		fatal(fmt.Errorf("UPDATER_PRIVATE_KEY decodes to %d bytes, want %d", len(key), ed25519.PrivateKeySize))
	}
	return key
}

func sign(path string) {
	keyHex := strings.TrimSpace(os.Getenv("UPDATER_PRIVATE_KEY"))
	if keyHex == "" {
		fmt.Fprintln(os.Stderr, "updatersign: UPDATER_PRIVATE_KEY not set — skipping signature")
		return
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		fatal(fmt.Errorf("UPDATER_PRIVATE_KEY is not a valid Ed25519 private key"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(key), data)
	out := path + ".sig"
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("updatersign: wrote %s\n", out)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: updatersign keygen | sign <file> | pubkey")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "updatersign:", err)
	os.Exit(1)
}
