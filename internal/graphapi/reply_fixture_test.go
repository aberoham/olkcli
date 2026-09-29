package graphapi

import (
	"net/http"
	"testing"
)

// isOriginalFormatRead reports whether req is the read of an original
// message's body type that precedes a plain reply or forward.
func isOriginalFormatRead(req *http.Request) bool {
	return req.Method == http.MethodGet && req.URL.Query().Get("$select") == "body"
}

// answerOriginalFormat answers the original-format read with a body of the
// given content type and passes every other request to next, so fixtures
// written for the Graph action itself need not model the read.
func answerOriginalFormat(contentType string, next roundTripFunc) roundTripFunc {
	return func(req *http.Request) *http.Response {
		if isOriginalFormatRead(req) {
			return graphJSONResponse(req, `{"body":{"contentType":"`+contentType+`","content":"original"}}`)
		}
		return next(req)
	}
}

// testReplyGraphClient is testGraphClient for reply and forward fixtures
// whose original message is plain text, where the comment is sent as written.
func testReplyGraphClient(t *testing.T, responder roundTripFunc) *Client {
	t.Helper()
	return testGraphClient(t, answerOriginalFormat("text", responder))
}
