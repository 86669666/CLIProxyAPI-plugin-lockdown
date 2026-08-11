package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubHomeRequestLogClient struct {
	heartbeatOK bool
	pushed      [][]byte
}

func (c *stubHomeRequestLogClient) HeartbeatOK() bool { return c.heartbeatOK }

func (c *stubHomeRequestLogClient) RPushRequestLog(_ context.Context, payload []byte) error {
	c.pushed = append(c.pushed, bytes.Clone(payload))
	return nil
}

func assertFileBodySourceCleaned(t *testing.T, partPaths []string) {
	t.Helper()

	dirs := make(map[string]struct{}, len(partPaths))
	for _, path := range partPaths {
		if _, errStat := os.Stat(path); !os.IsNotExist(errStat) {
			t.Fatalf("expected part %s to be removed, stat err=%v", path, errStat)
		}
		dirs[filepath.Dir(path)] = struct{}{}
	}
	for dir := range dirs {
		if _, errStat := os.Stat(dir); !os.IsNotExist(errStat) {
			t.Fatalf("expected part dir %s to be removed, stat err=%v", dir, errStat)
		}
	}
}

func TestFileBodySource_RecreatesPartDirAfterManualCleanup(t *testing.T) {
	logsDir := t.TempDir()
	source, errSource := NewFileBodySourceInDir(logsDir, "websocket-timeline-test")
	if errSource != nil {
		t.Fatalf("NewFileBodySourceInDir: %v", errSource)
	}
	if errAppend := source.AppendPart([]byte("before manual cleanup")); errAppend != nil {
		t.Fatalf("AppendPart before cleanup: %v", errAppend)
	}
	if errRemove := os.RemoveAll(logsDir); errRemove != nil {
		t.Fatalf("RemoveAll logs dir: %v", errRemove)
	}
	if errAppend := source.AppendPart([]byte("after manual cleanup")); errAppend != nil {
		t.Fatalf("AppendPart after cleanup: %v", errAppend)
	}

	raw, errBytes := source.Bytes()
	if errBytes != nil {
		t.Fatalf("Bytes after cleanup: %v", errBytes)
	}
	if bytes.Contains(raw, []byte("before manual cleanup")) {
		t.Fatalf("expected manually removed part to be skipped, got %q", string(raw))
	}
	if !bytes.Contains(raw, []byte("after manual cleanup")) {
		t.Fatalf("expected recreated part content, got %q", string(raw))
	}

	partPaths := source.Paths()
	if errCleanup := source.Cleanup(); errCleanup != nil {
		t.Fatalf("Cleanup: %v", errCleanup)
	}
	assertFileBodySourceCleaned(t, partPaths)
}

