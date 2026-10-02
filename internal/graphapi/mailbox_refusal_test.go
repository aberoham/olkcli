package graphapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// refusedMailboxCalls is one representative write from each family that takes a
// mailbox target. wantHint is a phrase unique to that family's guidance.
var refusedMailboxCalls = []struct {
	name     string
	action   string
	wantHint string
	call     func(c *Client, ctx context.Context, target string) error
}{
	{
		name:     "mark a message",
		action:   "updating message",
		wantHint: "Mail.ReadWrite.Shared",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.MarkMessage(ctx, target, "message-id", true)
		},
	},
	{
		name:     "flag a message",
		action:   "flagging message",
		wantHint: "Mail.ReadWrite.Shared",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.FlagMessage(ctx, target, "message-id", "flagged")
		},
	},
	{
		name:     "create an event",
		action:   "creating event",
		wantHint: "Calendars.ReadWrite.Shared",
		call: func(c *Client, ctx context.Context, target string) error {
			start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
			_, err := c.CreateEvent(ctx, target, &CreateEventOptions{Subject: "Review", Start: start, End: start.Add(time.Hour)})
			return err
		},
	},
	{
		name:     "respond to an event",
		action:   "responding to event",
		wantHint: "Calendars.ReadWrite.Shared",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.RespondToEvent(ctx, target, "event-id", "accept")
		},
	},
	{
		name:     "delete an event attachment",
		action:   "deleting event attachment",
		wantHint: "Calendars.ReadWrite.Shared",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.DeleteCalendarAttachment(ctx, target, "event-id", "attachment-id")
		},
	},
	{
		name:     "delete a contact",
		action:   "deleting contact",
		wantHint: "Contacts.ReadWrite.Shared",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.DeleteContact(ctx, target, "contact-id")
		},
	},
	{
		name:     "create a task list",
		action:   "creating todo list",
		wantHint: "does not document signed-in access to another mailbox's To Do lists",
		call: func(c *Client, ctx context.Context, target string) error {
			_, err := c.CreateTodoList(ctx, target, "Work")
			return err
		},
	},
	{
		name:     "delete an inbox rule",
		action:   "deleting mail rule",
		wantHint: "documents no shared-mailbox scope",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.DeleteMailRule(ctx, target, "rule-id")
		},
	},
	{
		name:     "create a category",
		action:   "creating category",
		wantHint: "documents no shared-mailbox scope",
		call: func(c *Client, ctx context.Context, target string) error {
			_, err := c.CreateCategory(ctx, target, "Green", "")
			return err
		},
	},
	{
		name:     "turn off automatic replies",
		action:   "updating auto-reply settings",
		wantHint: "documents no shared-mailbox scope",
		call: func(c *Client, ctx context.Context, target string) error {
			return c.SetAutoReply(ctx, target, "disabled", "", "", "", "", "")
		},
	},
}

// A refusal against another mailbox has to say which mailbox was refused and
// what that family of operation needs, and keep the Graph code and status
// reachable for --json. Each family carries its own guidance, because the scope
// that unlocks one does nothing for another.
func TestRefusedWritesInAnotherMailboxExplainTheFamilyGrant(t *testing.T) {
	const code, message = "ErrorAccessDenied", "Access is denied. Check credentials and try again."
	for _, tc := range refusedMailboxCalls {
		t.Run(tc.name, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if !strings.HasPrefix(req.URL.Path, "/v1.0/users/shared@example.com/") {
					t.Errorf("request path %q is not in the target mailbox", req.URL.Path)
				}
				return replyDraftErrorResponse(req, http.StatusForbidden, code, message)
			})
			err := tc.call(client, context.Background(), "shared@example.com")
			if err == nil {
				t.Fatal("want an error")
			}
			text := err.Error()
			for _, want := range []string{tc.action + " in shared@example.com", code, tc.wantHint} {
				if !strings.Contains(text, want) {
					t.Errorf("error %q lacks %q", text, want)
				}
			}
			if strings.Contains(text, "Mail.Send.Shared") {
				t.Errorf("error %q carries sending guidance", text)
			}
			if gotCode, status := ErrorMetadata(err); gotCode != code || status != http.StatusForbidden {
				t.Errorf("ErrorMetadata = (%q, %d), want (%q, 403)", gotCode, status, code)
			}
		})
	}
}

// The same refusal in the signed-in user's own mailbox must not mention another
// mailbox or shared-mailbox scopes: nothing about it is a delegation problem.
func TestRefusedWritesInOwnMailboxCarryNoDelegationGuidance(t *testing.T) {
	for _, tc := range refusedMailboxCalls {
		t.Run(tc.name, func(t *testing.T) {
			client := testGraphClient(t, func(req *http.Request) *http.Response {
				if !strings.HasPrefix(req.URL.Path, meBuilderPath+"/") {
					t.Errorf("request path %q is not in the caller's own mailbox", req.URL.Path)
				}
				return replyDraftErrorResponse(req, http.StatusForbidden, "ErrorAccessDenied", "Access is denied.")
			})
			err := tc.call(client, context.Background(), "")
			if err == nil {
				t.Fatal("want an error")
			}
			for _, unwanted := range []string{" in ", ".Shared", "another mailbox", "shared-mailbox"} {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("own-mailbox error %q contains %q", err, unwanted)
				}
			}
			if gotCode, status := ErrorMetadata(err); gotCode != "ErrorAccessDenied" || status != http.StatusForbidden {
				t.Errorf("ErrorMetadata = (%q, %d), want (ErrorAccessDenied, 403)", gotCode, status)
			}
		})
	}
}
