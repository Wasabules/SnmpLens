package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"SnmpLens/pkg/secrets"
)

// Custody of the key the renderer's settings are sealed with.
//
// crypto.js used to generate an AES-256-GCM key, export it as an EXTRACTABLE
// JWK, and write it to the SAME localStorage as the ciphertext it protects.
// Anyone who could read the WebView2 Local Storage folder had both, so the
// encryption was decorative — it kept a credential out of a casual glance at
// the file and out of nothing else.
//
// The key moves here. The credentials do NOT: they stay in localStorage,
// sealed, and the renderer still holds their plaintext in memory while the app
// runs. That is a deliberate, stated limit — see SettingsKeyStatus and the
// note in CLAUDE.md — and it is what makes this a small, reversible change
// rather than a rewrite of every request path.
//
// What it buys depends on the platform, and the UI says so rather than
// claiming "OS-protected" everywhere:
//
//	Windows  DPAPI seals the key to the user account. A copied profile is
//	         useless to another account and to another machine.
//	macOS    The Keychain holds it, subject to its own access control.
//	Linux    A 0600 file in a 0700 directory beside monitoring.db. That keeps
//	         it away from other accounts and out of a copied WebView2 profile,
//	         and that is all — another process running as the same user can
//	         still read it. Better than before; not a different category.

// SettingsKeyState is what the renderer needs to tell "no store here" from
// "store present, key unreadable" — two situations with opposite correct
// responses.
type SettingsKeyState struct {
	// Backend names the protection in effect, e.g. "windows-dpapi".
	Backend string `json:"backend"`
	// Available is false when there is no secret store at all on this machine.
	Available bool `json:"available"`
	// HasKey is true once a key exists and can be read.
	HasKey bool `json:"hasKey"`
	// Error is why the key could not be read, when it could not.
	Error string `json:"error,omitempty"`
}

const settingsPrefix = "enc:"

// errNoSecretStore is returned when there is nowhere to keep the key.
var errNoSecretStore = errors.New("no secret store is available on this machine")

// settingsKey returns the sealing key, creating one on first use.
//
// Cached: every secrets.Get is a whole-file read and decrypt, and a settings
// save seals one value per sensitive field plus three per target override.
//
// It deliberately does NOT collapse an error to "no key". sinkSecret and
// loadSessionCreds do that, and it is survivable there — a sink fails to send.
// Here it would mean re-sealing the user's credentials under a fresh key while
// the real one sits unreadable, which loses the old ones for good.
func (a *App) settingsKey() ([]byte, error) {
	if a.secrets == nil {
		return nil, errNoSecretStore
	}
	if len(a.settingsKeyCache) == 32 {
		return a.settingsKeyCache, nil
	}

	stored, err := a.secrets.Get(secrets.SettingsKeyRef())
	switch {
	case err == nil:
		key, decErr := base64.StdEncoding.DecodeString(stored)
		if decErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("the stored settings key is unusable (%d bytes)", len(key))
		}
		a.settingsKeyCache = key
		return key, nil

	case errors.Is(err, secrets.ErrNotFound):
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate settings key: %w", err)
		}
		if err := a.secrets.Set(secrets.SettingsKeyRef(), base64.StdEncoding.EncodeToString(key)); err != nil {
			return nil, fmt.Errorf("store settings key: %w", err)
		}
		a.settingsKeyCache = key
		return key, nil
	}
	return nil, fmt.Errorf("read settings key: %w", err)
}

// SettingsKeyStatus reports what custody is available, without creating a key.
func (a *App) SettingsKeyStatus() SettingsKeyState {
	if a.secrets == nil {
		return SettingsKeyState{Backend: "unavailable", Available: false}
	}
	state := SettingsKeyState{Backend: a.secrets.Backend(), Available: true}

	_, err := a.secrets.Get(secrets.SettingsKeyRef())
	switch {
	case err == nil:
		state.HasKey = true
	case errors.Is(err, secrets.ErrNotFound):
		// No key yet. Not an error: the first save makes one.
	default:
		state.Error = err.Error()
	}
	return state
}

