package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const uploadURL = "https://example.sharepoint.com/upload?tempauth=SECRET-TOKEN"

func statusResponse(req *http.Request, code int) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: req}
}

func TestUploadChunks(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	for _, tc := range []struct {
		name      string
		file      []byte
		transport transportFunc
		wantErr   string
	}{
		{"completed", data, func(r *http.Request) (*http.Response, error) { return statusResponse(r, http.StatusCreated), nil }, ""},
		{"file shrank", data[:60], func(r *http.Request) (*http.Response, error) { return statusResponse(r, http.StatusCreated), nil }, "reading file chunk"},
		{"never confirmed", data, func(r *http.Request) (*http.Response, error) { return statusResponse(r, http.StatusAccepted), nil }, "upload incomplete"},
		{"transport failure", data, func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset") }, "connection failed"},
		{"malformed redirect", data, func(r *http.Request) (*http.Response, error) {
			resp := statusResponse(r, http.StatusFound)
			resp.Header.Set("Location", "https://example.sharepoint.com/%zz?tempauth=SECRET-TOKEN")
			return resp, nil
		}, "connection failed"},
		{"cancelled", data, func(*http.Request) (*http.Response, error) { return nil, context.Canceled }, "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := uploadChunks(context.Background(), &http.Client{Transport: tc.transport}, uploadURL, bytes.NewReader(tc.file), int64(len(data)))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("upload: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			// The session URL is a bearer credential; no error may quote it.
			if strings.Contains(err.Error(), "SECRET-TOKEN") || strings.Contains(err.Error(), "sharepoint.com") {
				t.Errorf("error leaks the upload URL: %v", err)
			}
		})
	}
}

func TestCopyDeclared(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		size       int64
		wantErr    string
	}{
		{"exact", "hello", 5, ""},
		{"short", "hel", 5, "ended after"},
		{"long", "hello!", 5, "more data than expected"},
	} {
		var out bytes.Buffer
		err := copyDeclared(&out, strings.NewReader(tc.body), tc.size)
		if tc.wantErr == "" {
			if err != nil || out.String() != tc.body {
				t.Errorf("%s: %q, %v", tc.name, out.String(), err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}
