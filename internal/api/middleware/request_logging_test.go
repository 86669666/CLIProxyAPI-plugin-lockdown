package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
)

func TestShouldSkipMethodForRequestLogging(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		skip bool
	}{
		{
			name: "nil request",
			req:  nil,
			skip: true,
		},
		{
			name: "post request should not skip",
			req: &http.Request{
				Method: http.MethodPost,
				URL:    &url.URL{Path: "/v1/responses"},
			},
			skip: false,
		},
		{
			name: "plain get should skip",
			req: &http.Request{
				Method: http.MethodGet,
				URL:    &url.URL{Path: "/v1/models"},
				Header: http.Header{},
			},
			skip: true,
		},
		{
			name: "responses websocket upgrade should not skip",
			req: &http.Request{
				Method: http.MethodGet,
				URL:    &url.URL{Path: "/v1/responses"},
				Header: http.Header{"Upgrade": []string{"websocket"}},
			},
			skip: false,
		},
		{
			name: "codex responses websocket upgrade should not skip",
			req: &http.Request{
				Method: http.MethodGet,
				URL:    &url.URL{Path: "/backend-api/codex/responses"},
				Header: http.Header{"Upgrade": []string{"websocket"}},
			},
			skip: false,
		},
		{
			name: "responses get without upgrade should skip",
			req: &http.Request{
				Method: http.MethodGet,
				URL:    &url.URL{Path: "/v1/responses"},
				Header: http.Header{},
			},
			skip: true,
		},
	}

	for i := range tests {
		got := shouldSkipMethodForRequestLogging(tests[i].req)
		if got != tests[i].skip {
			t.Fatalf("%s: got skip=%t, want %t", tests[i].name, got, tests[i].skip)
		}
	}
}

func TestShouldCaptureRequestBody(t *testing.T) {
	tests := []struct {
		name          string
		loggerEnabled bool
		req           *http.Request
		want          bool
	}{
		{
			name:          "logger enabled defers unknown size",
			loggerEnabled: true,
			req: &http.Request{
				Body:          io.NopCloser(strings.NewReader("{}")),
				ContentLength: -1,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
			},
			want: false,
		},
		{
			name:          "nil request",
			loggerEnabled: false,
			req:           nil,
			want:          false,
		},
		{
			name:          "small known size json in error-only mode",
			loggerEnabled: false,
			req: &http.Request{
				Body:          io.NopCloser(strings.NewReader("{}")),
				ContentLength: 2,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
			},
			want: true,
		},
		{
			name:          "large known size skipped in error-only mode",
			loggerEnabled: false,
			req: &http.Request{
				Body:          io.NopCloser(strings.NewReader("x")),
				ContentLength: maxErrorOnlyCapturedRequestBodyBytes + 1,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
			},
			want: false,
		},
		{
			name:          "unknown size skipped in error-only mode",
			loggerEnabled: false,
			req: &http.Request{
				Body:          io.NopCloser(strings.NewReader("x")),
				ContentLength: -1,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
			},
			want: false,
		},
		{
			name:          "multipart skipped in error-only mode",
			loggerEnabled: false,
			req: &http.Request{
				Body:          io.NopCloser(strings.NewReader("x")),
				ContentLength: 1,
				Header:        http.Header{"Content-Type": []string{"multipart/form-data; boundary=abc"}},
			},
			want: false,
		},
	}

	for i := range tests {
		got := shouldCaptureRequestBody(tests[i].loggerEnabled, tests[i].req)
		if got != tests[i].want {
			t.Fatalf("%s: got %t, want %t", tests[i].name, got, tests[i].want)
		}
	}
}

