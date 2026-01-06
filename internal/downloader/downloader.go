package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"multy-loader/internal/config"
)

// Progress represents download progress
type Progress struct {
	FileID      string  `json:"fileId"`
	FileName    string  `json:"fileName"`
	Total       int64   `json:"total"`
	Downloaded  int64   `json:"downloaded"`
	Percent     float64 `json:"percent"`
	Speed       float64 `json:"speed"` // bytes per second
	Status      string  `json:"status"` // "downloading", "completed", "error", "cancelled"
	Error       string  `json:"error,omitempty"`
}

// FileStatus represents the status of a file on disk
type FileStatus struct {
	Exists bool  `json:"exists"`
	Size   int64 `json:"size"`
}

// Downloader handles file downloads
type Downloader struct {
	client     *http.Client
	progress   map[string]*Progress
	cancelFns  map[string]context.CancelFunc
	mu         sync.RWMutex
	listeners  []chan Progress
	listenerMu sync.RWMutex
}

// NewDownloader creates a new downloader
func NewDownloader() *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: 0, // No timeout for large files
		},
		progress:  make(map[string]*Progress),
		cancelFns: make(map[string]context.CancelFunc),
		listeners: make([]chan Progress, 0),
	}
}

// Subscribe to progress updates
func (d *Downloader) Subscribe() chan Progress {
	d.listenerMu.Lock()
	defer d.listenerMu.Unlock()
	ch := make(chan Progress, 100)
	d.listeners = append(d.listeners, ch)
	return ch
}

// Unsubscribe from progress updates
func (d *Downloader) Unsubscribe(ch chan Progress) {
	d.listenerMu.Lock()
	defer d.listenerMu.Unlock()
	for i, listener := range d.listeners {
		if listener == ch {
			d.listeners = append(d.listeners[:i], d.listeners[i+1:]...)
			close(ch)
			break
		}
	}
}

func (d *Downloader) broadcast(p Progress) {
	d.listenerMu.RLock()
	defer d.listenerMu.RUnlock()
	for _, ch := range d.listeners {
		select {
		case ch <- p:
		default:
			// Channel full, skip
		}
	}
}

// GetProgress returns current progress for a file
func (d *Downloader) GetProgress(fileID string) *Progress {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if p, ok := d.progress[fileID]; ok {
		return p
	}
	return nil
}

// GetAllProgress returns all active downloads
func (d *Downloader) GetAllProgress() map[string]*Progress {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]*Progress)
	for k, v := range d.progress {
		result[k] = v
	}
	return result
}

// CheckFileStatus checks if a file exists and its size
func (d *Downloader) CheckFileStatus(rootDir, folder, fileName string) FileStatus {
	fullPath := filepath.Join(config.ExpandPath(rootDir), folder, fileName)
	info, err := os.Stat(fullPath)
	if err != nil {
		return FileStatus{Exists: false, Size: 0}
	}
	return FileStatus{Exists: true, Size: info.Size()}
}

// Download downloads a file
func (d *Downloader) Download(ctx context.Context, entry config.FileEntry, rootDir string, token string, force bool) error {
	fullPath := filepath.Join(config.ExpandPath(rootDir), entry.Folder, entry.FileName)

	// Check if file exists and we're not forcing redownload
	if !force {
		if _, err := os.Stat(fullPath); err == nil {
			return nil // File exists, skip
		}
	}

	// Create directory if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Build download URL with token if needed
	downloadURL := entry.URL
	if entry.UseToken && token != "" {
		downloadURL = appendToken(entry.URL, token)
	}

	// Create context with cancel
	ctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	d.cancelFns[entry.ID] = cancel
	d.progress[entry.ID] = &Progress{
		FileID:   entry.ID,
		FileName: entry.FileName,
		Status:   "downloading",
	}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.cancelFns, entry.ID)
		d.mu.Unlock()
	}()

	// Start download
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		d.updateProgress(entry.ID, func(p *Progress) {
			p.Status = "error"
			p.Error = err.Error()
		})
		return err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.updateProgress(entry.ID, func(p *Progress) {
			p.Status = "error"
			p.Error = err.Error()
		})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("bad status: %s", resp.Status)
		d.updateProgress(entry.ID, func(p *Progress) {
			p.Status = "error"
			p.Error = err.Error()
		})
		return err
	}

	// Create temp file
	tmpPath := fullPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		d.updateProgress(entry.ID, func(p *Progress) {
			p.Status = "error"
			p.Error = err.Error()
		})
		return err
	}

	total := resp.ContentLength
	d.updateProgress(entry.ID, func(p *Progress) {
		p.Total = total
	})

	// Download with progress tracking
	startTime := time.Now()
	var downloaded int64
	buf := make([]byte, 32*1024) // 32KB buffer

	for {
		select {
		case <-ctx.Done():
			file.Close()
			os.Remove(tmpPath)
			d.updateProgress(entry.ID, func(p *Progress) {
				p.Status = "cancelled"
			})
			return ctx.Err()
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := file.Write(buf[:n])
			if writeErr != nil {
				file.Close()
				os.Remove(tmpPath)
				d.updateProgress(entry.ID, func(p *Progress) {
					p.Status = "error"
					p.Error = writeErr.Error()
				})
				return writeErr
			}
			downloaded += int64(n)

			// Update progress
			elapsed := time.Since(startTime).Seconds()
			var percent float64
			if total > 0 {
				percent = float64(downloaded) / float64(total) * 100
			}
			var speed float64
			if elapsed > 0 {
				speed = float64(downloaded) / elapsed
			}

			d.updateProgress(entry.ID, func(p *Progress) {
				p.Downloaded = downloaded
				p.Percent = percent
				p.Speed = speed
			})
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			file.Close()
			os.Remove(tmpPath)
			d.updateProgress(entry.ID, func(p *Progress) {
				p.Status = "error"
				p.Error = err.Error()
			})
			return err
		}
	}

	file.Close()

	// Rename temp file to final
	if err := os.Rename(tmpPath, fullPath); err != nil {
		os.Remove(tmpPath)
		d.updateProgress(entry.ID, func(p *Progress) {
			p.Status = "error"
			p.Error = err.Error()
		})
		return err
	}

	d.updateProgress(entry.ID, func(p *Progress) {
		p.Status = "completed"
		p.Percent = 100
		p.Downloaded = downloaded
	})

	return nil
}

