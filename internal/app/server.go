package app

import (
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
	"path/filepath"
	"strings"
	"time"

	webassets "github.com/bicilique/BastionGate-Demo/web"
)

const responseLimit = 1 << 20

var errFileTooLarge = errors.New("file exceeds configured upload limit")

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
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/uploads", s.handleUpload)
	mux.HandleFunc("GET /api/files/{fileID}/status", s.handleStatus)
	mux.HandleFunc("GET /api/files/{fileID}/report", s.handleReport)
	mux.HandleFunc("GET /api/files/{fileID}/download", s.handleDownload)
	mux.Handle("/", http.FileServer(http.FS(webassets.Files)))
	return secureHeaders(mux), nil
}

func (s *server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"maxUploadBytes": s.config.MaxUploadBytes,
		"policyCode":     s.config.PolicyCode,
	})
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
	reader, err := r.MultipartReader()
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_MULTIPART", "A valid multipart file is required.")
		return
	}

	var file *multipart.Part
	for {
		part, nextErr := reader.NextPart()
		if errors.As(nextErr, new(*http.MaxBytesError)) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", uploadLimitMessage(s.config.MaxUploadBytes))
			return
		}
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			writeProblem(w, http.StatusBadRequest, "INVALID_MULTIPART", "A valid multipart file is required.")
			return
		}
		if part.FormName() == "file" && part.FileName() != "" {
			file = part
			break
		}
		_ = part.Close()
	}
	if file == nil {
		writeProblem(w, http.StatusBadRequest, "FILE_REQUIRED", "Select a file to upload.")
		return
	}
	defer file.Close()
	if !supportedImageName(file.FileName()) {
		writeProblem(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_FILE_TYPE", "Choose a JPG or PNG profile photo.")
		return
	}

	correlationID, err := randomID()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ID_GENERATION_FAILED", "The upload could not be prepared.")
		return
	}
	sourceReference := "acme-profile-" + correlationID
	upstreamBody, contentType, streamResult := streamUpstreamUpload(
		file,
		s.config.PolicyCode,
		sourceReference,
		s.config.MaxUploadBytes,
	)

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

	response, requestErr := s.client.Do(request)
	streamErr := <-streamResult
	if errors.Is(streamErr, errFileTooLarge) {
		if response != nil {
			response.Body.Close()
		}
		writeProblem(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", uploadLimitMessage(s.config.MaxUploadBytes))
		return
	}
	if streamErr != nil {
		if response != nil {
			response.Body.Close()
		}
		writeProblem(w, http.StatusBadRequest, "FILE_READ_FAILED", "The selected file could not be read.")
		return
	}
	if requestErr != nil {
		writeProblem(w, http.StatusBadGateway, "BASTIONGATE_UNAVAILABLE", "BastionGate could not be reached.")
		return
	}
	if response == nil {
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
		writeProblem(w, response.StatusCode, "UPLOAD_REJECTED", "BastionGate rejected the upload.")
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
	writeJSON(w, http.StatusAccepted, filterBrowserSafe(accepted))
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
	payload = filterBrowserSafe(payload)
	if includeAuditURL && strings.TrimSpace(s.config.BastionGatePublicURL) != "" {
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

var browserSafeJSONKeys = map[string]struct{}{
	"completedAt":     {},
	"correlationId":   {},
	"deduplicated":    {},
	"durationMs":      {},
	"engine":          {},
	"engines":         {},
	"fileId":          {},
	"fileName":        {},
	"finalVerdict":    {},
	"md5":             {},
	"mimeType":        {},
	"name":            {},
	"policyCode":      {},
	"publicStatus":    {},
	"receivedAt":      {},
	"result":          {},
	"scan":            {},
	"scanEngines":     {},
	"sha256":          {},
	"signature":       {},
	"sizeBytes":       {},
	"sourceReference": {},
	"sourceType":      {},
	"startedAt":       {},
	"status":          {},
	"threatName":      {},
	"trust":           {},
	"updatedAt":       {},
	"verdict":         {},
	"version":         {},
}

func filterBrowserSafe(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			if _, safe := browserSafeJSONKeys[key]; !safe {
				continue
			}
			clean[key] = filterBrowserSafe(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = filterBrowserSafe(item)
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

func streamUpstreamUpload(
	file *multipart.Part,
	policyCode string,
	sourceReference string,
	maxUploadBytes int64,
) (io.Reader, string, <-chan error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	contentType := multipartWriter.FormDataContentType()
	result := make(chan error, 1)

	go func() {
		var streamErr error
		if streamErr = multipartWriter.WriteField("policyCode", policyCode); streamErr == nil {
			streamErr = multipartWriter.WriteField("sourceReference", sourceReference)
		}

		var output io.Writer
		if streamErr == nil {
			partHeader := make(textproto.MIMEHeader)
			partHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
				"name":     "file",
				"filename": file.FileName(),
			}))
			declaredContentType := file.Header.Get("Content-Type")
			if declaredContentType == "" {
				declaredContentType = "application/octet-stream"
			}
			partHeader.Set("Content-Type", declaredContentType)
			output, streamErr = multipartWriter.CreatePart(partHeader)
		}

		if streamErr == nil {
			var copied int64
			copied, streamErr = io.Copy(output, io.LimitReader(file, maxUploadBytes+1))
			if streamErr == nil && copied > maxUploadBytes {
				streamErr = errFileTooLarge
			}
		}
		if streamErr == nil {
			streamErr = multipartWriter.Close()
		}
		if streamErr != nil {
			_ = writer.CloseWithError(streamErr)
		} else {
			_ = writer.Close()
		}
		result <- streamErr
		close(result)
	}()

	return reader, contentType, result
}

func supportedImageName(fileName string) bool {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".jpg", ".jpeg", ".png":
		return true
	default:
		return false
	}
}

func uploadLimitMessage(maxUploadBytes int64) string {
	return fmt.Sprintf("Profile photos are limited to %d bytes.", maxUploadBytes)
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
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