func TestFileRequestLogger_HomeEnabled_ForwardsWhenRequestLogEnabled(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient {
		return stub
	}

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	requestHeaders := map[string][]string{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer secret"},
		"Cookie":        {"session=request-cookie-secret"},
	}

	errLog := logger.LogRequest(
		"/v1/chat/completions",
		http.MethodPost,
		requestHeaders,
		[]byte(`{"input":"hello"}`),
		http.StatusOK,
		map[string][]string{
			"Content-Type": {"application/json"},
			"Set-Cookie":   {"session=response-cookie-secret; HttpOnly"},
		},
		[]byte(`{"ok":true}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		"req-1",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequest error: %v", errLog)
	}

	entries, errRead := os.ReadDir(logsDir)
	if errRead != nil {
		t.Fatalf("failed to read logs dir: %v", errRead)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no local request log files, got entries: %+v", entries)
	}

	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}

	var got struct {
		Headers    map[string][]string `json:"headers"`
		RequestID  string              `json:"request_id"`
		RequestLog string              `json:"request_log"`
	}
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v payload=%s", errUnmarshal, string(stub.pushed[0]))
	}
	if got.Headers == nil || got.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("headers.content-type = %+v, want application/json", got.Headers["Content-Type"])
	}
	if got.Headers == nil || got.Headers["Authorization"][0] != "Bearer se...et" {
		t.Fatalf("headers.authorization = %+v, want redacted authorization", got.Headers["Authorization"])
	}
	if got.RequestID != "req-1" {
		t.Fatalf("request_id = %q, want req-1", got.RequestID)
	}
	if got.RequestLog == "" {
		t.Fatalf("request_log empty, want non-empty")
	}
	for _, secret := range []string{"request-cookie-secret", "response-cookie-secret"} {
		if strings.Contains(got.RequestLog, secret) {
			t.Fatalf("request_log leaked cookie secret %q: %s", secret, got.RequestLog)
		}
	}
	if !strings.Contains(got.RequestLog, "Cookie: "+redactedHeaderValue) || !strings.Contains(got.RequestLog, "Set-Cookie: "+redactedHeaderValue) {
		t.Fatalf("request_log missing redacted cookie headers: %s", got.RequestLog)
	}
}

func TestFileRequestLogger_LogRequestWithSourcesWritesLocalLogAndCleansParts(t *testing.T) {
	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)

	timelineSource, errSource := logger.NewFileBodySource("websocket-timeline-test")
	if errSource != nil {
		t.Fatalf("logger.NewFileBodySource: %v", errSource)
	}
	if errAppend := timelineSource.AppendPart([]byte("Timestamp: 2026-05-25T12:00:00Z\nEvent: websocket.request\n{}")); errAppend != nil {
		t.Fatalf("AppendPart request: %v", errAppend)
	}
	if errAppend := timelineSource.AppendPart([]byte("Timestamp: 2026-05-25T12:00:01Z\nEvent: websocket.response\n{}")); errAppend != nil {
		t.Fatalf("AppendPart response: %v", errAppend)
	}
	partPaths := timelineSource.Paths()
	for _, path := range partPaths {
		if !strings.HasPrefix(path, logsDir+string(os.PathSeparator)) {
			t.Fatalf("part path %s is not under logs dir %s", path, logsDir)
		}
	}

	errLog := logger.LogRequestWithOptionsAndSources(
		"/v1/responses/ws",
		http.MethodGet,
		map[string][]string{"Upgrade": {"websocket"}},
		nil,
		http.StatusSwitchingProtocols,
		map[string][]string{"Upgrade": {"websocket"}},
		nil,
		nil,
		timelineSource,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		"ws-req-1",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequestWithOptionsAndSources error: %v", errLog)
	}

	assertFileBodySourceCleaned(t, partPaths)

	entries, errRead := os.ReadDir(logsDir)
	if errRead != nil {
		t.Fatalf("failed to read logs dir: %v", errRead)
	}
	var logPath string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		logPath = logsDir + string(os.PathSeparator) + entry.Name()
		break
	}
	if logPath == "" {
		t.Fatal("expected local request log file")
	}
	raw, errReadLog := os.ReadFile(logPath)
	if errReadLog != nil {
		t.Fatalf("read log file: %v", errReadLog)
	}
	if !bytes.Contains(raw, []byte("=== WEBSOCKET TIMELINE ===")) {
		t.Fatalf("websocket timeline section missing: %s", string(raw))
	}
	if !bytes.Contains(raw, []byte("Event: websocket.request")) || !bytes.Contains(raw, []byte("Event: websocket.response")) {
		t.Fatalf("merged websocket events missing: %s", string(raw))
	}
}

func TestFileRequestLogger_HomeEnabled_ForwardsSourceLogAndCleansParts(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient {
		return stub
	}

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	timelineSource, errSource := logger.NewFileBodySource("home-websocket-timeline-test")
	if errSource != nil {
		t.Fatalf("logger.NewFileBodySource: %v", errSource)
	}
	if errAppend := timelineSource.AppendPart([]byte("Timestamp: 2026-05-25T12:00:00Z\nEvent: websocket.request\n{}")); errAppend != nil {
		t.Fatalf("AppendPart request: %v", errAppend)
	}
	partPaths := timelineSource.Paths()
	for _, path := range partPaths {
		if !strings.HasPrefix(path, logsDir+string(os.PathSeparator)) {
			t.Fatalf("part path %s is not under logs dir %s", path, logsDir)
		}
	}

	errLog := logger.LogRequestWithOptionsAndSources(
		"/v1/responses/ws",
		http.MethodGet,
		map[string][]string{"Upgrade": {"websocket"}},
		nil,
		http.StatusSwitchingProtocols,
		map[string][]string{"Upgrade": {"websocket"}},
		nil,
		nil,
		timelineSource,
		nil,
		nil,
		nil,
		nil,
		nil,
		false,
		"home-ws-req-1",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequestWithOptionsAndSources error: %v", errLog)
	}
	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}

	var got struct {
		RequestID  string `json:"request_id"`
		RequestLog string `json:"request_log"`
	}
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v payload=%s", errUnmarshal, string(stub.pushed[0]))
	}
	if got.RequestID != "home-ws-req-1" {
		t.Fatalf("request_id = %q, want home-ws-req-1", got.RequestID)
	}
	if !strings.Contains(got.RequestLog, "Event: websocket.request") {
		t.Fatalf("forwarded request_log missing websocket request: %s", got.RequestLog)
	}
	assertFileBodySourceCleaned(t, partPaths)
}

func TestFileRequestLogger_HomeEnabled_ForwardsStreamingRequestID(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient {
		return stub
	}

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	writer, errLog := logger.LogStreamingRequest(
		"/v1/responses",
		http.MethodPost,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"input":"hello"}`),
		"stream-req-1",
	)
	if errLog != nil {
		t.Fatalf("LogStreamingRequest error: %v", errLog)
	}

	if errStatus := writer.WriteStatus(http.StatusOK, map[string][]string{"Content-Type": {"text/event-stream"}}); errStatus != nil {
		t.Fatalf("WriteStatus error: %v", errStatus)
	}
	writer.WriteChunkAsync([]byte("data: ok\n\n"))
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("Close error: %v", errClose)
	}

	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}

	var got struct {
		RequestID  string `json:"request_id"`
		RequestLog string `json:"request_log"`
	}
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v payload=%s", errUnmarshal, string(stub.pushed[0]))
	}
	if got.RequestID != "stream-req-1" {
		t.Fatalf("request_id = %q, want stream-req-1", got.RequestID)
	}
	if got.RequestLog == "" {
		t.Fatalf("request_log empty, want non-empty")
	}
}