// Cancel cancels a download
func (d *Downloader) Cancel(fileID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cancel, ok := d.cancelFns[fileID]; ok {
		cancel()
	}
}

// DeleteFile deletes a file from disk
func (d *Downloader) DeleteFile(rootDir, folder, fileName string) error {
	fullPath := filepath.Join(config.ExpandPath(rootDir), folder, fileName)
	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (d *Downloader) updateProgress(fileID string, fn func(p *Progress)) {
	d.mu.Lock()
	if p, ok := d.progress[fileID]; ok {
		fn(p)
		// Broadcast update
		d.broadcast(*p)
	}
	d.mu.Unlock()
}

// appendToken adds token parameter to URL
func appendToken(rawURL string, token string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		// If can't parse, just append directly
		if strings.Contains(rawURL, "?") {
			return rawURL + "&token=" + token
		}
		return rawURL + "?token=" + token
	}
	
	q := parsed.Query()
	q.Set("token", token)
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// IsCivitaiURL checks if URL is from civitai.com
func IsCivitaiURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return strings.Contains(strings.ToLower(rawURL), "civitai.com")
	}
	return strings.Contains(strings.ToLower(parsed.Host), "civitai.com")
}

// GetFileInfoFromURL fetches filename from URL using HEAD request
func GetFileInfoFromURL(targetURL string, token string) (fileName string, fileSize int64) {
	// Build URL with token if it's civitai
	requestURL := targetURL
	if token != "" && IsCivitaiURL(targetURL) {
		requestURL = appendToken(targetURL, token)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Try HEAD request first
	fileName, fileSize = tryGetFileInfo(client, "HEAD", requestURL)
	if fileName != "" && !looksLikeID(fileName) {
		return fileName, fileSize
	}

	// For civitai and other sites that don't support HEAD properly,
	// try GET with Range header to get just the headers
	fileName, fileSize = tryGetFileInfo(client, "GET", requestURL)
	if fileName != "" && !looksLikeID(fileName) {
		return fileName, fileSize
	}

	// Fallback to URL path
	return extractFileNameFromURL(targetURL), fileSize
}

func tryGetFileInfo(client *http.Client, method string, targetURL string) (fileName string, fileSize int64) {
	req, err := http.NewRequest(method, targetURL, nil)
	if err != nil {
		return "", 0
	}

	// Add Range header for GET to avoid downloading entire file
	if method == "GET" {
		req.Header.Set("Range", "bytes=0-0")
	}

	// Add User-Agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0
	}
	defer resp.Body.Close()

	// Drain the body to allow connection reuse
	io.Copy(io.Discard, resp.Body)

	fileSize = resp.ContentLength
	if fileSize < 0 {
		// Try Content-Range header for Range requests
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			// Format: bytes 0-0/totalsize
			if idx := strings.LastIndex(contentRange, "/"); idx != -1 {
				if size, err := strconv.ParseInt(contentRange[idx+1:], 10, 64); err == nil {
					fileSize = size
				}
			}
		}
	}

	// Try to get filename from Content-Disposition header
	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition != "" {
		fileName = parseContentDisposition(contentDisposition)
		if fileName != "" {
			return fileName, fileSize
		}
	}

	return "", fileSize
}

// looksLikeID checks if filename looks like just an ID (numbers only)
func looksLikeID(name string) bool {
	// Remove extension if any
	if idx := strings.LastIndex(name, "."); idx != -1 {
		name = name[:idx]
	}
	// Check if it's all digits
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(name) > 0
}

// parseContentDisposition extracts filename from Content-Disposition header
func parseContentDisposition(header string) string {
	// Handle: attachment; filename="file.zip"
	// Handle: attachment; filename=file.zip
	// Handle: attachment; filename*=UTF-8''file.zip
	
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		
		// Check for filename*= (RFC 5987)
		if strings.HasPrefix(strings.ToLower(part), "filename*=") {
			value := part[10:]
			// Handle UTF-8 encoding like: UTF-8''filename.ext
			if idx := strings.Index(value, "''"); idx != -1 {
				encoded := value[idx+2:]
				decoded, err := url.PathUnescape(encoded)
				if err == nil {
					return decoded
				}
				return encoded
			}
			return strings.Trim(value, "\"'")
		}
		
		// Check for filename=
		if strings.HasPrefix(strings.ToLower(part), "filename=") {
			value := part[9:]
			return strings.Trim(value, "\"'")
		}
	}
	return ""
}

// extractFileNameFromURL extracts filename from URL path
func extractFileNameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		// Try simple extraction
		parts := strings.Split(rawURL, "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if idx := strings.Index(last, "?"); idx != -1 {
				last = last[:idx]
			}
			decoded, _ := url.PathUnescape(last)
			return decoded
		}
		return ""
	}
	
	path := parsed.Path
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		decoded, _ := url.PathUnescape(last)
		return decoded
	}
	return ""
}

