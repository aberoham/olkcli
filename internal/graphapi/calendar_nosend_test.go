package graphapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// Under --no-send, changing or deleting a meeting the mailbox organizes must not
// reach Graph when attendees would be notified. The fake records every request
// past the lookup.
func TestNoSendGuardsEventChangesThatNotifyAttendees(t *testing.T) {
	const meeting = `{"isOrganizer":true,"attendees":[{"emailAddress":{"address":"a@example.com"}}]}`
	for _, tc := range []struct {
		name    string
		lookup  string // event JSON, or "" for a failed lookup
		refused bool
	}{
		{"organized meeting with attendees", meeting, true},
		{"organizer unknown", `{"attendees":[{"emailAddress":{"address":"a@example.com"}}]}`, true},
		{"failed lookup", "", true},
		{"personal event", `{"isOrganizer":true,"attendees":[]}`, false},
		{"someone else's meeting", `{"isOrganizer":false,"attendees":[{"emailAddress":{"address":"a@example.com"}}]}`, false},
	} {
		for _, op := range []struct {
			name   string
			method string
			call   func(*Client) error
		}{
			{"update", http.MethodPatch, func(c *Client) error {
				subject := "moved"
				_, err := c.UpdateEvent(context.Background(), "", &UpdateEventOptions{EventID: "event-id", Subject: &subject})
				return err
			}},
			{"delete", http.MethodDelete, func(c *Client) error {
				return c.DeleteEvent(context.Background(), "", "event-id")
			}},
		} {
			t.Run(tc.name+"/"+op.name, func(t *testing.T) {
				changed := 0
				c := testGraphClient(t, func(req *http.Request) *http.Response {
					if req.Method == http.MethodGet {
						if tc.lookup == "" {
							return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{}, Body: http.NoBody, Request: req}
						}
						return graphJSONResponse(req, tc.lookup)
					}
					if req.Method == op.method {
						changed++
					}
					if req.Method == http.MethodDelete {
						return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: req}
					}
					return graphJSONResponse(req, `{"id":"event-id","subject":"moved"}`)
				})
				c.noSend = true
				err := op.call(c)
				if tc.refused {
					if !errors.Is(err, ErrNoSend) {
						t.Errorf("err = %v, want ErrNoSend", err)
					}
					if changed != 0 {
						t.Errorf("%d %s requests reached Graph", changed, op.method)
					}
					return
				}
				if err != nil || changed != 1 {
					t.Errorf("err = %v, %s requests = %d; want nil, 1", err, op.method, changed)
				}
			})
		}
	}
}

// Without --no-send no lookup is made.
func TestEventChangesSkipTheLookupWhenSendingIsAllowed(t *testing.T) {
	c := testGraphClient(t, func(req *http.Request) *http.Response {
		if req.Method == http.MethodGet {
			t.Errorf("unexpected lookup %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: req}
	})
	if err := c.DeleteEvent(context.Background(), "", "event-id"); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
}
