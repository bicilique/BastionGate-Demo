package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	webassets "github.com/bicilique/BastionGate-Demo/web"
)

const responseLimit = 1 << 20

type Config struct {
	BastionGateInternalURL string
	BastionGatePublicURL   string
	BastionGateAPIKey      string
	PolicyCode             string
	MaxUploadBytes         int64
}

type server struct {
	config Config
	client *http.Client
}

func NewHandler(config Config) (http.Handler, error) {
	if strings.TrimSpace(config.BastionGateAPIKey) == "" {
		return nil, errors.New("BASTIONGATE_API_KEY is required")
	}
	if _, err := url.ParseRequestURI(config.BastionGateInternalURL); err != nil {
		return nil, fmt.Errorf("invalid BASTIONGATE_INTERNAL_URL: %w", err)
	}
	if config.PolicyCode == "" {
		config.PolicyCode = "DEFAULT"
	}
	if config.MaxUploadBytes <= 0 {
		config.MaxUploadBytes = 2 << 20
	}

	s := &server{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/uploads", s.handleUpload)
	mux.HandleFunc("GET /api/files/{fileID}/status", s.handleStatus)
	mux.HandleFunc("GET /api/files/{fileID}/report", s.handleReport)
	mux.HandleFunc("GET /api/files/{fileID}/download", s.handleDownload)
	mux.Handle("/", http.FileServer(http.FS(webassets.Files)))
	return secureHeaders(mux), nil
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; "+
				"form-action 'self'; frame-ancestors 'none'; img-src 'self' blob:; "+
				"object-src 'none'; script-src 'self'; style-src 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	upstreamURL := strings.TrimRight(s.config.BastionGateInternalURL, "/") + "/actuator/health"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "disconnected"})
		return
	}
	response, err := s.client.Do(request)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "disconnected"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "disconnected"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.config.MaxUploadBytes+(64<<10))
	if err := r.ParseMultipartForm(s.config.MaxUploadBytes); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_MULTIPART", "A valid multipart file is required.")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "FILE_REQUIRED", "Select a file to upload.")
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(file, s.config.MaxUploadBytes+1))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "FILE_READ_FAILED", "The selected file could not be read.")
		return
	}
	if int64(len(fileBytes)) > s.config.MaxUploadBytes {
		writeProblem(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Profile photos are limited to 2 MiB.")
		return
	}

	correlationID, err := randomID()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ID_GENERATION_FAILED", "The upload could not be prepared.")
		return
	}
	sourceReference := "acme-profile-" + correlationID
	upstreamBody, contentType, err := buildUpstreamUpload(
		header.Filename,
		header.Header.Get("Content-Type"),
		fileBytes,
		s.config.PolicyCode,
		sourceReference,
	)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "UPLOAD_BUILD_FAILED", "The upload could not be prepared.")
		return
	}

	upstreamURL := strings.TrimRight(s.config.BastionGateInternalURL, "/") + "/api/v1/files/upload"
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, upstreamBody)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "UPLOAD_BUILD_FAILED", "The upload could not be prepared.")
		return
	}
	request.Header.Set("Authorization", "Bearer "+s.config.BastionGateAPIKey)
	request.Header.Set("X-Correlation-ID", correlationID)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "BASTIONGATE_UNAVAILABLE", "BastionGate could not be reached.")
		return
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		writeProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", "BastionGate returned an unreadable response.")
		return
	}
	if response.StatusCode != http.StatusAccepted {
		writeProblem(w, response.StatusCode, "UPLOAD_REJECTED", safeUpstreamMessage(raw))
		return
	}

	var accepted map[string]any
	if err := json.Unmarshal(raw, &accepted); err != nil {
		writeProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", "BastionGate returned invalid JSON.")
		return
	}
	if fileID, _ := accepted["fileId"].(string); fileID == "" {
		writeProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", "BastionGate did not return a file ID.")
		return
	}
	accepted["trust"] = "UNTRUSTED"
	accepted["correlationId"] = correlationID
	accepted["sourceReference"] = sourceReference
	writeJSON(w, http.StatusAccepted, accepted)
}

