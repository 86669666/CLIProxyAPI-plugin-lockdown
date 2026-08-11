package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

type homeRequestLogClient interface {
	HeartbeatOK() bool
	RPushRequestLog(ctx context.Context, payload []byte) error
}

var currentHomeRequestLogClient = func() homeRequestLogClient {
	return home.Current()
}

type homeRequestLogPayload struct {
	Headers    map[string][]string `json:"headers,omitempty"`
	RequestID  string              `json:"request_id,omitempty"`
	RequestLog string              `json:"request_log,omitempty"`
}

const (
	homeStreamingResponseBodyMaxBytes = 8 << 20
	homeRequestLogPayloadMaxBytes     = 16 << 20
	homeRequestLogTruncatedMarker     = "\n[REQUEST LOG TRUNCATED]\n"
	homeResponseBodyTruncatedMarker   = "[RESPONSE BODY TRUNCATED]"
)

func cloneHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if values == nil {
			out[key] = nil
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (l *FileRequestLogger) forwardRequestLogToHome(ctx context.Context, headers map[string][]string, requestID string, logText string) error {
	if l == nil || !l.homeEnabled {
		return nil
	}
	client := currentHomeRequestLogClient()
	if client == nil || !client.HeartbeatOK() {
		return nil
	}
	payload := homeRequestLogPayload{
		Headers:    RedactHeaders(headers),
		RequestID:  strings.TrimSpace(requestID),
		RequestLog: logText,
	}
	raw, errMarshal := json.Marshal(&payload)
	if errMarshal != nil {
		return errMarshal
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return client.RPushRequestLog(ctx, raw)
}

// SetHomeEnabled toggles home request-log forwarding.
// When enabled, request logs are not written to disk and are instead forwarded to home via Redis RESP.
func (l *FileRequestLogger) SetHomeEnabled(enabled bool) {
	if l == nil {
		return
	}
	l.homeEnabled = enabled
}

type homeStreamingLogWriter struct {
	url       string
	method    string
	timestamp time.Time

	requestHeaders map[string][]string
	requestBody    []byte

	chunkChan chan []byte
	doneChan  chan struct{}
	chunkMu   sync.Mutex
	closeOnce sync.Once
	closed    bool
	closeErr  error
	queuedLen int

	responseStatus        int
	statusWritten         bool
	responseHeaders       map[string][]string
	responseBody          bytes.Buffer
	responseBodyTruncated bool
	apiRequest            []byte
	apiResponse           []byte
	apiWebsocketTime      []byte
	requestID             string
	apiResponseTS         time.Time
	firstChunkTS          time.Time
}

func newHomeStreamingLogWriter(url, method string, headers map[string][]string, body []byte, requestID string) *homeStreamingLogWriter {
	writer := &homeStreamingLogWriter{
		url:            url,
		method:         method,
		timestamp:      time.Now(),
		requestHeaders: RedactHeaders(headers),
		requestBody:    append([]byte(nil), body...),
		requestID:      strings.TrimSpace(requestID),
		chunkChan:      make(chan []byte, 100),
		doneChan:       make(chan struct{}),
	}

	go writer.asyncWriter()
	return writer
}

func (w *homeStreamingLogWriter) asyncWriter() {
	defer close(w.doneChan)
	for chunk := range w.chunkChan {
		if len(chunk) == 0 {
			continue
		}
		_, _ = w.responseBody.Write(chunk)
	}
}

func (w *homeStreamingLogWriter) WriteChunkAsync(chunk []byte) {
	if w == nil || len(chunk) == 0 {
		return
	}

	w.chunkMu.Lock()
	defer w.chunkMu.Unlock()
	if w.closed || w.chunkChan == nil {
		return
	}
	remaining := homeStreamingResponseBodyMaxBytes - w.queuedLen
	if remaining <= 0 {
		w.responseBodyTruncated = true
		return
	}
	chunkLen := len(chunk)
	if chunkLen > remaining {
		chunkLen = remaining
		w.responseBodyTruncated = true
	}
	chunkCopy := append([]byte(nil), chunk[:chunkLen]...)
	select {
	case w.chunkChan <- chunkCopy:
		w.queuedLen += chunkLen
	default:
		w.responseBodyTruncated = true
	}
}

func (w *homeStreamingLogWriter) WriteStatus(status int, headers map[string][]string) error {
	if w == nil || status == 0 {
		return nil
	}
	w.chunkMu.Lock()
	defer w.chunkMu.Unlock()
	if w.closed {
		return nil
	}
	w.responseStatus = status
	w.statusWritten = true
	if headers != nil {
		w.responseHeaders = RedactHeaders(headers)
	}
	return nil
}

func (w *homeStreamingLogWriter) WriteAPIRequest(apiRequest []byte) error {
	if w == nil || len(apiRequest) == 0 {
		return nil
	}
	w.chunkMu.Lock()
	defer w.chunkMu.Unlock()
	if w.closed {
		return nil
	}
	w.apiRequest = bytes.Clone(apiRequest)
	return nil
}

func (w *homeStreamingLogWriter) WriteAPIResponse(apiResponse []byte) error {
	if w == nil || len(apiResponse) == 0 {
		return nil
	}
	w.chunkMu.Lock()
	defer w.chunkMu.Unlock()
	if w.closed {
		return nil
	}
	w.apiResponse = bytes.Clone(apiResponse)
	return nil
}

func (w *homeStreamingLogWriter) WriteAPIWebsocketTimeline(apiWebsocketTimeline []byte) error {
	if w == nil || len(apiWebsocketTimeline) == 0 {
		return nil
	}
	w.chunkMu.Lock()
	defer w.chunkMu.Unlock()
	if w.closed {
		return nil
	}
	w.apiWebsocketTime = bytes.Clone(apiWebsocketTimeline)
	return nil
}

func (w *homeStreamingLogWriter) SetFirstChunkTimestamp(timestamp time.Time) {
	if w == nil {
		return
	}
	if !timestamp.IsZero() {
		w.chunkMu.Lock()
		defer w.chunkMu.Unlock()
		if w.closed {
			return
		}
		w.firstChunkTS = timestamp
		w.apiResponseTS = timestamp
	}
}

func (w *homeStreamingLogWriter) Close() error {
	if w == nil {
		return nil
	}

	w.closeOnce.Do(func() {
		w.chunkMu.Lock()
		w.closed = true
		chunkChan := w.chunkChan
		if chunkChan != nil {
			close(chunkChan)
		}
		w.chunkMu.Unlock()

		<-w.doneChan

		client := currentHomeRequestLogClient()
		if client == nil || !client.HeartbeatOK() {
			w.releaseMemory()
			return
		}

		w.chunkMu.Lock()
		responsePayload := bytes.Clone(w.responseBody.Bytes())
		responseBodyTruncated := w.responseBodyTruncated
		responseStatus := w.responseStatus
		statusWritten := w.statusWritten
		responseHeaders := cloneHeaders(w.responseHeaders)
		apiRequest := bytes.Clone(w.apiRequest)
		apiResponse := bytes.Clone(w.apiResponse)
		apiWebsocketTime := bytes.Clone(w.apiWebsocketTime)
		requestHeaders := cloneHeaders(w.requestHeaders)
		requestBody := bytes.Clone(w.requestBody)
		url, method, timestamp := w.url, w.method, w.timestamp
		requestID, apiResponseTS := w.requestID, w.apiResponseTS
		w.chunkMu.Unlock()

		var buf bytes.Buffer
		upstreamTransport := inferUpstreamTransport(apiRequest, nil, apiResponse, nil, apiWebsocketTime, nil, nil)
		if errWrite := writeRequestInfoWithBody(&buf, url, method, requestHeaders, requestBody, "", timestamp, "http", upstreamTransport, true); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeAPISection(&buf, "=== API WEBSOCKET TIMELINE ===\n", "=== API WEBSOCKET TIMELINE", apiWebsocketTime, time.Time{}); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeAPISection(&buf, "=== API REQUEST ===\n", "=== API REQUEST", apiRequest, time.Time{}); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeAPISection(&buf, "=== API RESPONSE ===\n", "=== API RESPONSE", apiResponse, apiResponseTS); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeResponseSection(&buf, responseStatus, statusWritten, responseHeaders, bytes.NewReader(responsePayload), nil, false); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if responseBodyTruncated {
			_, _ = buf.WriteString("\n" + homeResponseBodyTruncatedMarker + "\n")
		}

		payload := homeRequestLogPayload{
			Headers:    RedactHeaders(requestHeaders),
			RequestID:  requestID,
			RequestLog: buf.String(),
		}
		raw, errMarshal := marshalBoundedHomeRequestLogPayload(payload)
		if errMarshal != nil {
			w.closeErr = errMarshal
			w.releaseMemory()
			return
		}
		w.closeErr = client.RPushRequestLog(context.Background(), raw)
		w.releaseMemory()
	})
	return w.closeErr
}

func (w *homeStreamingLogWriter) releaseMemory() {
	w.chunkMu.Lock()
	defer w.chunkMu.Unlock()
	w.responseBody = bytes.Buffer{}
	w.requestHeaders = nil
	w.requestBody = nil
	w.responseHeaders = nil
	w.apiRequest = nil
	w.apiResponse = nil
	w.apiWebsocketTime = nil
	w.chunkChan = nil
}

func marshalBoundedHomeRequestLogPayload(payload homeRequestLogPayload) ([]byte, error) {
	raw, errMarshal := json.Marshal(&payload)
	if errMarshal != nil || len(raw) <= homeRequestLogPayloadMaxBytes {
		return raw, errMarshal
	}

	low, high := 0, len(payload.RequestLog)
	for low <= high {
		middle := low + (high-low)/2
		candidate := payload
		candidate.RequestLog = payload.RequestLog[:middle] + homeRequestLogTruncatedMarker
		raw, errMarshal = json.Marshal(&candidate)
		if errMarshal != nil {
			return nil, errMarshal
		}
		if len(raw) <= homeRequestLogPayloadMaxBytes {
			low = middle + 1
			continue
		}
		high = middle - 1
	}

	candidate := payload
	if high >= 0 {
		candidate.RequestLog = payload.RequestLog[:high] + homeRequestLogTruncatedMarker
	} else {
		candidate.RequestLog = homeRequestLogTruncatedMarker
	}
	raw, errMarshal = json.Marshal(&candidate)
	if errMarshal == nil && len(raw) <= homeRequestLogPayloadMaxBytes {
		return raw, nil
	}
	candidate.Headers = nil
	candidate.RequestID = ""
	candidate.RequestLog = homeRequestLogTruncatedMarker
	return json.Marshal(&candidate)
}
