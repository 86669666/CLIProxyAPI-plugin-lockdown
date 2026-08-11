package logging

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	log "github.com/sirupsen/logrus"
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
	homeRequestBodyMaxBytes           = 1 << 20
	homeStreamingResponseBodyMaxBytes = 4 << 20
	homeAPIRequestMaxBytes            = 2 << 20
	homeAPIResponseMaxBytes           = 2 << 20
	homeWebsocketTimelineMaxBytes     = 1 << 20
	homeAPIWebsocketTimelineMaxBytes  = 1 << 20
	homeRequestLogTextMaxBytes        = 12 << 20
	homeRequestLogPayloadMaxBytes     = 16 << 20
	homeRequestIDMaxBytes             = 4 << 10
	homeFileSectionMaxParts           = 256
	homeHeaderMaxKeys                 = 128
	homeHeaderMaxValuesPerKey         = 16
	homeHeaderKeyMaxBytes             = 256
	homeHeaderValueMaxBytes           = 8 << 10
	homeHeadersMaxBytes               = 64 << 10
	homeRequestBodyTruncatedMarker    = "\n[REQUEST BODY TRUNCATED]\n"
	homeResponseBodyTruncatedMarker   = "\n[RESPONSE BODY TRUNCATED]\n"
	homeAPIRequestTruncatedMarker     = "\n[API REQUEST TRUNCATED]\n"
	homeAPIResponseTruncatedMarker    = "\n[API RESPONSE TRUNCATED]\n"
	homeWebsocketTruncatedMarker      = "\n[WEBSOCKET TIMELINE TRUNCATED]\n"
	homeAPIWebsocketTruncatedMarker   = "\n[API WEBSOCKET TIMELINE TRUNCATED]\n"
	homeRequestLogTruncatedMarker     = "\n[REQUEST LOG TRUNCATED]\n"
	homeHeadersTruncatedMarker        = "[HEADERS TRUNCATED]"
	homeHeadersTruncatedKey           = "X-CLIProxyAPI-Log-Headers-Truncated"
)

type boundedHomeLogBuffer struct {
	buffer    bytes.Buffer
	maxBytes  int
	truncated bool
}

func (b *boundedHomeLogBuffer) Write(payload []byte) (int, error) {
	if b == nil || len(payload) == 0 {
		return len(payload), nil
	}
	remaining := b.maxBytes - b.buffer.Len() - len(homeRequestLogTruncatedMarker)
	if remaining <= 0 {
		b.truncated = true
		return len(payload), nil
	}
	writeLen := len(payload)
	if writeLen > remaining {
		writeLen = remaining
		b.truncated = true
	}
	_, _ = b.buffer.Write(payload[:writeLen])
	return len(payload), nil
}

func (b *boundedHomeLogBuffer) String() string {
	if b == nil {
		return ""
	}
	if !b.truncated {
		return b.buffer.String()
	}
	return b.buffer.String() + homeRequestLogTruncatedMarker
}

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

func boundedRedactedHomeHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	markerBytes := len(homeHeadersTruncatedKey) + len(homeHeadersTruncatedMarker)
	dataLimit := homeHeadersMaxBytes - markerBytes
	out := make(map[string][]string, min(len(headers), homeHeaderMaxKeys)+1)
	totalBytes := 0
	keyCount := 0
	truncated := false
	for key, values := range headers {
		if keyCount >= homeHeaderMaxKeys {
			truncated = true
			break
		}
		keyCount++
		if strings.TrimSpace(key) == "" {
			continue
		}

		boundedKey := truncateHomeString(key, homeHeaderKeyMaxBytes, homeHeadersTruncatedMarker)
		if len(boundedKey) != len(key) {
			truncated = true
		}
		if totalBytes+len(boundedKey) > dataLimit {
			truncated = true
			break
		}
		totalBytes += len(boundedKey)

		valueLimit := len(values)
		if valueLimit > homeHeaderMaxValuesPerKey {
			valueLimit = homeHeaderMaxValuesPerKey
			truncated = true
		}
		boundedValues := make([]string, 0, valueLimit)
		for index := 0; index < valueLimit; index++ {
			value := values[index]
			boundedValue := truncateHomeString(value, homeHeaderValueMaxBytes, homeHeadersTruncatedMarker)
			if len(boundedValue) != len(value) {
				truncated = true
			}
			boundedValue = redactHeaderValue(key, boundedValue)
			if totalBytes+len(boundedValue) > dataLimit {
				truncated = true
				break
			}
			boundedValues = append(boundedValues, boundedValue)
			totalBytes += len(boundedValue)
		}
		if len(boundedValues) > 0 || values == nil {
			out[boundedKey] = boundedValues
		}
		if truncated && totalBytes >= dataLimit {
			break
		}
	}
	if keyCount < len(headers) {
		truncated = true
	}
	if truncated {
		out[homeHeadersTruncatedKey] = []string{homeHeadersTruncatedMarker}
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
		Headers:    boundedRedactedHomeHeaders(headers),
		RequestID:  truncateHomeString(strings.TrimSpace(requestID), homeRequestIDMaxBytes, ""),
		RequestLog: truncateHomeString(logText, homeRequestLogTextMaxBytes, homeRequestLogTruncatedMarker),
	}
	raw, errMarshal := marshalBoundedHomeRequestLogPayload(payload)
	if errMarshal != nil {
		return errMarshal
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return client.RPushRequestLog(ctx, raw)
}

func truncateHomeString(value string, maxBytes int, marker string) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	if len(marker) >= maxBytes {
		return marker[:maxBytes]
	}
	return value[:maxBytes-len(marker)] + marker
}

func cloneBoundedHomeSection(payload []byte, maxBytes int, marker string) []byte {
	if len(payload) == 0 || maxBytes <= 0 {
		return nil
	}
	if len(payload) <= maxBytes {
		return bytes.Clone(payload)
	}
	return markHomeSectionTruncated(payload, maxBytes, marker)
}

func markHomeSectionTruncated(payload []byte, maxBytes int, marker string) []byte {
	if maxBytes <= 0 {
		return nil
	}
	if len(marker) >= maxBytes {
		return bytes.Clone([]byte(marker[:maxBytes]))
	}
	payloadBytes := min(len(payload), maxBytes-len(marker))
	out := make([]byte, 0, payloadBytes+len(marker))
	out = append(out, payload[:payloadBytes]...)
	out = append(out, marker...)
	return out
}

func readBoundedHomeSection(payload []byte, source *FileBodySource, maxBytes int, marker string) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, nil
	}
	probeLimit := maxBytes + 1
	out := make([]byte, 0, min(probeLimit, len(payload)+1))
	truncated := false
	appendProbe := func(data []byte) {
		if len(data) == 0 || len(out) >= probeLimit {
			if len(data) > 0 {
				truncated = true
			}
			return
		}
		remaining := probeLimit - len(out)
		if len(data) > remaining {
			data = data[:remaining]
			truncated = true
		}
		out = append(out, data...)
	}

	appendProbe(payload)
	wrote := len(payload) > 0
	remaining := probeLimit - len(out)
	if wrote && remaining > 0 {
		remaining--
	}
	part, sourceTruncated, errRead := source.ReadBounded(homeFileSectionMaxParts, remaining)
	if errRead != nil {
		return nil, errRead
	}
	if len(part) > 0 {
		if wrote {
			appendProbe([]byte("\n"))
		}
		appendProbe(part)
	}
	if sourceTruncated {
		truncated = true
	}
	if len(out) > maxBytes {
		truncated = true
	}
	if !truncated {
		return out, nil
	}
	return markHomeSectionTruncated(out, maxBytes, marker), nil
}