func TestFileRequestLogger_HomeEnabled_DoesNotForwardForcedErrorLogsWhenRequestLogDisabled(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() {
		currentHomeRequestLogClient = original
	}()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient {
		return stub
	}

	logsDir := t.TempDir()
	logger := NewFileRequestLogger(false, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	errLog := logger.LogRequestWithOptions(
		"/v1/chat/completions",
		http.MethodPost,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"input":"hello"}`),
		http.StatusBadGateway,
		map[string][]string{"Content-Type": {"application/json"}},
		[]byte(`{"error":"upstream failure"}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		true,
		"req-2",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequestWithOptions error: %v", errLog)
	}

	if len(stub.pushed) != 0 {
		t.Fatalf("home pushed records = %d, want 0", len(stub.pushed))
	}

	entries, errRead := os.ReadDir(logsDir)
	if errRead != nil {
		t.Fatalf("failed to read logs dir: %v", errRead)
	}
	found := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected local forced error log file when request-log disabled")
	}
}

func TestRedactHeadersSensitiveVariants(t *testing.T) {
	input := map[string][]string{
		"Authorization": {"Bearer super-secret-token"}, "Proxy-Authorization": {"Basic credential"},
		"Cookie": {"session=secret"}, "Set-Cookie": {"session=secret"}, "X-API-Key": {"abcdefghijk"},
		"Api-Key": {"abcdefghijk"}, "X-Access-Token": {"abcdefghijk"}, "Client-Secret": {"abcdefghijk"},
		"Content-Type": {"application/json"},
	}
	got := RedactHeaders(input)
	for key, values := range got {
		if key == "Content-Type" {
			continue
		}
		if len(values) == 0 || values[0] == input[key][0] {
			t.Fatalf("%s was not redacted: %v", key, values)
		}
	}
	if got["Content-Type"][0] != "application/json" {
		t.Fatalf("non-sensitive header changed: %v", got["Content-Type"])
	}
	if input["Cookie"][0] != "session=secret" {
		t.Fatal("input headers mutated")
	}
}

