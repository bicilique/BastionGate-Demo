package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bicilique/BastionGate-Demo/internal/app"
)

func TestAcceptedUploadRemainsUntrusted(t *testing.T) {
	t.Parallel()

	type observedRequest struct {
		authorization string
		correlationID string
		policyCode    string
		sourceRef     string
		fileName      string
		fileBody      string
	}
	observed := make(chan observedRequest, 1)

	bastionGate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/upload" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		observed <- observedRequest{
			authorization: r.Header.Get("Authorization"),
			correlationID: r.Header.Get("X-Correlation-ID"),
			policyCode:    r.FormValue("policyCode"),
			sourceRef:     r.FormValue("sourceReference"),
			fileName:      header.Filename,
			fileBody:      string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{
			"fileId":"5f219cbe-ddd7-4a73-82f5-0e5d0ba49de9",
			"status":"QUEUED",
			"sourceType":"DIRECT_UPLOAD",
			"receivedAt":"2026-07-26T12:00:00"
		}`)
	}))
	defer bastionGate.Close()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: bastionGate.URL,
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
		PolicyCode:             "DEFAULT",
		MaxUploadBytes:         2 << 20,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	var requestBody bytes.Buffer
	form := multipart.NewWriter(&requestBody)
	part, err := form.CreateFormFile("file", "avatar.php.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "harmless-test-bytes")
	_ = form.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/uploads", &requestBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 202; body=%s", response.StatusCode, body)
	}

	var payload struct {
		FileID        string `json:"fileId"`
		Status        string `json:"status"`
		Trust         string `json:"trust"`
		CorrelationID string `json:"correlationId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.FileID == "" || payload.Status != "QUEUED" {
		t.Fatalf("unexpected accepted response: %+v", payload)
	}
	if payload.Trust != "UNTRUSTED" {
		t.Fatalf("trust = %q, want UNTRUSTED", payload.Trust)
	}
	if payload.CorrelationID == "" {
		t.Fatal("correlationId is empty")
	}

	got := <-observed
	if got.authorization != "Bearer server-only-secret" {
		t.Fatalf("Authorization = %q", got.authorization)
	}
	if got.correlationID == "" || got.correlationID != payload.CorrelationID {
		t.Fatalf("correlation IDs differ: upstream=%q response=%q", got.correlationID, payload.CorrelationID)
	}
	if got.policyCode != "DEFAULT" {
		t.Fatalf("policyCode = %q", got.policyCode)
	}
	if !strings.HasPrefix(got.sourceRef, "acme-profile-") {
		t.Fatalf("sourceReference = %q", got.sourceRef)
	}
	if got.fileName != "avatar.php.jpg" || got.fileBody != "harmless-test-bytes" {
		t.Fatalf("forwarded file = %q %q", got.fileName, got.fileBody)
	}
}