func (l *FileRequestLogger) boundedHomeResponse(responseHeaders map[string][]string, response []byte) ([]byte, error) {
	boundedResponse := cloneBoundedHomeSection(response, homeStreamingResponseBodyMaxBytes, homeResponseBodyTruncatedMarker)
	if len(response) == 0 || len(response) > homeStreamingResponseBodyMaxBytes {
		return boundedResponse, nil
	}

	var contentEncoding string
	for key, values := range responseHeaders {
		if strings.EqualFold(key, "content-encoding") && len(values) > 0 {
			contentEncoding = strings.ToLower(strings.TrimSpace(values[0]))
			break
		}
	}

	var reader io.Reader
	var closeReader func() error
	switch contentEncoding {
	case "gzip":
		gzipReader, errReader := gzip.NewReader(bytes.NewReader(response))
		if errReader != nil {
			return boundedResponse, fmt.Errorf("failed to create gzip reader: %w", errReader)
		}
		reader = gzipReader
		closeReader = gzipReader.Close
	case "deflate":
		flateReader := flate.NewReader(bytes.NewReader(response))
		reader = flateReader
		closeReader = flateReader.Close
	case "br":
		reader = brotli.NewReader(bytes.NewReader(response))
	case "zstd":
		zstdReader, errReader := zstd.NewReader(bytes.NewReader(response))
		if errReader != nil {
			return boundedResponse, fmt.Errorf("failed to create zstd reader: %w", errReader)
		}
		reader = zstdReader
		closeReader = func() error {
			zstdReader.Close()
			return nil
		}
	default:
		return boundedResponse, nil
	}
	if closeReader != nil {
		defer func() {
			if errClose := closeReader(); errClose != nil {
				log.WithError(errClose).Warn("failed to close bounded Home response reader")
			}
		}()
	}

	decompressed, errRead := io.ReadAll(io.LimitReader(reader, int64(homeStreamingResponseBodyMaxBytes+1)))
	if errRead != nil {
		return boundedResponse, fmt.Errorf("failed to decompress Home response: %w", errRead)
	}
	return cloneBoundedHomeSection(decompressed, homeStreamingResponseBodyMaxBytes, homeResponseBodyTruncatedMarker), nil
}