func TestFileRequestLogger_HomeEnabled_BoundsAllNonStreamingSectionsWithoutMutatingInputs(t *testing.T) {
	originalClient := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = originalClient }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }
	logger := NewFileRequestLogger(true, t.TempDir(), "", 0)
	logger.SetHomeEnabled(true)

	requestBody := bytes.Repeat([]byte("q"), homeRequestBodyMaxBytes+257)
	responseBody := bytes.Repeat([]byte("s"), homeStreamingResponseBodyMaxBytes+257)
	apiRequest := bytes.Repeat([]byte("a"), homeAPIRequestMaxBytes+257)
	apiResponse := bytes.Repeat([]byte("b"), homeAPIResponseMaxBytes+257)
	apiTimeline := bytes.Repeat([]byte("t"), homeAPIWebsocketTimelineMaxBytes+257)
	requestBodyBefore := bytes.Clone(requestBody)
	responseBodyBefore := bytes.Clone(responseBody)
	apiRequestBefore := bytes.Clone(apiRequest)
	apiResponseBefore := bytes.Clone(apiResponse)
	apiTimelineBefore := bytes.Clone(apiTimeline)

	errLog := logger.LogRequest(
		"/v1/responses",
		http.MethodPost,
		map[string][]string{"Authorization": {"Bearer secret"}},
		requestBody,
		http.StatusOK,
		map[string][]string{"Set-Cookie": {"session=secret"}},
		responseBody,
		nil,
		apiRequest,
		apiResponse,
		apiTimeline,
		nil,
		"bounded-non-streaming",
		time.Now(),
		time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequest error: %v", errLog)
	}
	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}
	if len(stub.pushed[0]) > homeRequestLogPayloadMaxBytes {
		t.Fatalf("home payload length = %d, want <= %d", len(stub.pushed[0]), homeRequestLogPayloadMaxBytes)
	}

	var got homeRequestLogPayload
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v", errUnmarshal)
	}
	if len(got.RequestLog) > homeRequestLogTextMaxBytes {
		t.Fatalf("request log length = %d, want <= %d", len(got.RequestLog), homeRequestLogTextMaxBytes)
	}
	for _, marker := range []string{
		homeRequestBodyTruncatedMarker,
		homeResponseBodyTruncatedMarker,
		homeAPIRequestTruncatedMarker,
		homeAPIResponseTruncatedMarker,
		homeAPIWebsocketTruncatedMarker,
	} {
		if !strings.Contains(got.RequestLog, marker) {
			t.Fatalf("request log missing marker %q", marker)
		}
	}
	if strings.Contains(got.RequestLog, "Bearer secret") || strings.Contains(got.RequestLog, "session=secret") {
		t.Fatal("request log leaked redacted header data")
	}
	for name, pair := range map[string][2][]byte{
		"request body":  {requestBody, requestBodyBefore},
		"response body": {responseBody, responseBodyBefore},
		"API request":   {apiRequest, apiRequestBefore},
		"API response":  {apiResponse, apiResponseBefore},
		"API timeline":  {apiTimeline, apiTimelineBefore},
	} {
		if !bytes.Equal(pair[0], pair[1]) {
			t.Fatalf("%s input was modified", name)
		}
	}
}

