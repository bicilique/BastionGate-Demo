package app

import (
	"bytes"
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
	mux.HandleFunc("POST /api/uploads", s.handleUpload)
	return mux, nil
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
