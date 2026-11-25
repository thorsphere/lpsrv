// Copyright (c) 2026 thorsphere.
// All Rights Reserved. Use is governed by the Functional Source License v1.1
// (FSL-1.1-ALv2) that can be found in the LICENSE file.

package lpsrv

// Import packages
import (
	"encoding/json" // Import the encoding/json package for JSON decoding and buffered encoding.
	"errors"        // Import the errors package for error matching and construction.
	"fmt"           // Import the fmt package for status validation diagnostics.
	"io"            // Import the io package for EOF checks on trailing JSON tokens.
	"net/http"      // Import the net/http package for body size limits and HTTP status codes.

	"github.com/thorsphere/tserr" // Import the tserr package for structured error construction.
	"github.com/thorsphere/tslog" // Import the tslog package for structured logging.
)

// jsonContentType is the HTTP Content-Type header value written on JSON responses.
const jsonContentType = "application/json"

// DecodeJSON returns the single JSON value of type T decoded from the body of
// r, enforcing a positive maxBytes limit via w and rejecting unknown fields
// and trailing data. On success it returns a zero status code and nil error.
// On failure it returns the zero value of T and a structured error, with status
// 400 for invalid request bodies, 413 for oversized bodies, or 500 if maxBytes
// is zero or negative.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, int, error) {
	// Prepare the zero value of T for error returns.
	var zero T
	// Validate the caller-supplied limit before accessing the request body.
	if maxBytes <= 0 {
		// Report the nonpositive limit as a server configuration error.
		return zero, http.StatusInternalServerError, tserr.Higher(&tserr.HigherArgs{
			Var:        "maxBytes",
			Actual:     maxBytes,
			LowerBound: 1,
		})
	}
	// Reject nil requests or requests carrying a nil body stream.
	if r == nil || r.Body == nil {
		// Return 400 Bad Request when no request body is available.
		return zero, http.StatusBadRequest, tserr.Empty("request body")
	}
	// Bound the request body size so oversized payloads are rejected without buffering.
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	// Build a streaming JSON decoder over the size-bounded body.
	dec := json.NewDecoder(body)
	// Reject unknown JSON object fields so misspelled keys fail fast.
	dec.DisallowUnknownFields()
	// Prepare the destination variable for the decoded JSON payload.
	var payload T
	// Decode the primary JSON value from the bounded request body.
	if err := dec.Decode(&payload); err != nil {
		// Prepare to match an oversized-body error from MaxBytesReader.
		var maxErr *http.MaxBytesError
		// Check whether reading the payload exceeded maxBytes.
		if errors.As(err, &maxErr) {
			// Reject oversized payloads with 413 Request Entity Too Large.
			return zero, http.StatusRequestEntityTooLarge, tserr.TooLarge(&tserr.TooLargeArgs{
				F:        "request body",
				Actual:   maxErr.Limit + 1,
				MaxBytes: maxErr.Limit,
			})
		}
		// Reject malformed or empty JSON bodies with 400 Bad Request.
		return zero, http.StatusBadRequest, tserr.InvalidJson(err)
	}
	// Check that no trailing data follows the single JSON value.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		// Prepare to match an oversized-body error encountered while scanning trailing bytes.
		var maxErr *http.MaxBytesError
		// Check whether trailing whitespace or data pushed the body past maxBytes.
		if errors.As(err, &maxErr) {
			// Reject oversized payloads with 413 Request Entity Too Large.
			return zero, http.StatusRequestEntityTooLarge, tserr.TooLarge(&tserr.TooLargeArgs{
				F:        "request body",
				Actual:   maxErr.Limit + 1,
				MaxBytes: maxErr.Limit,
			})
		}
		// Reject trailing JSON tokens with 400 Bad Request.
		return zero, http.StatusBadRequest, tserr.InvalidJson(errors.New("unexpected trailing data"))
	}
	// Return the validated payload, zero status, and nil error to indicate success.
	return payload, 0, nil
}

// WriteJSON marshals payload in memory before writing a JSON response with
// status to w. It requires a non-nil r and a final HTTP status (200–599).
// For statuses 204, 205, and 304, it writes only the status and ignores payload.
// Invalid arguments or marshaling failures are logged and produce a sanitized
// 500 Internal Server Error. Responses to HEAD requests omit the body.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	// Reject a missing request before inspecting the method or marshaling payload.
	if r == nil {
		// Log the caller error and send a sanitized response.
		writeInternalError(w, tserr.NilParam("request"))
		// Stop before attempting to encode the payload.
		return
	}
	// Determine whether the response must omit its body.
	head := r.Method == http.MethodHead
	// Reject statuses outside the final HTTP response range.
	if status < http.StatusOK || status > 599 {
		// Log the invalid status and send a sanitized response.
		writeJSONInternalError(w, head, tserr.Op(&tserr.OpArgs{
			Op:  "validate response status",
			Fn:  requestPath(r),
			Err: fmt.Errorf("expected a final HTTP status (200–599), got %d", status),
		}))
		// Stop before attempting to encode the payload.
		return
	}
	// Skip marshaling and body writes for statuses that prohibit content.
	switch status {
	case http.StatusNoContent, http.StatusResetContent, http.StatusNotModified:
		// Commit the status without adding a JSON Content-Type header.
		w.WriteHeader(status)
		// Stop before processing the payload.
		return
	}
	// Marshal payload in memory before sending headers so encoding failures can return 500.
	jsonBytes, err := json.Marshal(payload)
	// If JSON encoding fails, log the error and respond with a sanitized 500 status.
	if err != nil {
		// Write a sanitized 500 Internal Server Error before committing the supplied status.
		writeJSONInternalError(w, head, tserr.Op(&tserr.OpArgs{Op: "json.Marshal", Fn: requestPath(r), Err: err}))
		// Stop processing after writing the error response.
		return
	}
	// Set the JSON Content-Type header before writing the status line.
	w.Header().Set(contentTypeHeader, jsonContentType)
	// Write the HTTP status code header.
	w.WriteHeader(status)
	// Omit the response body on HEAD requests per RFC 9110 semantics.
	if head {
		// Return without writing body bytes on HEAD requests.
		return
	}
	// Write the buffered JSON response body and log any write failure.
	if _, err := w.Write(jsonBytes); err != nil {
		// Log the response write error for diagnostics after headers have been sent.
		tslog.Error(tserr.Op(&tserr.OpArgs{Op: "write response", Fn: requestPath(r), Err: err}))
	}
}

// writeJSONInternalError logs err and sends a sanitized 500 response, omitting
// the body when head is true.
func writeJSONInternalError(w http.ResponseWriter, head bool, err error) {
	// Use the standard internal error response when a body is allowed.
	if !head {
		// Log the error and write the generic client-facing message.
		writeInternalError(w, err)
		// Stop after writing the error response.
		return
	}
	// Log the underlying error without exposing it to the client.
	tslog.Error(err)
	// Obtain the response headers before committing the status.
	header := w.Header()
	// Discard any length set for the original response.
	header.Del("Content-Length")
	// Match the headers on the equivalent plain-text error response.
	header.Set(contentTypeHeader, "text/plain; charset=utf-8")
	// Prevent content-type sniffing, consistent with http.Error.
	header.Set("X-Content-Type-Options", "nosniff")
	// Commit the error status without writing a body.
	w.WriteHeader(http.StatusInternalServerError)
}
