// Package secrets stores the credentials outbound sinks need (SMTP passwords,
// webhook tokens) somewhere a windowless dispatcher can still read them.
//
// It exists because the two obvious places are both wrong here. The renderer's
// localStorage encryption keeps its AES key as an extractable plaintext JWK
// beside the ciphertext it protects, and nothing in the webview is reachable
// from a background goroutine anyway. Putting the password in the sink config
// row means plaintext in monitoring.db.
//
// What this defends against: another user on the machine reading the file, and
// a copied or backed-up monitoring.db leaking credentials. What it does NOT
// defend against: another process running as the same user — on a desktop OS
// that process can ask the same keychain or call the same DPAPI unseal. No
// desktop credential store solves that, and pretending otherwise would be worse
// than saying it plainly.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// MaxSecretLen is a portable ceiling enforced in Go, not only in the UI.
const MaxSecretLen = 2048

var (
	// ErrNotFound means no secret is stored under that reference.
	ErrNotFound = errors.New("secrets: not found")
	// ErrTooLong means the secret exceeds MaxSecretLen.
	ErrTooLong = errors.New("secrets: secret is too long")
)

// Store holds credentials by opaque reference. Only references ever cross the
// Wails bridge or land in monitoring.db.
type Store interface {
	Get(ref string) (string, error)
	Set(ref, secret string) error
	Delete(ref string) error
	// Backend names the protection actually in use, so the settings UI can say
	// what is true on THIS machine rather than what was hoped for.
	Backend() string
}

// SinkRef is the reference shape for a notification sink's credential.
func SinkRef(sinkID string) string { return "sink/" + sinkID + "/secret" }

// SessionRef is the reference shape for a monitoring session's SNMP
// credentials. They are stored as one JSON blob rather than three entries
// because they are always read together, and because a partial read would
// produce a confusing "auth works, privacy does not" failure.
func SessionRef(sessionID string) string { return "session/" + sessionID + "/snmp" }

// keyProtector guards the data-encryption key. Each OS gets the strongest
// mechanism available without a new dependency.
type keyProtector interface {
	loadKey() ([]byte, error)
	saveKey(key []byte) error
	name() string
}

// fileStore keeps an AES-256-GCM sealed map on disk. The key itself is
// protected by the platform's keyProtector.
type fileStore struct {
	mu        sync.Mutex
	path      string
	protector keyProtector
	key       []byte
}

// Open selects the best available backend for dir (the app's config directory).
func Open(dir string) (Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create %s: %w", dir, err)
	}
	s := &fileStore{
		path:      filepath.Join(dir, "sinks.secrets"),
		protector: newProtector(dir),
	}
	key, err := s.protector.loadKey()
	if err != nil {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("secrets: generate key: %w", err)
		}
		if err := s.protector.saveKey(key); err != nil {
			return nil, fmt.Errorf("secrets: persist key: %w", err)
		}
	}
	s.key = key
	return s, nil
}

// Backend reports the key protection in effect.
func (s *fileStore) Backend() string { return s.protector.name() }

func (s *fileStore) load() (map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("secrets: store is truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// A wrong key or a tampered file. Refuse rather than silently starting
		// empty, which would look like "my credentials vanished".
		return nil, fmt.Errorf("secrets: cannot decrypt the store: %w", err)
	}

	out := map[string]string{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *fileStore) save(values map[string]string) error {
	plain, err := json.Marshal(values)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)

	// Write to a temporary file and rename: a crash mid-write must not leave a
	// half-written store that no longer decrypts.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get returns the secret stored under ref.
func (s *fileStore) Get(ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load()
	if err != nil {
		return "", err
	}
	v, ok := values[ref]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores a secret. An empty secret deletes it, so clearing a password in
// the UI actually removes it rather than storing "".
func (s *fileStore) Set(ref, secret string) error {
	if len(secret) > MaxSecretLen {
		return ErrTooLong
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load()
	if err != nil {
		return err
	}
	if secret == "" {
		delete(values, ref)
	} else {
		values[ref] = secret
	}
	return s.save(values)
}

// Delete removes a secret.
func (s *fileStore) Delete(ref string) error {
	return s.Set(ref, "")
}