func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("fileID")
	status, upstreamStatus, err := s.getFileStatus(r.Context(), fileID)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "STATUS_UNAVAILABLE", "The file status could not be verified.")
		return
	}
	if upstreamStatus != http.StatusOK {
		writeProblem(w, upstreamStatus, "STATUS_REJECTED", "BastionGate did not return the file status.")
		return
	}
	if status.Status != "RELEASED" {
		writeProblem(w, http.StatusForbidden, "FILE_NOT_RELEASED", "Only files released by BastionGate can be downloaded.")
		return
	}

	upstreamURL := strings.TrimRight(s.config.BastionGateInternalURL, "/") +
		"/api/v1/files/" + url.PathEscape(fileID) + "/download"
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "DOWNLOAD_BUILD_FAILED", "The released file could not be requested.")
		return
	}
	request.Header.Set("Authorization", "Bearer "+s.config.BastionGateAPIKey)
	response, err := s.client.Do(request)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "BASTIONGATE_UNAVAILABLE", "BastionGate could not be reached.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeProblem(w, response.StatusCode, "DOWNLOAD_REJECTED", "BastionGate did not release the file.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, s.config.MaxUploadBytes+1))
	if err != nil || int64(len(body)) > s.config.MaxUploadBytes {
		writeProblem(w, http.StatusBadGateway, "INVALID_DOWNLOAD", "BastionGate returned an invalid released file.")
		return
	}
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", response.Header.Get("Content-Disposition"))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.proxyJSONResource(w, r, "status", false)
}

func (s *server) handleReport(w http.ResponseWriter, r *http.Request) {
	s.proxyJSONResource(w, r, "report", true)
}

func (s *server) proxyJSONResource(w http.ResponseWriter, r *http.Request, resource string, includeAuditURL bool) {
	fileID := r.PathValue("fileID")
	upstreamURL := strings.TrimRight(s.config.BastionGateInternalURL, "/") +
		"/api/v1/files/" + url.PathEscape(fileID) + "/" + resource
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "REQUEST_BUILD_FAILED", "The BastionGate request could not be prepared.")
		return
	}
	request.Header.Set("Authorization", "Bearer "+s.config.BastionGateAPIKey)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "BASTIONGATE_UNAVAILABLE", "BastionGate could not be reached.")
		return
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		writeProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", "BastionGate returned an unreadable response.")
		return
	}
	if response.StatusCode != http.StatusOK {
		if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		code := "BASTIONGATE_REQUEST_REJECTED"
		message := "BastionGate rejected the request."
		switch response.StatusCode {
		case http.StatusTooManyRequests:
			code = "RATE_LIMITED"
			message = "BastionGate rate limited status polling."
		case http.StatusUnauthorized, http.StatusForbidden:
			code = "BASTIONGATE_AUTH_REJECTED"
			message = "BastionGate rejected the configured API client."
		case http.StatusNotFound:
			code = "FILE_NOT_FOUND"
			message = "BastionGate could not find this file."
		}
		writeProblem(w, response.StatusCode, code, message)
		return
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", "BastionGate returned invalid JSON.")
		return
	}
	payload = redactSensitive(payload)
	if includeAuditURL {
		if object, ok := payload.(map[string]any); ok {
			object["auditUrl"] = strings.TrimRight(s.config.BastionGatePublicURL, "/") +
				"/files/" + url.PathEscape(fileID)
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

type fileStatus struct {
	Status       string `json:"status"`
	FinalVerdict string `json:"finalVerdict"`
}

var sensitiveJSONKeys = map[string]struct{}{
	"apikey":              {},
	"authorization":       {},
	"locationref":         {},
	"object_storage_path": {},
	"quarantinebucket":    {},
	"quarantinekey":       {},
	"storagecredentials":  {},
	"storageendpoint":     {},
}

func redactSensitive(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", ""))
			if _, sensitive := sensitiveJSONKeys[normalized]; sensitive {
				continue
			}
			clean[key] = redactSensitive(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = redactSensitive(item)
		}
		return clean
	default:
		return typed
	}
}

func (s *server) getFileStatus(ctx context.Context, fileID string) (fileStatus, int, error) {
	var status fileStatus
	upstreamURL := strings.TrimRight(s.config.BastionGateInternalURL, "/") +
		"/api/v1/files/" + url.PathEscape(fileID) + "/status"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return status, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+s.config.BastionGateAPIKey)
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return status, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		return status, response.StatusCode, errors.New("unreadable status response")
	}
	if response.StatusCode != http.StatusOK {
		return status, response.StatusCode, nil
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return status, response.StatusCode, err
	}
	return status, response.StatusCode, nil
}

func buildUpstreamUpload(
	fileName string,
	declaredContentType string,
	fileBytes []byte,
	policyCode string,
	sourceReference string,
) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("policyCode", policyCode); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("sourceReference", sourceReference); err != nil {
		return nil, "", err
	}
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": fileName,
	}))
	if declaredContentType == "" {
		declaredContentType = "application/octet-stream"
	}
	partHeader.Set("Content-Type", declaredContentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func safeUpstreamMessage(raw []byte) string {
	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		if payload.Message != "" {
			return payload.Message
		}
		if payload.Error != "" {
			return payload.Error
		}
	}
	return "BastionGate rejected the upload."
}

func writeProblem(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"code":    code,
		"message": message,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
