package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	_ "image/webp"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

const (
	uploadDir   = "./uploads"
	maxSize     = 50 * 1024 * 1024 // 50 MB
	templateDir = "./templates"
)

type ImageMeta struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	URL    string            `json:"url"`
	Size   int64             `json:"size"`
	Mime   string            `json:"mime"`
	Width  int               `json:"width,omitempty"`
	Height int               `json:"height,omitempty"`
	Exif   map[string]string `json:"exif,omitempty"`
}

type UploadResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	Error   string `json:"error,omitempty"`
}

func main() {
	// Ensure directories exist
	os.MkdirAll(uploadDir, 0755)
	os.MkdirAll(templateDir, 0755)
	os.MkdirAll("./static", 0755)

	// Create templates if missing
	createTemplates()

	// Static file server
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// Routes
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api", handleAPI)

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	images := scanImages(uploadDir)
	shuffleImages(images)
	bgPool := images
	if len(images) > 6 {
		bgPool = images[:6]
	}

	data := struct {
		Images []string
		BGPool []string
		Year   int
	}{
		Images: images,
		BGPool: bgPool,
		Year:   time.Now().Year(),
	}

	tmpl := template.Must(template.ParseFiles(filepath.Join(templateDir, "index.html")))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, data)
}

func handleAPI(w http.ResponseWriter, r *http.Request) {
	// Common headers
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	// Allow preflight
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.Method {
	case "GET":
		handleListImages(w, r)
	case "POST":
		handleUpload(w, r)
	default:
		writeJSONError(w, "Unsupported method", http.StatusMethodNotAllowed)
	}
}

