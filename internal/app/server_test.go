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

func TestBlockedFileCannotBeDownloaded(t *testing.T) {
	t.Parallel()

	const fileID = "5f219cbe-ddd7-4a73-82f5-0e5d0ba49de9"
	downloadRequested := false
	bastionGate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer server-only-secret" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/files/" + fileID + "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"fileId":"`+fileID+`",
				"status":"BLOCKED",
				"publicStatus":"BLOCKED",
				"finalVerdict":"MALICIOUS"
			}`)
		case "/api/v1/files/" + fileID + "/download":
			downloadRequested = true
			_, _ = io.WriteString(w, "must not be requested")
		default:
			http.NotFound(w, r)
		}
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
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/files/" + fileID + "/download")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 403; body=%s", response.StatusCode, body)
	}
	if downloadRequested {
		t.Fatal("blocked file download was requested from BastionGate")
	}
}

func TestReleasedFileCanBeDownloaded(t *testing.T) {
	t.Parallel()

	const fileID = "6ad74e04-9254-439f-8a6b-a2a61d085407"
	bastionGate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/files/" + fileID + "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"fileId":"`+fileID+`",
				"status":"RELEASED",
				"publicStatus":"CLEAN",
				"finalVerdict":"CLEAN"
			}`)
		case "/api/v1/files/" + fileID + "/download":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="profile.png"`)
			_, _ = io.WriteString(w, "released-image-bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	defer bastionGate.Close()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: bastionGate.URL,
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
		MaxUploadBytes:         2 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/files/" + fileID + "/download")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.StatusCode, body)
	}
	if response.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type = %q", response.Header.Get("Content-Type"))
	}
	if string(body) != "released-image-bytes" {
		t.Fatalf("body = %q", body)
	}
}

func TestReportRedactsStorageAndCredentialFields(t *testing.T) {
	t.Parallel()

	const fileID = "7be62819-c3c5-4db2-9f33-d7c3491d0530"
	bastionGate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/"+fileID+"/report" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"fileId":"`+fileID+`",
			"status":"BLOCKED",
			"finalVerdict":"MALICIOUS",
			"quarantine":{
				"status":"ACTIVE",
				"quarantineBucket":"private-bucket",
				"quarantineKey":"private/object",
				"locationRef":"s3://private-bucket/private/object"
			},
			"engines":[{
				"engine":"CLAMAV",
				"threatName":"Eicar-Signature",
				"apiKey":"must-never-leak"
			}]
		}`)
	}))
	defer bastionGate.Close()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: bastionGate.URL,
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
		MaxUploadBytes:         2 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/files/" + fileID + "/report")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.StatusCode, body)
	}
	for _, forbidden := range []string{"private-bucket", "private/object", "must-never-leak", "quarantineBucket", "quarantineKey", "locationRef", "apiKey"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("report leaked %q: %s", forbidden, body)
		}
	}
	if !bytes.Contains(body, []byte("Eicar-Signature")) {
		t.Fatalf("report removed safe scan evidence: %s", body)
	}
	if !bytes.Contains(body, []byte("http://localhost:8080/files/"+fileID)) {
		t.Fatalf("report missing audit URL: %s", body)
	}
}

func TestStatusPreservesRetryAfterOnRateLimit(t *testing.T) {
	t.Parallel()

	const fileID = "d20cafcb-45b2-40ab-b6e4-1ab24f20d97c"
	bastionGate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"slow down","apiKey":"must-never-leak"}`)
	}))
	defer bastionGate.Close()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: bastionGate.URL,
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/files/" + fileID + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", response.StatusCode, body)
	}
	if response.Header.Get("Retry-After") != "3" {
		t.Fatalf("Retry-After = %q", response.Header.Get("Retry-After"))
	}
	if bytes.Contains(body, []byte("must-never-leak")) {
		t.Fatalf("error leaked upstream credentials: %s", body)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "RATE_LIMITED" {
		t.Fatalf("code = %q, want RATE_LIMITED", problem.Code)
	}
}

func TestHealthReportsBastionGateConnectionWithoutCredentials(t *testing.T) {
	t.Parallel()

	bastionGate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/actuator/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"status":"UP"}`)
	}))
	defer bastionGate.Close()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: bastionGate.URL,
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"status":"connected"`)) {
		t.Fatalf("unexpected health response: %s", body)
	}
	if bytes.Contains(body, []byte("server-only-secret")) || bytes.Contains(body, []byte(bastionGate.URL)) {
		t.Fatalf("health response leaked internal configuration: %s", body)
	}
}

func TestHomeServesAcmePeopleWithoutServerSecrets(t *testing.T) {
	t.Parallel()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: "http://127.0.0.1:1",
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.StatusCode, body)
	}
	if !bytes.Contains(body, []byte("Acme People")) {
		t.Fatalf("home does not identify the demo app: %s", body)
	}
	if bytes.Contains(body, []byte("server-only-secret")) {
		t.Fatal("home leaked the BastionGate API key")
	}
	if response.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("home is missing Content-Security-Policy")
	}
	if response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", response.Header.Get("X-Content-Type-Options"))
	}
}

func TestFaviconAssetIsAvailable(t *testing.T) {
	t.Parallel()

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: "http://127.0.0.1:1",
		BastionGatePublicURL:   "http://localhost:8080",
		BastionGateAPIKey:      "server-only-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/favicon.svg")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("Content-Type = %q", response.Header.Get("Content-Type"))
	}
}
