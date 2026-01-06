package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"multy-loader/internal/config"
	"multy-loader/internal/downloader"
)

// CheckCivitaiURL checks if URL is from civitai.com
func (h *Handler) CheckCivitaiURL(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	isCivitai := downloader.IsCivitaiURL(url)
	jsonResponse(w, map[string]bool{"isCivitai": isCivitai})
}

// GetFileInfo fetches filename from URL headers
func (h *Handler) GetFileInfo(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		errorResponse(w, http.StatusBadRequest, "url required")
		return
	}
	token := r.URL.Query().Get("token")

	fileName, fileSize := downloader.GetFileInfoFromURL(targetURL, token)
	jsonResponse(w, map[string]interface{}{
		"fileName": fileName,
		"fileSize": fileSize,
	})
}

// Handler holds dependencies for HTTP handlers
type Handler struct {
	configMgr  *config.Manager
	downloader *downloader.Downloader
}

// NewHandler creates a new handler
func NewHandler(configMgr *config.Manager, dl *downloader.Downloader) *Handler {
	return &Handler{
		configMgr:  configMgr,
		downloader: dl,
	}
}

// Response helpers
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func errorResponse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// ListConfigs returns all config names
func (h *Handler) ListConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := h.configMgr.ListConfigs()
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, configs)
}

// GetConfig returns a specific config
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		errorResponse(w, http.StatusBadRequest, "config name required")
		return
	}

	cfg, err := h.configMgr.LoadConfig(name)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}
	jsonResponse(w, cfg)
}

// SaveConfig saves a config
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := h.configMgr.SaveConfig(&cfg); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]string{"status": "ok"})
}

// DeleteConfig deletes a config
func (h *Handler) DeleteConfig(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		errorResponse(w, http.StatusBadRequest, "config name required")
		return
	}

	if err := h.configMgr.DeleteConfig(name); err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}
	jsonResponse(w, map[string]string{"status": "ok"})
}

// GetFolders returns folders in a directory
func (h *Handler) GetFolders(w http.ResponseWriter, r *http.Request) {
	rootDir := r.URL.Query().Get("root")
	if rootDir == "" {
		errorResponse(w, http.StatusBadRequest, "root directory required")
		return
	}

	folders, err := config.GetFoldersInRoot(rootDir)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, folders)
}

// FileStatusRequest for checking file status
type FileStatusRequest struct {
	RootDir string             `json:"rootDir"`
	Files   []config.FileEntry `json:"files"`
}

// FileStatusResponse contains file statuses
type FileStatusResponse struct {
	Statuses map[string]downloader.FileStatus `json:"statuses"`
}

// CheckFileStatus checks status of files
func (h *Handler) CheckFileStatus(w http.ResponseWriter, r *http.Request) {
	var req FileStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	statuses := make(map[string]downloader.FileStatus)
	for _, f := range req.Files {
		statuses[f.ID] = h.downloader.CheckFileStatus(req.RootDir, f.Folder, f.FileName)
	}
	jsonResponse(w, FileStatusResponse{Statuses: statuses})
}

// DownloadRequest for downloading files
type DownloadRequest struct {
	RootDir string             `json:"rootDir"`
	Token   string             `json:"token"`
	Files   []config.FileEntry `json:"files"`
	Force   bool               `json:"force"`
}

// Download initiates downloads
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Start downloads in background
	go func() {
		var wg sync.WaitGroup
		for _, f := range req.Files {
			wg.Add(1)
			go func(entry config.FileEntry) {
				defer wg.Done()
				h.downloader.Download(context.Background(), entry, req.RootDir, req.Token, req.Force)
			}(f)
		}
		wg.Wait()
	}()

	jsonResponse(w, map[string]string{"status": "started"})
}

// CancelDownload cancels a download
func (h *Handler) CancelDownload(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("id")
	if fileID == "" {
		errorResponse(w, http.StatusBadRequest, "file id required")
		return
	}
	h.downloader.Cancel(fileID)
	jsonResponse(w, map[string]string{"status": "cancelled"})
}