func TestHomeStreamingLogWriter_BoundsConstructionFieldsWithoutMutatingInputs(t *testing.T) {
	originalClient := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = originalClient }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }

	requestBody := bytes.Repeat([]byte("r"), homeRequestBodyMaxBytes+257)
	apiRequest := bytes.Repeat([]byte("q"), homeAPIRequestMaxBytes+257)
	apiResponse := bytes.Repeat([]byte("p"), homeAPIResponseMaxBytes+257)
	apiTimeline := bytes.Repeat([]byte("w"), homeAPIWebsocketTimelineMaxBytes+257)
	requestBodyBefore := bytes.Clone(requestBody)
	apiRequestBefore := bytes.Clone(apiRequest)
	apiResponseBefore := bytes.Clone(apiResponse)
	apiTimelineBefore := bytes.Clone(apiTimeline)

	writer := newHomeStreamingLogWriter("/v1/responses", http.MethodPost, nil, requestBody, "bounded-construction")
	if errWrite := writer.WriteAPIRequest(apiRequest); errWrite != nil {
		t.Fatalf("WriteAPIRequest error: %v", errWrite)
	}
	if errWrite := writer.WriteAPIResponse(apiResponse); errWrite != nil {
		t.Fatalf("WriteAPIResponse error: %v", errWrite)
	}
	if errWrite := writer.WriteAPIWebsocketTimeline(apiTimeline); errWrite != nil {
		t.Fatalf("WriteAPIWebsocketTimeline error: %v", errWrite)
	}

	for name, bounded := range map[string]struct {
		payload []byte
		max     int
		marker  string
	}{
		"request body": {writer.requestBody, homeRequestBodyMaxBytes, homeRequestBodyTruncatedMarker},
		"API request":  {writer.apiRequest, homeAPIRequestMaxBytes, homeAPIRequestTruncatedMarker},
		"API response": {writer.apiResponse, homeAPIResponseMaxBytes, homeAPIResponseTruncatedMarker},
		"API timeline": {writer.apiWebsocketTime, homeAPIWebsocketTimelineMaxBytes, homeAPIWebsocketTruncatedMarker},
	} {
		if len(bounded.payload) > bounded.max {
			t.Fatalf("%s length = %d, want <= %d", name, len(bounded.payload), bounded.max)
		}
		if !bytes.Contains(bounded.payload, []byte(bounded.marker)) {
			t.Fatalf("%s missing marker %q", name, bounded.marker)
		}
	}
	for name, pair := range map[string][2][]byte{
		"request body": {requestBody, requestBodyBefore},
		"API request":  {apiRequest, apiRequestBefore},
		"API response": {apiResponse, apiResponseBefore},
		"API timeline": {apiTimeline, apiTimelineBefore},
	} {
		if !bytes.Equal(pair[0], pair[1]) {
			t.Fatalf("%s input was modified", name)
		}
	}

	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("Close error: %v", errClose)
	}
	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}
	if len(stub.pushed[0]) > homeRequestLogPayloadMaxBytes {
		t.Fatalf("home payload length = %d, want <= %d", len(stub.pushed[0]), homeRequestLogPayloadMaxBytes)
	}
}