func TestDeferredRequestBodyCaptureDoesNotDrainUnreadBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := logging.NewFileRequestLogger(false, t.TempDir(), "", 10)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("remaining-body"))
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	requestInfo := &RequestInfo{Headers: map[string][]string{"Content-Type": {"application/json"}}}
	capture := attachDeferredRequestBodyCapture(request, logger, requestInfo, false, false)
	if capture == nil {
		t.Fatal("deferred request body capture was not attached")
	}
	defer capture.Cleanup()

	firstByte := make([]byte, 1)
	if _, errRead := request.Body.Read(firstByte); errRead != nil {
		t.Fatalf("read first request byte: %v", errRead)
	}
	captured, marker, errCaptured := capture.Bytes()
	if errCaptured != nil {
		t.Fatalf("read captured body: %v", errCaptured)
	}
	if string(captured) != "r" {
		t.Fatalf("captured body = %q, want %q", string(captured), "r")
	}
	if !strings.Contains(marker, "REQUEST BODY CAPTURE INCOMPLETE") {
		t.Fatalf("capture marker = %q, want incomplete marker", marker)
	}
	remaining, errRemaining := io.ReadAll(capture.body)
	if errRemaining != nil {
		t.Fatalf("read remaining body: %v", errRemaining)
	}
	if string(remaining) != "emaining-body" {
		t.Fatalf("remaining body = %q, want %q", string(remaining), "emaining-body")
	}
}

func TestRequestLoggingMiddlewareCapturesLargeErrorRequestAndDeferredAPIRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logsDir := t.TempDir()
	logger := logging.NewFileRequestLogger(false, logsDir, "", 10)
	payload := append([]byte(`{"marker":"large-error-body","padding":"`), bytes.Repeat([]byte("x"), int(maxErrorOnlyCapturedRequestBodyBytes))...)
	payload = append(payload, []byte(`"}`)...)
	upstreamBody := []byte(`{"model":"upstream-model","input":"translated"}`)

	router := gin.New()
	router.Use(RequestLoggingMiddleware(logger))
	router.POST("/v1/responses", func(c *gin.Context) {
		body, errRead := io.ReadAll(c.Request.Body)
		if errRead != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if !bytes.Equal(body, payload) {
			c.Status(http.StatusInternalServerError)
			return
		}
		executorCtx := context.WithValue(context.Background(), "gin", c)
		helps.RecordAPIRequest(executorCtx, &config.Config{}, helps.UpstreamRequestLog{
			URL:     "https://api.example.com/v1/responses",
			Method:  http.MethodPost,
			Headers: http.Header{"Content-Type": []string{"application/json"}},
			Body:    upstreamBody,
		})
		c.JSON(http.StatusBadRequest, gin.H{"error": "upstream rejected request"})
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	entries, errReadDir := os.ReadDir(logsDir)
	if errReadDir != nil {
		t.Fatalf("read logs dir: %v", errReadDir)
	}
	var logPath string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
			logPath = logsDir + string(os.PathSeparator) + entry.Name()
			break
		}
	}
	if logPath == "" {
		t.Fatal("forced error log was not created")
	}
	content, errReadLog := os.ReadFile(logPath)
	if errReadLog != nil {
		t.Fatalf("read error log: %v", errReadLog)
	}
	if !bytes.Contains(content, payload) {
		t.Fatal("error log does not contain the complete large request body")
	}
	if !bytes.Contains(content, []byte("=== API REQUEST 1 ===")) {
		t.Fatal("error log does not contain the deferred API request section")
	}
	if !bytes.Contains(content, upstreamBody) {
		t.Fatal("error log does not contain the deferred upstream request body")
	}
}

func TestAttachRequestLogSourcesUsesLoggerLogsDir(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logsDir := t.TempDir()
	logger := logging.NewFileRequestLogger(true, logsDir, "", 0)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/backend-api/codex/responses", nil)
	c.Request.Header.Set("Upgrade", "websocket")

	attachRequestLogSources(c, logger, true)
	defer cleanupFileBodySourcesFromContext(c)

	for _, key := range []string{
		logging.WebsocketTimelineSourceContextKey,
		logging.APIWebsocketTimelineSourceContextKey,
	} {
		value, exists := c.Get(key)
		if !exists {
			t.Fatalf("expected %s source to be attached", key)
		}
		source, ok := value.(*logging.FileBodySource)
		if !ok || source == nil {
			t.Fatalf("%s source type = %T", key, value)
		}
		file, errPart := source.CreatePart("probe")
		if errPart != nil {
			t.Fatalf("CreatePart(%s): %v", key, errPart)
		}
		path := file.Name()
		if errClose := file.Close(); errClose != nil {
			t.Fatalf("close part: %v", errClose)
		}
		if !strings.HasPrefix(path, logsDir+string(os.PathSeparator)) {
			t.Fatalf("%s part path %s is not under logs dir %s", key, path, logsDir)
		}
	}
}

