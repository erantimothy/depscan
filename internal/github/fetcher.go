// Package github implements domain.RepoFetcher against GitHub's codeload
// tarball endpoint. It's stdlib-only — no git binary, no external
// libraries — which keeps it portable to any environment that can make
// outbound HTTPS calls, including ones (like this sandbox) with no route
// to a package proxy.
package github

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erantimothy/depscan/internal/domain"
)

// maxExtractedBytes bounds total decompressed output. Extracting an
// archive without a size limit is a classic "zip bomb" vector: a
// malicious or corrupt tarball could decompress to gigabytes from a
// tiny download, exhausting disk. Enterprise code handling any
// untrusted archive should always cap this.
const maxExtractedBytes = 500 * 1024 * 1024 // 500 MiB

// Fetcher implements domain.RepoFetcher using codeload.github.com.
type Fetcher struct {
	client *http.Client
}

// New returns a Fetcher with a sensible request timeout. Never use
// http.DefaultClient for outbound calls in production code — it has no
// timeout at all, meaning a hung remote server can block a goroutine
// forever.
func New() *Fetcher {
	return &Fetcher{
		client: &http.Client{Timeout: 2 * time.Minute},
	}
}

// Fetch downloads {owner}/{repo} at ref (defaulting to "HEAD", i.e. the
// default branch) as a tarball, and extracts it into a fresh temp
// directory. GitHub's tarballs wrap everything in a single top-level
// folder like "repo-<sha>/" — we strip that prefix so the returned path
// points directly at the repo root (where go.mod files actually live).
func (f *Fetcher) Fetch(ctx context.Context, source domain.RepoSource) (string, func(), error) {
	if source.Owner == "" || source.Repo == "" {
		return "", nil, fmt.Errorf("github fetch: owner and repo are required")
	}
	ref := source.Ref
	if ref == "" {
		ref = "HEAD"
	}

	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", source.Owner, source.Repo, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("downloading %s/%s@%s: %w", source.Owner, source.Repo, ref, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil, fmt.Errorf("%s/%s@%s: %w", source.Owner, source.Repo, ref, domain.ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("downloading %s/%s@%s: unexpected status %d", source.Owner, source.Repo, ref, resp.StatusCode)
	}

	dest, err := os.MkdirTemp("", "depscan-repo-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dest) }

	if err := extractTarGz(resp.Body, dest); err != nil {
		cleanup() // don't leak the temp dir if extraction fails partway through
		return "", nil, fmt.Errorf("extracting %s/%s@%s: %w", source.Owner, source.Repo, ref, err)
	}

	return dest, cleanup, nil
}

// extractTarGz decompresses and unpacks a .tar.gz stream into dest,
// stripping each entry's top-level path component (GitHub's synthetic
// "repo-sha/" wrapper directory).
func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var totalBytes int64

	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil // reached the end of the archive cleanly
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		relPath := stripTopLevelDir(header.Name)
		if relPath == "" {
			continue // the top-level directory entry itself; nothing to extract
		}

		target := filepath.Join(dest, relPath)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			// Guards against "tar slip" / path traversal: a malicious
			// archive entry named "../../etc/passwd" would otherwise
			// let filepath.Join escape dest entirely. Always verify the
			// resolved path is still inside the intended destination
			// before writing anything from an untrusted archive.
			return fmt.Errorf("tar entry escapes destination: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating dir %s: %w", target, err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent dir for %s: %w", target, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}

			written, err := io.CopyN(out, tr, maxExtractedBytes-totalBytes+1)
			out.Close()
			totalBytes += written
			if totalBytes > maxExtractedBytes {
				return fmt.Errorf("archive exceeds %d byte extraction limit", maxExtractedBytes)
			}
			if err != nil && err != io.EOF {
				return fmt.Errorf("writing %s: %w", target, err)
			}

		default:
			// Skip symlinks, hardlinks, etc. — go.mod scanning only
			// needs regular files and directories.
		}
	}
}

// stripTopLevelDir removes the first path segment ("repo-sha/") from a
// tar entry name, e.g. "agent-manager-abc123/go.mod" -> "go.mod".
func stripTopLevelDir(name string) string {
	name = strings.TrimPrefix(name, "./")
	idx := strings.Index(name, "/")
	if idx == -1 {
		return "" // this is the top-level directory entry itself
	}
	return name[idx+1:]
}