func TestFileRequestLogger_HomeEnabled_BoundsHugeFileBackedPartSets(t *testing.T) {
	originalClient := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = originalClient }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }
	logsDir := t.TempDir()
	logger := NewFileRequestLogger(true, logsDir, "", 0)
	logger.SetHomeEnabled(true)

	newFloodedSource := func(prefix string, content []byte) (*FileBodySource, string) {
		t.Helper()
		source, errSource := logger.NewFileBodySource(prefix)
		if errSource != nil {
			t.Fatalf("NewFileBodySource(%s): %v", prefix, errSource)
		}
		file, errCreate := source.CreatePart("flood")
		if errCreate != nil {
			t.Fatalf("CreatePart(%s): %v", prefix, errCreate)
		}
		if len(content) > 0 {
			if _, errWrite := file.Write(content); errWrite != nil {
				t.Fatalf("write part(%s): %v", prefix, errWrite)
			}
		}
		if errClose := file.Close(); errClose != nil {
			t.Fatalf("close part(%s): %v", prefix, errClose)
		}
		path := file.Name()
		source.mu.Lock()
		source.paths = make([]string, 100_000)
		for index := range source.paths {
			source.paths[index] = path
		}
		source.mu.Unlock()
		return source, path
	}

	websocketSource, websocketPath := newFloodedSource("websocket", nil)
	apiRequestSource, apiRequestPath := newFloodedSource("api-request", []byte("small-api-request"))
	apiResponseSource, apiResponsePath := newFloodedSource("api-response", nil)
	apiTimelineSource, apiTimelinePath := newFloodedSource("api-timeline", []byte("small-api-timeline"))

	errLog := logger.LogRequestWithOptionsAndAllSources(
		"/v1/responses", http.MethodPost, nil, nil, http.StatusOK, nil, nil,
		nil, websocketSource, nil, apiRequestSource, nil, apiResponseSource, nil, apiTimelineSource,
		nil, false, "huge-file-parts", time.Now(), time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequestWithOptionsAndAllSources error: %v", errLog)
	}
	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}
	if len(stub.pushed[0]) > homeRequestLogPayloadMaxBytes {
		t.Fatalf("home payload length = %d, want <= %d", len(stub.pushed[0]), homeRequestLogPayloadMaxBytes)
	}

	var got homeRequestLogPayload
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v", errUnmarshal)
	}
	for _, marker := range []string{
		homeWebsocketTruncatedMarker,
		homeAPIRequestTruncatedMarker,
		homeAPIResponseTruncatedMarker,
		homeAPIWebsocketTruncatedMarker,
	} {
		if !strings.Contains(got.RequestLog, marker) {
			t.Fatalf("request log missing marker %q", marker)
		}
	}
	if strings.Count(got.RequestLog, "small-api-request") > homeFileSectionMaxParts {
		t.Fatal("API request source read more than the part limit")
	}
	if strings.Count(got.RequestLog, "small-api-timeline") > homeFileSectionMaxParts {
		t.Fatal("API timeline source read more than the part limit")
	}
	assertFileBodySourceCleaned(t, []string{websocketPath, apiRequestPath, apiResponsePath, apiTimelinePath})
}

