package updater

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadAndApply downloads the pending update's asset, verifies its SHA-256
// against the release checksums file, and applies it. For platforms/install
// methods that cannot be updated in place (macOS, Linux .deb) it simply opens
// the asset URL in the browser.
//
// On a successful self-apply the application relaunches and quits, so callers
// should not expect this method to return in that case.
func (s *Service) DownloadAndApply() error {
	s.mu.Lock()
	p := s.pending
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("no update ready; run a check first")
	}

	ctx := s.context()

	// Fallback: let the OS/browser handle formats we can't self-apply.
	if p.mode == applyBrowser {
		wruntime.BrowserOpenURL(ctx, p.assetURL)
		return nil
	}

	if p.checksumURL == "" {
		return fmt.Errorf("release %s is missing %s; cannot verify the download", p.version, checksumsAsset)
	}

	wantSum, err := s.verifiedChecksum(ctx, p.checksumURL, p.checksumSigURL, p.assetName)
	if err != nil {
		return err
	}

	tmpPath, gotSum, err := s.download(ctx, p.assetURL, p.assetName)
	if tmpPath != "" {
		defer os.Remove(tmpPath)
	}
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}

	if !strings.EqualFold(gotSum, wantSum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", wantSum, gotSum)
	}

	switch p.mode {
	case applyReplace:
		return applyBinary(ctx, tmpPath)
	case applyInstaller:
		return runInstaller(ctx, tmpPath)
	default:
		return fmt.Errorf("unsupported apply mode")
	}
}

// download streams url to a temp file named after asset, emitting progress
// events, and returns the temp path and the file's hex SHA-256.
func (s *Service) download(ctx context.Context, url, asset string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "SnmpLens-updater")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected status %s", resp.Status)
	}

	f, err := os.CreateTemp("", "snmplens-update-*-"+asset)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	h := sha256.New()
	pr := &progressReader{
		ctx:    ctx,
		reader: resp.Body,
		total:  resp.ContentLength,
	}
	if _, err := io.Copy(io.MultiWriter(f, h), pr); err != nil {
		return f.Name(), "", err
	}

	wruntime.EventsEmit(ctx, "update:progress", 100)
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// verifiedChecksum downloads the checksums manifest and, when a public key is
// embedded, its Ed25519 signature; it verifies authenticity and returns the
// SHA-256 recorded for the given asset. This is the trust anchor: once the
// manifest is authenticated, the asset is trusted by its SHA-256.
func (s *Service) verifiedChecksum(ctx context.Context, checksumURL, sigURL, asset string) (string, error) {
	manifest, err := s.fetchBytes(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("fetching checksums: %w", err)
	}

	if signatureEnforced() {
		if sigURL == "" {
			return "", fmt.Errorf("release is missing %s.sig; refusing an unsigned update", checksumsAsset)
		}
		sig, err := s.fetchBytes(ctx, sigURL)
		if err != nil {
			return "", fmt.Errorf("fetching signature: %w", err)
		}
		if err := verifyManifestSignature(manifest, sig); err != nil {
			return "", err
		}
	}

	return parseChecksum(manifest, asset)
}

// fetchBytes downloads a small resource fully into memory (1 MiB cap).
func (s *Service) fetchBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SnmpLens-updater")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// parseChecksum extracts the hex SHA-256 for asset from a `sha256sum`-format
// manifest ("<hex>  <name>", with an optional "*" binary-mode prefix on name).
func parseChecksum(manifest []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(manifest))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) == asset {
			return fields[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum entry for %s", asset)
}

// applyBinary self-replaces the running executable with the downloaded binary,
// then relaunches it and quits. Used for the portable/raw-binary distributions.
func applyBinary(ctx context.Context, binPath string) error {
	f, err := os.Open(binPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := selfupdate.Apply(f, selfupdate.Options{}); err != nil {
		// Best-effort rollback of the partial replacement.
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("update failed and rollback also failed: %w", rerr)
		}
		return fmt.Errorf("applying update: %w", err)
	}

	relaunch(ctx)
	return nil
}

// runInstaller launches the downloaded installer detached, then quits so it can
// replace the running files. Used for the Windows NSIS distribution.
func runInstaller(ctx context.Context, installerPath string) error {
	cmd := exec.Command(installerPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching installer: %w", err)
	}
	// Do not Wait: the installer outlives us.
	wruntime.Quit(ctx)
	return nil
}

// relaunch starts a fresh copy of the (now updated) executable and quits.
func relaunch(ctx context.Context) {
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(exe)
		_ = cmd.Start()
	}
	// Give the new process a moment to spin up before we exit.
	time.Sleep(300 * time.Millisecond)
	wruntime.Quit(ctx)
}

// progressReader wraps a reader and emits throttled "update:progress" events
// (integer percent) as bytes flow through.
type progressReader struct {
	ctx      context.Context
	reader   io.Reader
	total    int64
	read     int64
	lastEmit int
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	p.read += int64(n)
	if p.total > 0 {
		pct := int(p.read * 100 / p.total)
		if pct != p.lastEmit && pct >= 0 && pct <= 100 {
			p.lastEmit = pct
			wruntime.EventsEmit(p.ctx, "update:progress", pct)
		}
	}
	return n, err
}
