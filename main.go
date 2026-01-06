package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"multy-loader/internal/config"
	"multy-loader/internal/downloader"
	"multy-loader/internal/handlers"
)

//go:embed web/templates/*
var webFS embed.FS

func main() {
	// Get executable directory for configs
	execPath, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	execDir := filepath.Dir(execPath)
	configsDir := filepath.Join(execDir, "configs")

	// For development: use current directory
	if os.Getenv("DEV") == "1" {
		cwd, _ := os.Getwd()
		configsDir = filepath.Join(cwd, "configs")
	}

	// Initialize config manager
	cfgMgr, err := config.NewManager(configsDir)
	if err != nil {
		log.Fatal("Failed to initialize config manager:", err)
	}

	// Initialize downloader
	dl := downloader.NewDownloader()

	// Initialize handlers
	h := handlers.NewHandler(cfgMgr, dl)

	// Setup routes
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/configs", h.ListConfigs)
	mux.HandleFunc("/api/config", h.ConfigHandler)
	mux.HandleFunc("/api/config/export", h.ExportConfig)
	mux.HandleFunc("/api/config/import", h.ImportConfig)
	mux.HandleFunc("/api/folders", h.GetFolders)
	mux.HandleFunc("/api/files/status", h.CheckFileStatus)
	mux.HandleFunc("/api/check-civitai", h.CheckCivitaiURL)
	mux.HandleFunc("/api/file-info", h.GetFileInfo)
	mux.HandleFunc("/api/download/cancel", h.CancelDownload)
	mux.HandleFunc("/api/download", h.Download)
	mux.HandleFunc("/api/progress", h.GetProgress)
	mux.HandleFunc("/api/progress/stream", h.ProgressStream)
	mux.HandleFunc("/api/file", h.FileHandler)

	// Serve embedded static files
	templatesFS, err := fs.Sub(webFS, "web/templates")
	if err != nil {
		log.Fatal("Failed to get templates FS:", err)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(templatesFS, "index.html")
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "9894"
	}

	addr := fmt.Sprintf(":%s", port)
	fmt.Printf("🚀 Multy Loader starting on http://localhost%s\n", addr)
	fmt.Printf("📁 Configs directory: %s\n", configsDir)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal("Server failed:", err)
	}
}
