package cli

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceUploader handles creating and uploading workspace zips.
type WorkspaceUploader struct {
	baseDir     string
	ignoreRules []string
}

// NewWorkspaceUploader creates a new workspace uploader for the given directory.
func NewWorkspaceUploader(dir string) *WorkspaceUploader {
	w := &WorkspaceUploader{
		baseDir: dir,
		ignoreRules: []string{
			// Default ignores
			".git",
			".git/**",
			"node_modules",
			"node_modules/**",
			"__pycache__",
			"__pycache__/**",
			".venv",
			".venv/**",
			"venv",
			"venv/**",
			".env",
			"*.pyc",
			".DS_Store",
			"*.log",
		},
	}

	// Load .gitignore if exists
	w.loadGitignore()

	return w
}

// loadGitignore reads .gitignore and adds patterns to ignore rules.
func (w *WorkspaceUploader) loadGitignore() {
	gitignorePath := filepath.Join(w.baseDir, ".gitignore")
	f, err := os.Open(gitignorePath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		w.ignoreRules = append(w.ignoreRules, line)
	}
}

// shouldIgnore checks if a path should be ignored.
func (w *WorkspaceUploader) shouldIgnore(relPath string) bool {
	// Always include the root
	if relPath == "." || relPath == "" {
		return false
	}

	baseName := filepath.Base(relPath)

	for _, pattern := range w.ignoreRules {
		// Handle directory patterns (ending with /)
		pattern = strings.TrimSuffix(pattern, "/")

		// Check if pattern matches the base name
		if matched, _ := filepath.Match(pattern, baseName); matched {
			return true
		}

		// Check if pattern matches the full relative path
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return true
		}

		// Handle ** patterns (recursive)
		if strings.Contains(pattern, "**") {
			// Convert ** to match any path
			regexPattern := strings.ReplaceAll(pattern, "**", "*")
			if matched, _ := filepath.Match(regexPattern, relPath); matched {
				return true
			}
		}

		// Check if any parent directory matches
		parts := strings.Split(relPath, string(filepath.Separator))
		for _, part := range parts {
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
		}
	}

	return false
}

// CreateZip creates a zip file of the workspace and returns it as bytes.
func (w *WorkspaceUploader) CreateZip() ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	err := filepath.Walk(w.baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(w.baseDir, path)
		if err != nil {
			return err
		}

		// Skip ignored paths
		if w.shouldIgnore(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Create header
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Copy file contents
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(writer, f)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create zip: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip: %w", err)
	}

	return buf.Bytes(), nil
}

// Upload uploads the workspace zip to the executor.
func (w *WorkspaceUploader) Upload(uploadURL, token, machineID string) error {
	zipData, err := w.CreateZip()
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(zipData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/zip")
	req.Header.Set("fly-force-instance-id", machineID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s - %s", resp.Status, string(body))
	}

	return nil
}

// UploadWorkspace creates and uploads a workspace zip from the current directory.
func UploadWorkspace(uploadURL, token, machineID string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	uploader := NewWorkspaceUploader(cwd)
	return uploader.Upload(uploadURL, token, machineID)
}
