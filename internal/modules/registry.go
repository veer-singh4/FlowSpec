package modules

import (
	"archive/zip"
	"compress/gzip"
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RegistryClient downloads and caches Terraform modules from the Terraform Registry.
type RegistryClient struct {
	CacheDir    string // e.g. ".flow/modules"
	RegistryURL string // e.g. "https://registry.terraform.io/v1/modules"
	HTTPClient  *http.Client
}

// NewRegistryClient creates a new registry client with defaults.
func NewRegistryClient(cacheDir string) *RegistryClient {
	return &RegistryClient{
		CacheDir:    cacheDir,
		RegistryURL: "https://registry.terraform.io/v1/modules",
		HTTPClient:  http.DefaultClient,
	}
}

//VersionsResponse is the JSON response from the registry versions endpoint.
type VersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

// ModuleVersions lists available versions for a module.
type ModuleVersions struct {
	Versions []VersionEntry `json:"versions"`
}

// VersionEntry is a single available version.
type VersionEntry struct {
	Version string `json:"version"`
}

// Ensure makes sure a module is downloaded and cached locally.
// Returns the local filesystem path to the cached module.
func (r *RegistryClient) Ensure(coords *RegistryCoords, version string) (string, error) {
	if coords == nil {
		return "", fmt.Errorf("nil registry coordinates")
	}

	// If no version specified, fetch latest
	if version == "" {
		latest, err := r.FetchLatestVersion(coords)
		if err != nil {
			return "", fmt.Errorf("failed to fetch latest version for %s: %w", coords.FullSource(), err)
		}
		version = latest
	}

	// Check if already cached
	localPath := r.modulePath(coords, version)
	if info, err := os.Stat(localPath); err == nil && info.IsDir() {
		return localPath, nil
	}

	// Download
	fmt.Printf("  ↓ downloading %s@%s ...\n", coords.FullSource(), version)
	if err := r.download(coords, version, localPath); err != nil {
		return "", err
	}

	fmt.Printf("  ✓ cached at %s\n", localPath)
	return localPath, nil
}

// FetchLatestVersion queries the registry for the latest version of a module.
func (r *RegistryClient) FetchLatestVersion(coords *RegistryCoords) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/%s/versions",
		r.RegistryURL, coords.Namespace, coords.Name, coords.System)

	resp, err := r.HTTPClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned %d for %s", resp.StatusCode, url)
	}

	var vr VersionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return "", fmt.Errorf("failed to decode versions response: %w", err)
	}

	if len(vr.Modules) == 0 || len(vr.Modules[0].Versions) == 0 {
		return "", fmt.Errorf("no versions found for %s", coords.FullSource())
	}

	// The registry returns versions in order; the first one is typically the latest.
	return vr.Modules[0].Versions[0].Version, nil
}

// download fetches a module from the registry and extracts it locally.
func (r *RegistryClient) download(coords *RegistryCoords, version, destPath string) error {
	// Step 1: Get the download URL via the download endpoint
	downloadURL := fmt.Sprintf("%s/%s/%s/%s/%s/download",
		r.RegistryURL, coords.Namespace, coords.Name, coords.System, version)

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return err
	}

	// Don't follow redirects — we need the X-Terraform-Get header
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	sourceURL := resp.Header.Get("X-Terraform-Get")
	if sourceURL == "" {
		// Some responses may redirect directly
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			sourceURL = resp.Header.Get("Location")
		}
	}

	if sourceURL == "" {
		return fmt.Errorf("no download URL found for %s@%s (status=%d)", coords.FullSource(), version, resp.StatusCode)
	}

	// Clean up go-getter syntax: remove "git::" prefix, "?archive=tar.gz" suffix, "//" subdir
	sourceURL = cleanGetterURL(sourceURL)

	// Step 2: Download the archive
	archiveResp, err := r.HTTPClient.Get(sourceURL)
	if err != nil {
		return fmt.Errorf("failed to download archive: %w", err)
	}
	defer archiveResp.Body.Close()

	if archiveResp.StatusCode != http.StatusOK {
		return fmt.Errorf("archive download returned %d", archiveResp.StatusCode)
	}

	// Step 3: Create temp file for archive
	if err := os.MkdirAll(destPath, 0o755); err != nil {
		return fmt.Errorf("failed to create module dir: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "flowmod-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, archiveResp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download archive: %w", err)
	}
	tmpFile.Close()

	// Step 4: Extract
	if err := extractArchive(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to extract module: %w", err)
	}

	return nil
}

// modulePath returns the local cache path for a module + version.
func (r *RegistryClient) modulePath(coords *RegistryCoords, version string) string {
	return filepath.Join(r.CacheDir, coords.Namespace, coords.Name, coords.System, version)
}

// cleanGetterURL normalizes a go-getter style URL to a plain HTTP URL.
func cleanGetterURL(raw string) string {
	// Remove "git::" prefix
	raw = strings.TrimPrefix(raw, "git::")

	// Remove "//*" subdir syntax
	if idx := strings.Index(raw, "//"); idx > 0 {
		raw = raw[:idx]
	}

	// Remove query params like ?archive=tar.gz
	if idx := strings.Index(raw, "?"); idx > 0 {
		raw = raw[:idx]
	}

	return raw
}

// extractArchive tries to extract a .tar.gz or .zip archive into destPath.
func extractArchive(archivePath, destPath string) error {
	// Try tar.gz first
	if err := extractTarGz(archivePath, destPath); err == nil {
		return nil
	}

	// Try zip
	if err := extractZip(archivePath, destPath); err == nil {
		return nil
	}

	return fmt.Errorf("failed to extract archive (tried tar.gz and zip)")
}

func extractTarGz(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	stripPrefix := ""

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// GitHub tarballs have a top-level directory — strip it.
		name := header.Name
		if stripPrefix == "" {
			parts := strings.SplitN(name, "/", 2)
			if len(parts) == 2 {
				stripPrefix = parts[0] + "/"
			}
		}
		name = strings.TrimPrefix(name, stripPrefix)
		if name == "" || name == "." {
			continue
		}

		target := filepath.Join(destPath, filepath.FromSlash(name))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}

	return nil
}

func extractZip(archivePath, destPath string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destPath, filepath.FromSlash(f.Name))

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