func cleanupFileBodySourcesFromContext(c *gin.Context) {
	if c == nil {
		return
	}
	for _, key := range []string{
		logging.WebsocketTimelineSourceContextKey,
		logging.APIWebsocketTimelineSourceContextKey,
	} {
		value, exists := c.Get(key)
		if !exists {
			continue
		}
		if source, ok := value.(*logging.FileBodySource); ok && source != nil {
			_ = source.Cleanup()
		}
	}
}

func TestDecodeCapturedRequestBodyForLogWithLimitTruncatesZstdExpansion(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1024)
	var compressed bytes.Buffer
	encoder, errNewWriter := zstd.NewWriter(&compressed)
	if errNewWriter != nil {
		t.Fatalf("zstd.NewWriter: %v", errNewWriter)
	}
	if _, errWrite := encoder.Write(payload); errWrite != nil {
		t.Fatalf("zstd write: %v", errWrite)
	}
	if errClose := encoder.Close(); errClose != nil {
		t.Fatalf("zstd close: %v", errClose)
	}

	decoded := decodeCapturedRequestBodyForLogWithLimit(compressed.Bytes(), "zstd", 64)
	if len(decoded) > 128 {
		t.Fatalf("limited decoded body length = %d, want bounded output", len(decoded))
	}
	if !bytes.Contains(decoded, []byte("DECOMPRESSED REQUEST BODY TRUNCATED")) {
		t.Fatalf("decoded body = %q, want truncation marker", string(decoded))
	}
}

func TestDecodeCapturedZstdRequestBodyWithLimitRejectsLargeWindow(t *testing.T) {
	payload := []byte("small payload")
	var compressed bytes.Buffer
	encoder, errNewWriter := zstd.NewWriter(&compressed, zstd.WithEncoderConcurrency(1))
	if errNewWriter != nil {
		t.Fatalf("zstd.NewWriter: %v", errNewWriter)
	}
	if _, errWrite := encoder.Write(payload); errWrite != nil {
		t.Fatalf("zstd write: %v", errWrite)
	}
	if errClose := encoder.Close(); errClose != nil {
		t.Fatalf("zstd close: %v", errClose)
	}

	highWindowFrame := bytes.Clone(compressed.Bytes())
	if len(highWindowFrame) < 6 || highWindowFrame[4]&0x20 != 0 {
		t.Fatalf("unexpected zstd frame header: %x", highWindowFrame)
	}
	const sixtyFourMiBWindowDescriptor byte = 0x80
	highWindowFrame[5] = sixtyFourMiBWindowDescriptor

	decoder, errNewReader := zstd.NewReader(bytes.NewReader(highWindowFrame))
	if errNewReader != nil {
		t.Fatalf("create unrestricted zstd decoder: %v", errNewReader)
	}
	decoded, errRead := io.ReadAll(decoder)
	decoder.Close()
	if errRead != nil {
		t.Fatalf("decode valid high-window frame: %v", errRead)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded high-window payload = %q, want %q", decoded, payload)
	}

	if _, _, errDecode := decodeCapturedZstdRequestBodyWithLimit(highWindowFrame, maxDeferredErrorRequestBodyBytes); errDecode == nil {
		t.Fatal("high-window zstd frame was accepted")
	}
}

