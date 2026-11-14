package data

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DownloadStats summarizes one idempotent snapshot download.
type DownloadStats struct {
	Archives int
	Workers  int
}

// Downloader fetches and verifies fixed monthly archives.
type Downloader struct {
	Client  *http.Client
	Workers int
}

// NewDownloader returns a downloader with bounded concurrency and timeouts.
func NewDownloader(workers int) Downloader {
	if workers < 1 {
		workers = 1
	}
	return Downloader{
		Client:  &http.Client{Timeout: 90 * time.Second},
		Workers: workers,
	}
}

// Download retrieves each checksum and archive, verifies SHA-256, and extracts its CSV.
func (d Downloader) Download(ctx context.Context, root string, config Config) (DownloadStats, error) {
	sources, err := config.Sources()
	if err != nil {
		return DownloadStats{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Source)
	var wait sync.WaitGroup
	var firstErr error
	var errorLock sync.Mutex

	worker := func() {
		defer wait.Done()
		for source := range jobs {
			if ctx.Err() != nil {
				return
			}
			if err := d.downloadSource(ctx, root, source); err != nil {
				errorLock.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				errorLock.Unlock()
				return
			}
		}
	}

	workers := d.Workers
	if workers > len(sources) {
		workers = len(sources)
	}
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go worker()
	}
	for _, source := range sources {
		select {
		case jobs <- source:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wait.Wait()
	if firstErr != nil {
		return DownloadStats{}, firstErr
	}
	return DownloadStats{Archives: len(sources), Workers: workers}, nil
}

func (d Downloader) downloadSource(ctx context.Context, root string, source Source) error {
	checksumPath := filepath.Join(root, filepath.FromSlash(source.ChecksumRelativePath))
	archivePath := filepath.Join(root, filepath.FromSlash(source.ArchiveRelativePath))
	csvPath := filepath.Join(root, filepath.FromSlash(source.CSVRelativePath))
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return fmt.Errorf("create raw data directory for %s: %w", source.ArchiveName, err)
	}

	if _, err := os.Stat(checksumPath); os.IsNotExist(err) {
		if _, err := d.downloadFile(ctx, source.ChecksumURL, checksumPath); err != nil {
			return fmt.Errorf("download %s: %w", filepath.Base(checksumPath), err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", source.ChecksumRelativePath, err)
	}
	expected, err := readOfficialChecksum(checksumPath, source.ArchiveName)
	if err != nil {
		return fmt.Errorf("parse %s: %w", source.ChecksumRelativePath, err)
	}

	actual, err := fileSHA256(archivePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hash %s: %w", source.ArchiveRelativePath, err)
	}
	if actual != expected {
		actual, err = d.downloadFile(ctx, source.ArchiveURL, archivePath)
		if err != nil {
			return fmt.Errorf("download %s: %w", source.ArchiveName, err)
		}
	}
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", source.ArchiveName, expected, actual)
	}

	if err := extractExpectedCSV(archivePath, csvPath, source.CSVName); err != nil {
		return fmt.Errorf("extract %s: %w", source.ArchiveName, err)
	}
	return nil
}

func (d Downloader) downloadFile(ctx context.Context, url, destination string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := d.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", response.Status)
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), response.Body); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readOfficialChecksum(path, archiveName string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(content))
	if len(fields) < 1 || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("invalid checksum document")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", fmt.Errorf("invalid SHA-256: %w", err)
	}
	if len(fields) >= 2 && filepath.Base(fields[1]) != archiveName {
		return "", fmt.Errorf("checksum names %q instead of %q", fields[1], archiveName)
	}
	return strings.ToLower(fields[0]), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractExpectedCSV(archivePath, destination, expectedName string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()

	var source *zip.File
	for _, candidate := range archive.File {
		if candidate.Name == expectedName {
			source = candidate
			break
		}
	}
	if source == nil {
		return fmt.Errorf("archive does not contain %s", expectedName)
	}
	reader, err := source.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".extract-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := io.Copy(temporary, reader); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, destination)
}
