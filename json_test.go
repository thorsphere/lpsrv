// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv_test

// Import packages
import (
	"io"                // Import the io package for a simulated response write error.
	"net/http"          // Import the net/http package for requests and status codes.
	"net/http/httptest" // Import the net/http/httptest package for socket-free requests and recorders.
	"strings"           // Import the strings package for in-memory request bodies.
	"testing"           // Import the testing package for table-driven tests.

	"github.com/thorsphere/lpsrv" // Import the lpsrv package under test.
	"github.com/thorsphere/tserr" // Import the tserr package for assertion errors.
)

// TestDecodeJSON verifies strict decoding, request body limits, and failure results.
func TestDecodeJSON(t *testing.T) {
	// Define a payload type that lets the decoder distinguish known and unknown fields.
	type payload struct {
		Name string `json:"name"` // The name decoded from the JSON object.
	}
	// Define the valid request body and its exact byte length.
	const (
		validBody = `{"name":"Ada"}`      // A complete JSON object with a known field.
		bodyBytes = int64(len(validBody)) // The byte length of validBody.
	)
	// Define cases for successful decoding, invalid input, and both size-error paths.
	tests := []struct {
		name       string  // The subtest name.
		body       string  // The request body content.
		maxBytes   int64   // The byte limit passed to DecodeJSON.
		nilRequest bool    // Whether to pass a nil request.
		nilBody    bool    // Whether to clear the request body.
		want       payload // The expected decoded value.
		wantStatus int     // The expected HTTP status, or zero on success.
	}{
		{name: "valid object at exact limit", body: validBody, maxBytes: bodyBytes, want: payload{Name: "Ada"}},
		{name: "trailing whitespace within limit", body: validBody + " \n\t", maxBytes: bodyBytes + 3, want: payload{Name: "Ada"}},
		{name: "zero limit", body: validBody, maxBytes: 0, wantStatus: http.StatusInternalServerError},
		{name: "negative limit", body: validBody, maxBytes: -1, wantStatus: http.StatusInternalServerError},
		{name: "nil request", nilRequest: true, maxBytes: bodyBytes, wantStatus: http.StatusBadRequest},
		{name: "nil body", nilBody: true, maxBytes: bodyBytes, wantStatus: http.StatusBadRequest},
		{name: "empty body", maxBytes: bodyBytes, wantStatus: http.StatusBadRequest},
		{name: "malformed JSON", body: `{"name":}`, maxBytes: bodyBytes, wantStatus: http.StatusBadRequest},
		{name: "wrong field type", body: `{"name":42}`, maxBytes: bodyBytes, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: `{"name":"Ada","extra":true}`, maxBytes: 64, wantStatus: http.StatusBadRequest},
		{name: "second JSON value", body: validBody + ` {}`, maxBytes: bodyBytes + 3, wantStatus: http.StatusBadRequest},
		{name: "trailing invalid data", body: validBody + ` !`, maxBytes: bodyBytes + 2, wantStatus: http.StatusBadRequest},
		{name: "object exceeds limit", body: validBody, maxBytes: bodyBytes - 1, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "trailing whitespace exceeds limit", body: validBody + " ", maxBytes: bodyBytes, wantStatus: http.StatusRequestEntityTooLarge},
	}
	// Run the cases with independent requests and recorders.
	for _, tt := range tests {
		// Check a single request's decoded value, status, and error.
		t.Run(tt.name, func(t *testing.T) {
			// Retain the reader so invalid limits can be checked for body consumption.
			body := strings.NewReader(tt.body)
			// Create an in-memory request with the case's body content.
			r := httptest.NewRequest(http.MethodPost, "/", body)
			// Replace the request with nil when the case calls for it.
			if tt.nilRequest {
				// Pass no request to the decoder.
				r = nil
			} else if tt.nilBody {
				// Pass a request without a body stream.
				r.Body = nil
			}
			// Decode the case's request using its configured limit.
			got, status, err := lpsrv.DecodeJSON[payload](httptest.NewRecorder(), r, tt.maxBytes)
			// Compare the entire payload, including that failures return its zero value.
			if got != tt.want || status != tt.wantStatus {
				// Report the actual and expected payload and status.
				t.Errorf("DecodeJSON returned (%+v, %d), want (%+v, %d)", got, status, tt.want, tt.wantStatus)
			}
			// Require a non-nil error for failure cases.
			if tt.wantStatus != 0 && err == nil {
				// Report the missing error.
				t.Error(tserr.NilFailed("DecodeJSON"))
			}
			// Require a nil error for success cases.
			if tt.wantStatus == 0 && err != nil {
				// Report the unexpected error.
				t.Error(tserr.NilExpected(&tserr.NilExpectedArgs{Op: "DecodeJSON", Err: err}))
			}
			// Check that nonpositive limits are rejected before any body bytes are read.
			if tt.maxBytes <= 0 && body.Len() != len(tt.body) {
				// Report the number of bytes unexpectedly read.
				t.Errorf("DecodeJSON read %d body bytes with a nonpositive limit", len(tt.body)-body.Len())
			}
		})
	}
}

