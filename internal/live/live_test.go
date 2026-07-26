package live_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bicilique/BastionGate-Demo/internal/app"
)

func TestLocalBastionGateBlocksRuntimeEICARWithClamAV(t *testing.T) {
	if os.Getenv("BASTIONGATE_LIVE_TEST") != "1" {
		t.Skip("set BASTIONGATE_LIVE_TEST=1 to run against local BastionGate")
	}
	apiKey := os.Getenv("BASTIONGATE_API_KEY")
	if apiKey == "" {
		t.Fatal("BASTIONGATE_API_KEY is required for the live test")
	}
	internalURL := envOrDefault("BASTIONGATE_INTERNAL_URL", "http://localhost:8080")
	publicURL := envOrDefault("BASTIONGATE_PUBLIC_URL", "http://localhost:8080")

	handler, err := app.NewHandler(app.Config{
		BastionGateInternalURL: internalURL,
		BastionGatePublicURL:   publicURL,
		BastionGateAPIKey:      apiKey,
		PolicyCode:             envOrDefault("BASTIONGATE_POLICY_CODE", "DEFAULT"),
		MaxUploadBytes:         2 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	demo := httptest.NewServer(handler)
	defer demo.Close()

	fileID := submitEICAR(t, demo.URL)
	status := pollFinalStatus(t, demo.URL, fileID)
	if status.Status != "BLOCKED" || status.FinalVerdict != "MALICIOUS" {
		t.Fatalf("final status = %+v, want BLOCKED/MALICIOUS", status)
	}

	response, err := http.Get(demo.URL + "/api/files/" + fileID + "/report")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("report status = %d: %s", response.StatusCode, body)
	}
	var report struct {
		AuditURL string `json:"auditUrl"`
		Engines  []struct {
			Engine     string `json:"engine"`
			ThreatName string `json:"threatName"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, engine := range report.Engines {
		if strings.EqualFold(engine.Engine, "CLAMAV") &&
			strings.Contains(strings.ToLower(engine.ThreatName), "eicar") {
			found = true
		}
	}
	if !found {
		t.Fatalf("report has no ClamAV EICAR evidence: %s", body)
	}
	wantAuditURL := strings.TrimRight(publicURL, "/") + "/files/" + fileID
	if report.AuditURL != wantAuditURL {
		t.Fatalf("auditUrl = %q, want %q", report.AuditURL, wantAuditURL)
	}
}

func submitEICAR(t *testing.T, demoURL string) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name": "file", "filename": "avatar.php.jpg",
	}))
	header.Set("Content-Type", "image/jpeg")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	eicar, err := os.ReadFile("../../testdata/avatar.php.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(eicar); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	response, err := http.Post(demoURL+"/api/uploads", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("upload status = %d: %s", response.StatusCode, raw)
	}
	var accepted struct {
		FileID string `json:"fileId"`
		Trust  string `json:"trust"`
	}
	if err := json.Unmarshal(raw, &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.FileID == "" || accepted.Trust != "UNTRUSTED" {
		t.Fatalf("unexpected accepted submission: %+v", accepted)
	}
	return accepted.FileID
}

func pollFinalStatus(t *testing.T, demoURL string, fileID string) struct {
	Status       string `json:"status"`
	FinalVerdict string `json:"finalVerdict"`
} {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(demoURL + "/api/files/" + fileID + "/status")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode == http.StatusTooManyRequests {
			time.Sleep(time.Second)
			continue
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status request = %d: %s", response.StatusCode, raw)
		}
		var status struct {
			Status       string `json:"status"`
			FinalVerdict string `json:"finalVerdict"`
		}
		if err := json.Unmarshal(raw, &status); err != nil {
			t.Fatal(err)
		}
		switch status.Status {
		case "RELEASED", "BLOCKED", "REVIEW_REQUIRED", "FAILED":
			return status
		}
		time.Sleep(time.Second)
	}
	t.Fatal("file did not reach a final status within 90 seconds")
	return struct {
		Status       string `json:"status"`
		FinalVerdict string `json:"finalVerdict"`
	}{}
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