// SettingsSeal encrypts a batch of values, in order.
//
// Batched because a save seals every sensitive field at once, and one bridge
// round trip per field would make saving settings visibly slow. An empty value
// stays empty — an empty community is "not set", and sealing it would make it
// indistinguishable from one that is.
func (a *App) SettingsSeal(values []string) ([]string, error) {
	key, err := a.settingsKey()
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	out := make([]string, len(values))
	for i, value := range values {
		if value == "" || strings.HasPrefix(value, settingsPrefix) {
			// Already sealed, or nothing to seal. Re-sealing an already sealed
			// value would double-encrypt it and lose the plaintext.
			out[i] = value
			continue
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, fmt.Errorf("nonce: %w", err)
		}
		sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
		out[i] = settingsPrefix + base64.StdEncoding.EncodeToString(sealed)
	}
	return out, nil
}

// SettingsOpen decrypts a batch of values, in order.
//
// The whole batch fails together. A partial answer would leave the renderer
// deciding which half of someone's credentials to trust, and the safe
// response to any failure here is the same one: keep the stored ciphertext,
// change nothing, and say so.
func (a *App) SettingsOpen(values []string) ([]string, error) {
	key, err := a.settingsKey()
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	out := make([]string, len(values))
	for i, value := range values {
		if !strings.HasPrefix(value, settingsPrefix) {
			out[i] = value
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(value[len(settingsPrefix):])
		if err != nil {
			return nil, fmt.Errorf("value %d is not valid base64", i)
		}
		if len(raw) < gcm.NonceSize() {
			return nil, fmt.Errorf("value %d is truncated", i)
		}
		nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
		plain, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return nil, fmt.Errorf("value %d cannot be decrypted with the stored key", i)
		}
		out[i] = string(plain)
	}
	return out, nil
}

// SettingsAdoptKey takes custody of the key crypto.js used to keep in
// localStorage, given the `k` member of its exported JWK.
//
// This is the whole migration. The format on both sides is a 12-byte nonce
// followed by the GCM output, so once the key is here every existing sealed
// value opens unchanged and nothing has to be re-encrypted — which means there
// is no window in which a user's credentials exist in only one place.
//
// It REFUSES to overwrite an existing key. Adopting twice would replace the
// key that opens the stored blob with one that does not, and the failure would
// look exactly like a corrupt store.
func (a *App) SettingsAdoptKey(jwkK string) error {
	if a.secrets == nil {
		return errNoSecretStore
	}

	// JWK "k" is base64url without padding (RFC 7517).
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(jwkK, "="))
	if err != nil {
		return fmt.Errorf("the legacy key is not valid base64url: %w", err)
	}
	if len(key) != 32 {
		return fmt.Errorf("the legacy key is %d bytes, expected 32", len(key))
	}

	if existing, err := a.secrets.Get(secrets.SettingsKeyRef()); err == nil {
		// Idempotent when it is the same key: a migration interrupted after
		// Set but before the browser removed its copy must be able to finish.
		if existing == base64.StdEncoding.EncodeToString(key) {
			a.settingsKeyCache = key
			return nil
		}
		return errors.New("a settings key is already stored; refusing to replace it")
	} else if !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("read settings key: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	if err := a.secrets.Set(secrets.SettingsKeyRef(), encoded); err != nil {
		return fmt.Errorf("store settings key: %w", err)
	}

	// Read it back before reporting success. The renderer deletes its own copy
	// on the strength of this, and a store that accepted a write it cannot
	// return would otherwise take the only key with it.
	back, err := a.secrets.Get(secrets.SettingsKeyRef())
	if err != nil {
		return fmt.Errorf("the key was stored but cannot be read back: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(back), []byte(encoded)) != 1 {
		return errors.New("the key read back does not match the one stored")
	}

	a.settingsKeyCache = key
	return nil
}

// SettingsForgetKey deletes the key, which makes every sealed value
// unreadable. Used by "reset settings", which also clears the values.
func (a *App) SettingsForgetKey() error {
	a.settingsKeyCache = nil
	if a.secrets == nil {
		return nil
	}
	return a.secrets.Delete(secrets.SettingsKeyRef())
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