func TestFileRequestLogger_HomeEnabled_BoundsHeadersBeforeFormattingAndMarshal(t *testing.T) {
	originalClient := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = originalClient }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }
	logger := NewFileRequestLogger(true, t.TempDir(), "", 0)
	logger.SetHomeEnabled(true)

	requestSecret := strings.Repeat("request-secret-", homeHeaderValueMaxBytes)
	responseSecret := strings.Repeat("response-secret-", homeHeaderValueMaxBytes)
	requestHeaders := map[string][]string{
		"Authorization": {"Bearer " + requestSecret},
		"Cookie":        {"session=" + requestSecret},
		"X-Many-Values": make([]string, 10_000),
	}
	for index := range requestHeaders["X-Many-Values"] {
		requestHeaders["X-Many-Values"][index] = fmt.Sprintf("value-%d", index)
	}
	responseHeaders := map[string][]string{
		"Set-Cookie": {"session=" + responseSecret},
	}
	for index := 0; index < 1_000; index++ {
		requestHeaders[fmt.Sprintf("X-Secret-Request-%05d", index)] = []string{fmt.Sprintf("count-secret-request-%05d", index)}
		responseHeaders[fmt.Sprintf("X-Secret-Response-%05d", index)] = []string{fmt.Sprintf("count-secret-response-%05d", index)}
	}
	requestBefore := cloneHeaders(requestHeaders)
	responseBefore := cloneHeaders(responseHeaders)

	errLog := logger.LogRequest(
		"/v1/chat/completions", http.MethodPost, requestHeaders, nil, http.StatusOK,
		responseHeaders, nil, nil, nil, nil, nil, nil, "huge-headers", time.Now(), time.Now(),
	)
	if errLog != nil {
		t.Fatalf("LogRequest error: %v", errLog)
	}
	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}
	if len(stub.pushed[0]) > homeRequestLogPayloadMaxBytes {
		t.Fatalf("home payload length = %d, want <= %d", len(stub.pushed[0]), homeRequestLogPayloadMaxBytes)
	}

	var got homeRequestLogPayload
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v", errUnmarshal)
	}
	if got.Headers[homeHeadersTruncatedKey][0] != homeHeadersTruncatedMarker {
		t.Fatalf("structured headers missing truncation marker: %#v", got.Headers[homeHeadersTruncatedKey])
	}
	if !strings.Contains(got.RequestLog, homeHeadersTruncatedKey+": "+homeHeadersTruncatedMarker) {
		t.Fatal("text request log missing header truncation marker")
	}
	if len(got.Headers) > homeHeaderMaxKeys+1 {
		t.Fatalf("structured header count = %d, want <= %d", len(got.Headers), homeHeaderMaxKeys+1)
	}
	totalBytes := 0
	for key, values := range got.Headers {
		if len(key) > homeHeaderKeyMaxBytes {
			t.Fatalf("header key length = %d, want <= %d", len(key), homeHeaderKeyMaxBytes)
		}
		for _, value := range values {
			if len(value) > homeHeaderValueMaxBytes {
				t.Fatalf("header value length = %d, want <= %d", len(value), homeHeaderValueMaxBytes)
			}
			totalBytes += len(key) + len(value)
		}
	}
	if totalBytes > homeHeadersMaxBytes {
		t.Fatalf("structured header bytes = %d, want <= %d", totalBytes, homeHeadersMaxBytes)
	}
	for _, secret := range []string{
		requestSecret[:128],
		responseSecret[:128],
		"session=" + requestSecret[:128],
		"session=" + responseSecret[:128],
		"count-secret-request-",
		"count-secret-response-",
	} {
		if strings.Contains(string(stub.pushed[0]), secret) {
			t.Fatal("Home payload leaked an original sensitive header value")
		}
	}
	if !headersEqual(requestHeaders, requestBefore) {
		t.Fatal("request headers were modified")
	}
	if !headersEqual(responseHeaders, responseBefore) {
		t.Fatal("response headers were modified")
	}
}

func headersEqual(left, right map[string][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValues := range left {
		rightValues, ok := right[key]
		if !ok || len(leftValues) != len(rightValues) {
			return false
		}
		for index := range leftValues {
			if leftValues[index] != rightValues[index] {
				return false
			}
		}
	}
	return true
}

func TestFileRequestLogger_ForwardRequestLogToHomeUsesBoundedMarshal(t *testing.T) {
	originalClient := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = originalClient }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }
	logger := NewFileRequestLogger(true, t.TempDir(), "", 0)
	logger.SetHomeEnabled(true)

	logBytes := bytes.Repeat([]byte{0x01}, homeRequestLogTextMaxBytes+257)
	logBefore := bytes.Clone(logBytes)
	if errForward := logger.forwardRequestLogToHome(context.Background(), nil, "bounded-forward", string(logBytes)); errForward != nil {
		t.Fatalf("forwardRequestLogToHome error: %v", errForward)
	}
	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}
	if len(stub.pushed[0]) > homeRequestLogPayloadMaxBytes {
		t.Fatalf("home payload length = %d, want <= %d", len(stub.pushed[0]), homeRequestLogPayloadMaxBytes)
	}
	var got homeRequestLogPayload
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v", errUnmarshal)
	}
	if !strings.Contains(got.RequestLog, homeRequestLogTruncatedMarker) {
		t.Fatalf("request log missing marker %q", homeRequestLogTruncatedMarker)
	}
	if !bytes.Equal(logBytes, logBefore) {
		t.Fatal("request log input was modified")
	}
}