func (l *FileRequestLogger) buildBoundedHomeRequestLog(
	url, method string,
	requestHeaders map[string][]string,
	requestBody []byte,
	statusCode int,
	responseHeaders map[string][]string,
	response, websocketTimeline []byte,
	websocketTimelineSource *FileBodySource,
	apiRequest []byte,
	apiRequestSource *FileBodySource,
	apiResponse []byte,
	apiResponseSource *FileBodySource,
	apiWebsocketTimeline []byte,
	apiWebsocketTimelineSource *FileBodySource,
	apiResponseErrors []*interfaces.ErrorMessage,
	requestTimestamp, apiResponseTimestamp time.Time,
) (string, error) {
	requestHeaders = boundedRedactedHomeHeaders(requestHeaders)
	responseHeaders = boundedRedactedHomeHeaders(responseHeaders)
	boundedRequestBody := cloneBoundedHomeSection(requestBody, homeRequestBodyMaxBytes, homeRequestBodyTruncatedMarker)
	boundedWebsocketTimeline, errTimeline := readBoundedHomeSection(websocketTimeline, websocketTimelineSource, homeWebsocketTimelineMaxBytes, homeWebsocketTruncatedMarker)
	if errTimeline != nil {
		return "", errTimeline
	}
	boundedAPIRequest, errAPIRequest := readBoundedHomeSection(apiRequest, apiRequestSource, homeAPIRequestMaxBytes, homeAPIRequestTruncatedMarker)
	if errAPIRequest != nil {
		return "", errAPIRequest
	}
	boundedAPIResponse, errAPIResponse := readBoundedHomeSection(apiResponse, apiResponseSource, homeAPIResponseMaxBytes, homeAPIResponseTruncatedMarker)
	if errAPIResponse != nil {
		return "", errAPIResponse
	}
	boundedAPITimeline, errAPITimeline := readBoundedHomeSection(apiWebsocketTimeline, apiWebsocketTimelineSource, homeAPIWebsocketTimelineMaxBytes, homeAPIWebsocketTruncatedMarker)
	if errAPITimeline != nil {
		return "", errAPITimeline
	}
	boundedResponse, decompressErr := l.boundedHomeResponse(responseHeaders, response)
	if decompressErr != nil {
		boundedResponse = cloneBoundedHomeSection(response, homeStreamingResponseBodyMaxBytes, homeResponseBodyTruncatedMarker)
	}

	buf := &boundedHomeLogBuffer{maxBytes: homeRequestLogTextMaxBytes}
	writeErr := l.writeNonStreamingLog(
		buf,
		url,
		method,
		requestHeaders,
		boundedRequestBody,
		"",
		boundedWebsocketTimeline,
		nil,
		boundedAPIRequest,
		nil,
		boundedAPIResponse,
		nil,
		boundedAPITimeline,
		nil,
		apiResponseErrors,
		statusCode,
		responseHeaders,
		boundedResponse,
		decompressErr,
		requestTimestamp,
		apiResponseTimestamp,
	)
	if writeErr != nil {
		return "", writeErr
	}
	return buf.String(), nil
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
		requestHeaders: boundedRedactedHomeHeaders(headers),
		requestBody:    cloneBoundedHomeSection(body, homeRequestBodyMaxBytes, homeRequestBodyTruncatedMarker),
		requestID:      truncateHomeString(strings.TrimSpace(requestID), homeRequestIDMaxBytes, ""),
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
		w.responseHeaders = boundedRedactedHomeHeaders(headers)
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
	w.apiRequest = cloneBoundedHomeSection(apiRequest, homeAPIRequestMaxBytes, homeAPIRequestTruncatedMarker)
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
	w.apiResponse = cloneBoundedHomeSection(apiResponse, homeAPIResponseMaxBytes, homeAPIResponseTruncatedMarker)
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
	w.apiWebsocketTime = cloneBoundedHomeSection(apiWebsocketTimeline, homeAPIWebsocketTimelineMaxBytes, homeAPIWebsocketTruncatedMarker)
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
		responsePayload := w.responseBody.Bytes()
		responseBodyTruncated := w.responseBodyTruncated
		if responseBodyTruncated && len(responsePayload) > homeStreamingResponseBodyMaxBytes-len(homeResponseBodyTruncatedMarker) {
			responsePayload = responsePayload[:homeStreamingResponseBodyMaxBytes-len(homeResponseBodyTruncatedMarker)]
		}
		responseStatus := w.responseStatus
		statusWritten := w.statusWritten
		responseHeaders := w.responseHeaders
		apiRequest := w.apiRequest
		apiResponse := w.apiResponse
		apiWebsocketTime := w.apiWebsocketTime
		requestHeaders := w.requestHeaders
		requestBody := w.requestBody
		url, method, timestamp := w.url, w.method, w.timestamp
		requestID, apiResponseTS := w.requestID, w.apiResponseTS
		w.chunkMu.Unlock()

		buf := &boundedHomeLogBuffer{maxBytes: homeRequestLogTextMaxBytes}
		upstreamTransport := inferUpstreamTransport(apiRequest, nil, apiResponse, nil, apiWebsocketTime, nil, nil)
		if errWrite := writeRequestInfoWithBody(buf, url, method, requestHeaders, requestBody, "", timestamp, "http", upstreamTransport, true); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeAPISection(buf, "=== API WEBSOCKET TIMELINE ===\n", "=== API WEBSOCKET TIMELINE", apiWebsocketTime, time.Time{}); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeAPISection(buf, "=== API REQUEST ===\n", "=== API REQUEST", apiRequest, time.Time{}); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeAPISection(buf, "=== API RESPONSE ===\n", "=== API RESPONSE", apiResponse, apiResponseTS); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if errWrite := writeResponseSection(buf, responseStatus, statusWritten, responseHeaders, bytes.NewReader(responsePayload), nil, false); errWrite != nil {
			w.closeErr = errWrite
			w.releaseMemory()
			return
		}
		if responseBodyTruncated {
			_, _ = io.WriteString(buf, homeResponseBodyTruncatedMarker)
		}

		payload := homeRequestLogPayload{
			Headers:    requestHeaders,
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
	payload.RequestID = truncateHomeString(payload.RequestID, homeRequestIDMaxBytes, "")
	payload.RequestLog = truncateHomeString(payload.RequestLog, homeRequestLogTextMaxBytes, homeRequestLogTruncatedMarker)
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
