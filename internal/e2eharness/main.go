package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/bicilique/BastionGate-Demo/internal/app"
)

const (
	blockedFileID  = "123e4567-e89b-12d3-a456-426614174001"
	releasedFileID = "123e4567-e89b-12d3-a456-426614174002"
)

func main() {
	fixture := newFixture()
	upstream := httptest.NewServer(fixture)
	defer upstream.Close()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: upstream.URL,
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "e2e-server-secret",
		PolicyCode:             "DEFAULT",
		MaxUploadBytes:         2 << 20,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(http.ListenAndServe("127.0.0.1:13001", handler))
}

type fixture struct {
	mu        sync.RWMutex
	fileNames map[string]string
}

func newFixture() *fixture {
	return &fixture{fileNames: make(map[string]string)}
}

func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/actuator/health" {
		writeFixtureJSON(w, http.StatusOK, map[string]string{"status": "UP"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer e2e-server-secret" {
		writeFixtureJSON(w, http.StatusUnauthorized, map[string]string{"message": "missing test credential"})
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/files/upload":
		f.upload(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
		f.status(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/report"):
		f.report(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/download"):
		f.download(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeFixtureJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeFixtureJSON(w, http.StatusBadRequest, map[string]string{"message": "file required"})
		return
	}
	file.Close()
	if r.FormValue("policyCode") != "DEFAULT" ||
		!strings.HasPrefix(r.FormValue("sourceReference"), "acme-profile-") ||
		r.Header.Get("X-Correlation-ID") == "" {
		writeFixtureJSON(w, http.StatusBadRequest, map[string]string{"message": "server metadata missing"})
		return
	}

	fileID := releasedFileID
	if header.Filename == "avatar.php.jpg" {
		fileID = blockedFileID
	}
	f.mu.Lock()
	f.fileNames[fileID] = header.Filename
	f.mu.Unlock()
	writeFixtureJSON(w, http.StatusAccepted, map[string]any{
		"fileId": fileID, "status": "QUEUED", "sourceType": "DIRECT_UPLOAD",
	})
}

func (f *fixture) status(w http.ResponseWriter, r *http.Request) {
	fileID := pathFileID(r.URL.Path)
	if fileID == blockedFileID {
		writeFixtureJSON(w, http.StatusOK, map[string]any{
			"fileId": fileID, "status": "BLOCKED", "finalVerdict": "MALICIOUS",
		})
		return
	}
	if fileID == releasedFileID {
		writeFixtureJSON(w, http.StatusOK, map[string]any{
			"fileId": fileID, "status": "RELEASED", "finalVerdict": "CLEAN",
		})
		return
	}
	http.NotFound(w, r)
}

func (f *fixture) report(w http.ResponseWriter, r *http.Request) {
	fileID := pathFileID(r.URL.Path)
	f.mu.RLock()
	fileName := f.fileNames[fileID]
	f.mu.RUnlock()
	if fileID == blockedFileID {
		writeFixtureJSON(w, http.StatusOK, map[string]any{
			"fileId": fileID, "fileName": fileName, "policyCode": "DEFAULT",
			"status": "BLOCKED", "finalVerdict": "MALICIOUS",
			"engines": []map[string]any{{
				"engine": "CLAMAV", "threatName": "Eicar-Signature",
			}},
			"quarantine": map[string]any{
				"status": "ACTIVE", "quarantineBucket": "private-bucket",
			},
		})
		return
	}
	if fileID == releasedFileID {
		writeFixtureJSON(w, http.StatusOK, map[string]any{
			"fileId": fileID, "fileName": fileName, "policyCode": "DEFAULT",
			"status": "RELEASED", "finalVerdict": "CLEAN",
			"engines": []map[string]any{{
				"engine": "CLAMAV", "clean": true, "result": "OK",
			}},
		})
		return
	}
	http.NotFound(w, r)
}

func (f *fixture) download(w http.ResponseWriter, r *http.Request) {
	if pathFileID(r.URL.Path) != releasedFileID {
		http.Error(w, "not released", http.StatusForbidden)
		return
	}
	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `attachment; filename="clean.png"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

func pathFileID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 5 {
		return ""
	}
	return parts[3]
}

func writeFixtureJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