// failingJSONWriter records response headers but rejects every body write.
type failingJSONWriter struct {
	*httptest.ResponseRecorder     // The recorder that receives the response headers and status.
	writes                     int // The number of attempted body writes.
}

// Write rejects p without recording any body bytes.
func (w *failingJSONWriter) Write(p []byte) (int, error) {
	// Record each attempt to write a response body.
	w.writes++
	// Return a write error without accepting any bytes.
	return 0, io.ErrClosedPipe
}

// TestWriteJSON verifies the status, committed content type, and body of JSON responses.
func TestWriteJSON(t *testing.T) {
	// Define cases for normal responses, default statuses, and failure paths.
	tests := []struct {
		name       string // The subtest name.
		method     string // The request method, or empty for a nil request.
		status     int    // The status supplied to WriteJSON.
		payload    any    // The value supplied to WriteJSON for marshaling.
		failWrite  bool   // Whether the writer should reject body writes.
		wantStatus int    // The expected committed HTTP status.
		wantType   string // The expected committed Content-Type header.
		wantBody   string // The expected response body.
	}{
		{name: "explicit status", method: http.MethodPost, status: http.StatusCreated, payload: map[string]string{"name": "Ada"}, wantStatus: http.StatusCreated, wantType: "application/json", wantBody: `{"name":"Ada"}`},
		{name: "zero status", method: http.MethodGet, payload: []int{1, 2}, wantStatus: http.StatusOK, wantType: "application/json", wantBody: `[1,2]`},
		{name: "negative status and nil request", status: -1, wantStatus: http.StatusOK, wantType: "application/json", wantBody: `null`},
		{name: "HEAD omits body", method: http.MethodHead, status: http.StatusAccepted, payload: "Ada", wantStatus: http.StatusAccepted, wantType: "application/json"},
		{name: "marshal failure replaces success with sanitized error", method: http.MethodGet, status: http.StatusCreated, payload: func() {}, wantStatus: http.StatusInternalServerError, wantType: "text/plain; charset=utf-8", wantBody: "Internal Server Error\n"},
		{name: "write failure preserves committed status", method: http.MethodGet, status: http.StatusAccepted, payload: "Ada", failWrite: true, wantStatus: http.StatusAccepted, wantType: "application/json"},
	}
	// Run each case with an independent response recorder.
	for _, tt := range tests {
		// Verify the response produced for this case.
		t.Run(tt.name, func(t *testing.T) {
			// Leave the request nil unless the case specifies a method.
			var r *http.Request
			// Create an in-memory request when a method is specified.
			if tt.method != "" {
				// Create the request without opening a socket.
				r = httptest.NewRequest(tt.method, "/widgets", nil)
			}
			// Create a recorder to capture the response.
			recorder := httptest.NewRecorder()
			// Wrap the recorder for cases that simulate a write failure.
			failingWriter := &failingJSONWriter{ResponseRecorder: recorder}
			// Use the recorder directly by default.
			var w http.ResponseWriter = recorder
			// Switch writers when the case requires a failing body write.
			if tt.failWrite {
				// Use the writer that fails while preserving the recorded headers.
				w = failingWriter
			}
			// Write the response using the supplied request, status, and payload.
			lpsrv.WriteJSON(w, r, tt.status, tt.payload)
			// Read the status and headers as committed to the client.
			result := recorder.Result()
			// Verify that the intended status was committed.
			if result.StatusCode != tt.wantStatus {
				// Report the unexpected status.
				t.Errorf("status = %d, want %d", result.StatusCode, tt.wantStatus)
			}
			// Verify that the correct content type was set before committing headers.
			if got := result.Header.Get("Content-Type"); got != tt.wantType {
				// Report the unexpected content type.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "Content-Type", Actual: got, Want: tt.wantType}))
			}
			// Verify the exact body, including sanitized errors and empty responses.
			if got := recorder.Body.String(); got != tt.wantBody {
				// Report the unexpected response body.
				t.Error(tserr.EqualStr(&tserr.EqualStrArgs{Var: "response body", Actual: got, Want: tt.wantBody}))
			}
			// Verify the write failure was encountered without a second body write.
			if tt.failWrite && failingWriter.writes != 1 {
				// Report the unexpected number of write attempts.
				t.Errorf("body write attempts = %d, want 1", failingWriter.writes)
			}
		})
	}
}