// GetProgress returns download progress
func (h *Handler) GetProgress(w http.ResponseWriter, r *http.Request) {
	progress := h.downloader.GetAllProgress()
	jsonResponse(w, progress)
}

// DeleteFileRequest for deleting a file
type DeleteFileRequest struct {
	RootDir  string `json:"rootDir"`
	Folder   string `json:"folder"`
	FileName string `json:"fileName"`
}

// DeleteFile deletes a file from disk
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	var req DeleteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := h.downloader.DeleteFile(req.RootDir, req.Folder, req.FileName); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]string{"status": "ok"})
}

// ExtractRequest for extracting archive
type ExtractRequest struct {
	RootDir  string `json:"rootDir"`
	Folder   string `json:"folder"`
	FileName string `json:"fileName"`
}

// ExtractArchive extracts an archive file
func (h *Handler) ExtractArchive(w http.ResponseWriter, r *http.Request) {
	var req ExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if !downloader.IsArchive(req.FileName) {
		errorResponse(w, http.StatusBadRequest, "file is not a supported archive")
		return
	}

	extracted, err := h.downloader.ExtractArchive(req.RootDir, req.Folder, req.FileName)
	if err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Convert to config.ExtractedFile format
	extractedFiles := make([]config.ExtractedFile, len(extracted))
	for i, e := range extracted {
		extractedFiles[i] = config.ExtractedFile{
			Name: e.Name,
			Size: e.Size,
		}
	}

	jsonResponse(w, map[string]interface{}{
		"status":    "ok",
		"extracted": extractedFiles,
	})
}

// DeleteExtractedFileRequest for deleting an extracted file
type DeleteExtractedFileRequest struct {
	RootDir  string `json:"rootDir"`
	Folder   string `json:"folder"`
	FileName string `json:"fileName"` // Path to extracted file relative to folder
}

// DeleteExtractedFile deletes an extracted file from disk
func (h *Handler) DeleteExtractedFile(w http.ResponseWriter, r *http.Request) {
	var req DeleteExtractedFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := h.downloader.DeleteExtractedFile(req.RootDir, req.Folder, req.FileName); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonResponse(w, map[string]string{"status": "ok"})
}

// CheckArchive checks if file is an archive
func (h *Handler) CheckArchive(w http.ResponseWriter, r *http.Request) {
	fileName := r.URL.Query().Get("fileName")
	isArchive := downloader.IsArchive(fileName)
	jsonResponse(w, map[string]bool{"isArchive": isArchive})
}

// SSE endpoint for real-time progress updates
func (h *Handler) ProgressStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		errorResponse(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering if present

	ch := h.downloader.Subscribe()
	defer h.downloader.Unsubscribe(ch)

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n")
	flusher.Flush()

	// Heartbeat to keep connection alive (important for SSH tunnels)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// Send heartbeat comment to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case progress := <-ch:
			data, _ := json.Marshal(progress)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ExportConfig exports a config as JSON for download
func (h *Handler) ExportConfig(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		errorResponse(w, http.StatusBadRequest, "config name required")
		return
	}

	cfg, err := h.configMgr.LoadConfig(name)
	if err != nil {
		errorResponse(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", name))
	json.NewEncoder(w).Encode(cfg)
}

// ImportConfig imports a config from uploaded JSON
func (h *Handler) ImportConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		errorResponse(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if cfg.Name == "" {
		errorResponse(w, http.StatusBadRequest, "config name required")
		return
	}

	if err := h.configMgr.SaveConfig(&cfg); err != nil {
		errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]string{"status": "ok", "name": cfg.Name})
}

// ConfigHandler routes /api/config based on method
func (h *Handler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetConfig(w, r)
	case http.MethodPost:
		h.SaveConfig(w, r)
	case http.MethodDelete:
		h.DeleteConfig(w, r)
	default:
		errorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// FileHandler routes /api/file based on method
func (h *Handler) FileHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		h.DeleteFile(w, r)
	default:
		errorResponse(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