func TestHomeStreamingLogWriter_BoundsResponseBodyAndMarksTruncated(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = original }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }

	writer := newHomeStreamingLogWriter("/v1/responses", http.MethodPost, nil, nil, "bounded-1")
	writer.WriteStatus(http.StatusOK, map[string][]string{"Content-Type": {"text/event-stream"}})
	writer.WriteChunkAsync(bytes.Repeat([]byte{0x7f}, homeStreamingResponseBodyMaxBytes+1))
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("Close error: %v", errClose)
	}

	if len(stub.pushed) != 1 {
		t.Fatalf("home pushed records = %d, want 1", len(stub.pushed))
	}
	if len(stub.pushed[0]) > homeRequestLogPayloadMaxBytes {
		t.Fatalf("home payload length = %d, want <= %d", len(stub.pushed[0]), homeRequestLogPayloadMaxBytes)
	}

	var got homeRequestLogPayload
	if errUnmarshal := json.Unmarshal(stub.pushed[0], &got); errUnmarshal != nil {
		t.Fatalf("unmarshal payload: %v", errUnmarshal)
	}
	if strings.Count(got.RequestLog, string([]byte{0x7f})) != homeStreamingResponseBodyMaxBytes-len(homeResponseBodyTruncatedMarker) {
		t.Fatalf("response body bytes = %d, want %d", strings.Count(got.RequestLog, string([]byte{0x7f})), homeStreamingResponseBodyMaxBytes-len(homeResponseBodyTruncatedMarker))
	}
	if !strings.Contains(got.RequestLog, homeResponseBodyTruncatedMarker) {
		t.Fatalf("request log missing %q marker", homeResponseBodyTruncatedMarker)
	}
	if writer.responseBody.Cap() != 0 {
		t.Fatalf("response body capacity = %d, want 0 after Close", writer.responseBody.Cap())
	}
}

func TestHomeStreamingLogWriter_CloseHeartbeatFailureDrainsAndReleases(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = original }()

	stub := &stubHomeRequestLogClient{heartbeatOK: true}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }
	writer := newHomeStreamingLogWriter("/v1/responses", http.MethodPost, nil, nil, "heartbeat-1")
	writer.WriteChunkAsync([]byte("queued"))
	stub.heartbeatOK = false

	closeDone := make(chan struct{})
	go func() {
		if errClose := writer.Close(); errClose != nil {
			t.Errorf("Close error: %v", errClose)
		}
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close blocked after heartbeat failure")
	}

	select {
	case <-writer.doneChan:
	default:
		t.Fatal("writer goroutine is still running")
	}
	if writer.chunkChan != nil {
		t.Fatal("chunk channel was not released")
	}
	if writer.responseBody.Cap() != 0 {
		t.Fatalf("response body capacity = %d, want 0 after Close", writer.responseBody.Cap())
	}
	if len(stub.pushed) != 0 {
		t.Fatalf("home pushed records = %d, want 0", len(stub.pushed))
	}
}

func TestHomeStreamingLogWriter_ConcurrentCloseIsSafe(t *testing.T) {
	original := currentHomeRequestLogClient
	defer func() { currentHomeRequestLogClient = original }()

	stub := &stubHomeRequestLogClient{heartbeatOK: false}
	currentHomeRequestLogClient = func() homeRequestLogClient { return stub }
	writer := newHomeStreamingLogWriter("/v1/responses", http.MethodPost, nil, nil, "concurrent-1")

	const closeCount = 32
	closeDone := make(chan struct{}, closeCount)
	for i := 0; i < closeCount; i++ {
		go func() {
			writer.WriteChunkAsync([]byte("concurrent"))
			if errClose := writer.Close(); errClose != nil {
				t.Errorf("Close error: %v", errClose)
			}
			closeDone <- struct{}{}
		}()
	}

	for i := 0; i < closeCount; i++ {
		select {
		case <-closeDone:
		case <-time.After(time.Second):
			t.Fatal("concurrent Close blocked")
		}
	}
	if writer.chunkChan != nil {
		t.Fatal("chunk channel was not released")
	}
}