func TestCaptureRequestInfoDecodesZstdRequestBodyForLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"model":"test-model","stream":true}`)
	var compressed bytes.Buffer
	encoder, errNewWriter := zstd.NewWriter(&compressed)
	if errNewWriter != nil {
		t.Fatalf("zstd.NewWriter: %v", errNewWriter)
	}
	if _, errWrite := encoder.Write(payload); errWrite != nil {
		t.Fatalf("zstd write: %v", errWrite)
	}
	if errClose := encoder.Close(); errClose != nil {
		t.Fatalf("zstd close: %v", errClose)
	}
	compressedBytes := compressed.Bytes()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressedBytes))
	req.Header.Set("Content-Encoding", "zstd")
	c.Request = req

	info, errCapture := captureRequestInfo(c, true)
	if errCapture != nil {
		t.Fatalf("captureRequestInfo: %v", errCapture)
	}
	if !bytes.Equal(info.Body, payload) {
		t.Fatalf("logged request body = %q, want %q", string(info.Body), string(payload))
	}

	restoredBody, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		t.Fatalf("read restored request body: %v", errRead)
	}
	if !bytes.Equal(restoredBody, compressedBytes) {
		t.Fatal("request body was not restored with the original compressed bytes")
	}
}

func TestRequestLoggingMiddleware_ClientCancellationExclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("499 status does not create error log when request-log is false", func(t *testing.T) {
		logsDir := t.TempDir()
		logger := logging.NewFileRequestLogger(false, logsDir, "", 10)

		router := gin.New()
		router.Use(RequestLoggingMiddleware(logger))
		router.POST("/v1/responses", func(c *gin.Context) {
			c.AbortWithStatus(clienterror.StatusClientClosedRequest)
		})

		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != clienterror.StatusClientClosedRequest {
			t.Fatalf("status = %d, want %d", resp.Code, clienterror.StatusClientClosedRequest)
		}

		entries, errRead := os.ReadDir(logsDir)
		if errRead != nil {
			t.Fatalf("read logs dir: %v", errRead)
		}
		if len(entries) != 0 {
			t.Fatalf("expected 0 log files for 499 cancellation in error-only mode, got %d files", len(entries))
		}
	})

	t.Run("context canceled does not create error log when request-log is false", func(t *testing.T) {
		logsDir := t.TempDir()
		logger := logging.NewFileRequestLogger(false, logsDir, "", 10)

		router := gin.New()
		router.Use(RequestLoggingMiddleware(logger))
		router.POST("/v1/responses", func(c *gin.Context) {
			// Simulate client closing connection mid-flight
			ctx, cancel := context.WithCancel(c.Request.Context())
			cancel()
			c.Request = c.Request.WithContext(ctx)
			c.Status(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		entries, errRead := os.ReadDir(logsDir)
		if errRead != nil {
			t.Fatalf("read logs dir: %v", errRead)
		}
		if len(entries) != 0 {
			t.Fatalf("expected 0 log files for canceled context in error-only mode, got %d files", len(entries))
		}
	})

	t.Run("400 bad request creates error log when request-log is false", func(t *testing.T) {
		logsDir := t.TempDir()
		logger := logging.NewFileRequestLogger(false, logsDir, "", 10)

		router := gin.New()
		router.Use(RequestLoggingMiddleware(logger))
		router.POST("/v1/responses", func(c *gin.Context) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parameter"})
		})

		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"bad":"param"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusBadRequest)
		}

		entries, errRead := os.ReadDir(logsDir)
		if errRead != nil {
			t.Fatalf("read logs dir: %v", errRead)
		}
		var errorLogCount int
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
				errorLogCount++
			}
		}
		if errorLogCount != 1 {
			t.Fatalf("expected 1 error log file for 400 Bad Request, got %d", errorLogCount)
		}
	})

	t.Run("499 status logs standard request when request-log is true", func(t *testing.T) {
		logsDir := t.TempDir()
		logger := logging.NewFileRequestLogger(true, logsDir, "", 10)

		router := gin.New()
		router.Use(RequestLoggingMiddleware(logger))
		router.POST("/v1/responses", func(c *gin.Context) {
			c.AbortWithStatus(clienterror.StatusClientClosedRequest)
		})

		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)

		entries, errRead := os.ReadDir(logsDir)
		if errRead != nil {
			t.Fatalf("read logs dir: %v", errRead)
		}
		var standardLogCount int
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "error-") && strings.HasSuffix(entry.Name(), ".log") {
				standardLogCount++
			}
		}
		if standardLogCount != 1 {
			t.Fatalf("expected 1 standard request log file when request-log=true, got %d", standardLogCount)
		}
	})
}

type boundedCaptureLogger struct {
	enabled      bool
	tempDir      string
	requestBody  []byte
	responseBody []byte
}

func (l *boundedCaptureLogger) LogRequest(_ string, _ string, _ map[string][]string, body []byte, _ int, _ map[string][]string, response []byte, _ []byte, _ []byte, _ []byte, _ []byte, _ []*interfaces.ErrorMessage, _ string, _ time.Time, _ time.Time) error {
	l.requestBody = body
	l.responseBody = response
	return nil
}

func (l *boundedCaptureLogger) LogStreamingRequest(string, string, map[string][]string, []byte, string) (logging.StreamingLogWriter, error) {
	return &testStreamingLogWriter{}, nil
}

func (l *boundedCaptureLogger) IsEnabled() bool {
	return l.enabled
}

func (l *boundedCaptureLogger) NewFileBodySource(prefix string) (*logging.FileBodySource, error) {
	return logging.NewFileBodySourceInDir(l.tempDir, prefix)
}

type constantByteReader struct {
	value     byte
	remaining int64
}

func (r *constantByteReader) Read(payload []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(payload)) > r.remaining {
		payload = payload[:r.remaining]
	}
	for index := range payload {
		payload[index] = r.value
	}
	r.remaining -= int64(len(payload))
	return len(payload), nil
}

func TestRequestLoggingMiddleware_EnabledLargeChunkedBodyIsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	bodySize := maxDeferredErrorRequestBodyBytes + 4096
	logger := &boundedCaptureLogger{enabled: true, tempDir: t.TempDir()}
	var consumed int64

	router := gin.New()
	router.Use(RequestLoggingMiddleware(logger))
	router.POST("/v1/responses", func(c *gin.Context) {
		var errCopy error
		consumed, errCopy = io.Copy(io.Discard, c.Request.Body)
		if errCopy != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", &constantByteReader{value: 'x', remaining: bodySize})
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if consumed != bodySize {
		t.Fatalf("handler consumed %d bytes, want %d", consumed, bodySize)
	}
	marker := "\n[REQUEST BODY TRUNCATED: captured first 33554432 bytes]"
	if len(logger.requestBody) != int(maxDeferredErrorRequestBodyBytes)+len(marker) {
		t.Fatalf("logged request body length = %d, want %d", len(logger.requestBody), int(maxDeferredErrorRequestBodyBytes)+len(marker))
	}
	if !bytes.Equal(logger.requestBody[:64], bytes.Repeat([]byte{'x'}, 64)) {
		t.Fatal("logged request body did not retain the expected prefix")
	}
	if !bytes.HasSuffix(logger.requestBody, []byte(marker)) {
		t.Fatalf("logged request body missing deterministic truncation marker: %q", logger.requestBody[len(logger.requestBody)-len(marker):])
	}
}

func TestRequestLoggingMiddleware_ZstdDecompressionIsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var compressed bytes.Buffer
	encoder, errNewWriter := zstd.NewWriter(&compressed)
	if errNewWriter != nil {
		t.Fatalf("zstd.NewWriter: %v", errNewWriter)
	}
	decompressedSize := maxDeferredErrorRequestBodyBytes + 4096
	if _, errCopy := io.Copy(encoder, &constantByteReader{value: 'z', remaining: decompressedSize}); errCopy != nil {
		t.Fatalf("compress request body: %v", errCopy)
	}
	if errClose := encoder.Close(); errClose != nil {
		t.Fatalf("close zstd encoder: %v", errClose)
	}
	compressedBody := bytes.Clone(compressed.Bytes())

	logger := &boundedCaptureLogger{enabled: true, tempDir: t.TempDir()}
	var received []byte
	router := gin.New()
	router.Use(RequestLoggingMiddleware(logger))
	router.POST("/v1/responses", func(c *gin.Context) {
		var errRead error
		received, errRead = io.ReadAll(c.Request.Body)
		if errRead != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if !bytes.Equal(received, compressedBody) {
		t.Fatal("handler did not receive the complete original compressed request body")
	}
	marker := "\n[DECOMPRESSED REQUEST BODY TRUNCATED]"
	if len(logger.requestBody) != int(maxDeferredErrorRequestBodyBytes)+len(marker) {
		t.Fatalf("logged decompressed body length = %d, want %d", len(logger.requestBody), int(maxDeferredErrorRequestBodyBytes)+len(marker))
	}
	if !bytes.Equal(logger.requestBody[:64], bytes.Repeat([]byte{'z'}, 64)) {
		t.Fatal("logged decompressed body did not retain the expected prefix")
	}
	if !bytes.HasSuffix(logger.requestBody, []byte(marker)) {
		t.Fatalf("logged decompressed body missing truncation marker: %q", logger.requestBody[len(logger.requestBody)-len(marker):])
	}
}