func handleListImages(w http.ResponseWriter, r *http.Request) {
	images := scanImages(uploadDir)
	var result []ImageMeta

	for _, img := range images {
		filePath := filepath.Join(uploadDir, img)
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		mimeType := mime.TypeByExtension(filepath.Ext(img))
		if mimeType == "" {
			// try to detect
			f, _ := os.Open(filePath)
			buf := make([]byte, 512)
			n, _ := f.Read(buf)
			mimeType = http.DetectContentType(buf[:n])
			f.Close()
		}

		meta := ImageMeta{
			ID:   img,
			Name: img,
			URL:  "/uploads/" + img,
			Size: info.Size(),
			Mime: mimeType,
		}

		// Get image dimensions
		f, err := os.Open(filePath)
		if err == nil {
			cfg, _, err := image.DecodeConfig(f)
			if err == nil {
				meta.Width = cfg.Width
				meta.Height = cfg.Height
			}
			f.Seek(0, 0)
			// Read EXIF (best-effort)
			x, err := exif.Decode(f)
			if err == nil && x != nil {
				meta.Exif = map[string]string{}
				if tm, err := x.DateTime(); err == nil {
					meta.Exif["DateTime"] = tm.Format(time.RFC3339)
				}
				if cam, err := x.Get(exif.Model); err == nil {
					meta.Exif["CameraModel"], _ = cam.StringVal()
				}
				if make, err := x.Get(exif.Make); err == nil {
					meta.Exif["CameraMake"], _ = make.StringVal()
				}
				if lat, long, err := x.LatLong(); err == nil {
					meta.Exif["Latitude"] = fmt.Sprintf("%f", lat)
					meta.Exif["Longitude"] = fmt.Sprintf("%f", long)
				}
			}
			f.Close()
		}

		result = append(result, meta)
	}

	// Sort by name
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	json.NewEncoder(w).Encode(result)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeJSONError(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Check file size
	if header.Size > maxSize {
		writeJSONError(w, "File exceeds maximum size 50 MB", http.StatusBadRequest)
		return
	}

	// Read first 512 bytes to detect content type
	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil && err != io.EOF {
		writeJSONError(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	file.Seek(0, 0) // Reset file pointer

	contentType := http.DetectContentType(buffer)
	if !strings.HasPrefix(contentType, "image/") {
		writeJSONError(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	// Generate safe filename
	ext := filepath.Ext(header.Filename)
	_ = ext
	safeName := regexp.MustCompile(`[^a-zA-Z0-9\.\-_]`).ReplaceAllString(header.Filename, "_")
	uniqueName := randomString(12) + "_" + safeName

	// Create target file
	targetPath := filepath.Join(uploadDir, uniqueName)
	targetFile, err := os.Create(targetPath)
	if err != nil {
		writeJSONError(w, "Could not save file", http.StatusInternalServerError)
		return
	}
	defer targetFile.Close()

	// Copy file content
	_, err = io.Copy(targetFile, file)
	if err != nil {
		writeJSONError(w, "Could not save file", http.StatusInternalServerError)
		return
	}

	info, _ := os.Stat(targetPath)
	response := UploadResponse{
		Success: true,
		ID:      uniqueName,
		URL:     "/uploads/" + uniqueName,
		Size:    info.Size(),
	}

	json.NewEncoder(w).Encode(response)
}

func scanImages(dir string) []string {
	var images []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return images
	}

	imageRegex := regexp.MustCompile(`(?i)\.(jpe?g|png|webp|gif)$`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if imageRegex.MatchString(entry.Name()) {
			images = append(images, entry.Name())
		}
	}

	sort.Strings(images)
	return images
}

func shuffleImages(images []string) {
	if len(images) <= 1 {
		return
	}
	// Use crypto/rand for seeding
	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	seed := int64(0)
	for _, b := range randBytes {
		seed = seed*256 + int64(b)
	}
	// fallback to simple shuffle
	for i := range images {
		j := int(seed%int64(len(images)))
		images[i], images[j] = images[j], images[i]
		seed = (seed*1664525 + 1013904223) & 0xffffffff
	}
}

func randomString(length int) string {
	const chars = "0123456789abcdef"
	bytes := make([]byte, length)
	rand.Read(bytes)
	for i := range bytes {
		bytes[i] = chars[bytes[i]%byte(len(chars))]
	}
	return string(bytes)
}

func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func createTemplates() {
	// Only create if missing
	path := filepath.Join(templateDir, "index.html")
	if _, err := os.Stat(path); err == nil {
		return
	}

	indexHTML := ` + "`" + `<!doctype html>
<html lang="cs">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width,initial-scale=1" />
<title>AI-Morph Galerie — Neuromorphic</title>

<script src="https://cdn.tailwindcss.com"></script>
<script src="https://unpkg.com/feather-icons"></script>

<link rel="stylesheet" href="/static/styles.css" />

</head>
<body class="dark"> 
<div id="bg-wrap" aria-hidden="true">
  {{range $i, $bg := .BGPool}}
  <div class="bg-layer" id="bg-{{$i}}" data-bg-url="/uploads/{{$bg}}"></div>
  {{end}}
</div>

<header class="w-full">
  <div class="container flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold">AI-Morph Galerie</h1>
      <p class="text-sm text-gray-300/70">Neuromorphic UI · ratio-aware modal · AI-like morph</p>
    </div>

    <div class="flex items-center gap-3">
      <label id="upload-label" class="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-white/6 hover:bg-white/8 cursor-pointer card">
        <svg data-feather="upload" class="w-4 h-4"></svg>
        <span class="text-sm">Nahrát</span>
        <input id="upload" type="file" accept="image/*" multiple class="sr-only" />
      </label>

      <button id="toggle-theme" class="inline-flex items-center gap-2 px-3 py-2 rounded-lg bg-white/6 hover:bg-white/8 card" title="Přepnout motiv">
        <svg data-feather="moon" class="w-4 h-4"></svg>
      </button>
    </div>
  </div>
</header>

<main class="container mt-6">
  <div id="grid" class="grid"></div>
</main>

<footer class="container mt-10 mb-12 text-sm text-gray-300/70">
  © {{.Year}} — Until Design Fluent
</footer>

<div id="modal" aria-hidden="true" role="dialog">
  <div id="modal-overlay" onclick="closeModal()"></div>
  <div id="modal-body" role="document" aria-label="Image preview">
    <div id="layer1" class="morph-layer">
      <img src="" alt="">
      <div class="layer-spinner" id="spinner1">⌛</div>
    </div>
    <div id="layer2" class="morph-layer">
      <img src="" alt="">
      <div class="layer-spinner" id="spinner2">⌛</div>
    </div>

    <div id="btn-close" class="ctrl" title="Zavřít" onclick="closeModal()"><svg data-feather="x" class="w-5 h-5"></svg></div>
    <div id="btn-prev" class="ctrl" title="Předchozí" onclick="prevImage()"><svg data-feather="chevron-left" class="w-5 h-5"></svg></div>
    <div id="btn-next" class="ctrl" title="Další" onclick="nextImage()"><svg data-feather="chevron-right" class="w-5 h-5"></svg></div>
    <div id="btn-download" class="ctrl" title="Stáhnout" onclick="downloadCurrent()"><svg data-feather="download" class="w-5 h-5"></svg></div>
  </div>
</div>

<div id="upload-modal" aria-hidden="true">
  <div class="upload-card card">
    <div>
      <div style="font-weight:700">Nahrávání souborů</div>
      <div id="upload-fname" style="opacity:.85;margin-top:6px;font-size:13px">Vyber soubory...</div>
      <div id="upload-status" style="opacity:.8;margin-top:8px;font-size:13px">0 / 0 • 0 B / 0 B</div>
    </div>

    <div style="display:flex;align-items:center;gap:12px">
      <div class="progress-wrap" aria-hidden="true">
        <svg class="progress-svg" width="108" height="108" viewBox="0 0 100 100">
          <circle cx="50" cy="50" r="40" stroke="rgba(255,255,255,0.12)" stroke-width="10" fill="none"></circle>
          <circle id="progress-circle" cx="50" cy="50" r="40" stroke="#ffffff" stroke-width="10" stroke-linecap="round" fill="none" stroke-dasharray="251.2" stroke-dashoffset="251.2"></circle>
        </svg>
        <div class="progress-text" id="progress-text">0%</div>
      </div>
    </div>
  </div>
</div>

<script src="/static/main.js"></script>

</body>
</html>` + "`" + `

	tmpl := template.Must(template.New("index.html").Funcs(template.FuncMap{
		"Year": func() int { return time.Now().Year() },
	}).Parse(indexHTML))

	file, err := os.Create(path)
	if err != nil {
		log.Println("Error creating template:", err)
		return
	}
	defer file.Close()

	tmpl.Execute(file, nil)
}
