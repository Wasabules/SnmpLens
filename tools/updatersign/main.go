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
	fmt.Fprintln(os.Stderr, "usage: updatersign keygen | sign <file>")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "updatersign:", err)
	os.Exit(1)
}
