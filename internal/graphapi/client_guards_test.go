package graphapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The capability guard is the first statement in every mutating method, so it
// returns before the (nil) inner client is ever touched. These representative
// calls cover each return shape (error, (T,error), (string,error)) and the send
// vs write distinction.

func TestNoWriteGuardBlocksMutations(t *testing.T) {
	c := &Client{noWrite: true}
	ctx := context.Background()

	checks := []struct {
		name string
		call func() error
	}{
		{"DeleteMessage", func() error { return c.DeleteMessage(ctx, "", "id") }},
		{"DeleteMessage from a shared mailbox", func() error { return c.DeleteMessage(ctx, "shared@example.com", "id") }},
		{"MoveMessage", func() error { _, err := c.MoveMessage(ctx, "", "id", "f"); return err }},
		{"MoveMessage in a shared mailbox", func() error { _, err := c.MoveMessage(ctx, "team@example.com", "id", "f"); return err }},
		{"DeleteEvent", func() error { return c.DeleteEvent(ctx, "", "id") }},
		{"DeleteContact", func() error { return c.DeleteContact(ctx, "", "id") }},
		{"DeleteDriveItem", func() error { return c.DeleteDriveItem(ctx, "d", "i") }},
		{"DeleteTodoTask", func() error { return c.DeleteTodoTask(ctx, "", "l", "t") }},
		{"CreateMailFolder", func() error { _, err := c.CreateMailFolder(ctx, "", "", "n"); return err }},
		{"CreateMailFolder in a shared mailbox", func() error {
			_, err := c.CreateMailFolder(ctx, "shared@example.com", "p", "n")
			return err
		}},
		{"RenameMailFolder", func() error { _, err := c.RenameMailFolder(ctx, "", "f", "n"); return err }},
		{"RenameMailFolder in a shared mailbox", func() error {
			_, err := c.RenameMailFolder(ctx, "shared@example.com", "f", "n")
			return err
		}},
		{"DeleteMailFolder", func() error { return c.DeleteMailFolder(ctx, "", "f") }},
		{"DeleteMailFolder from a shared mailbox", func() error { return c.DeleteMailFolder(ctx, "shared@example.com", "f") }},
		{"CreateUploadSession", func() error { _, err := c.CreateUploadSession(ctx, "d", "p", false); return err }},
		{"CreateEvent", func() error {
			_, err := c.CreateEvent(ctx, "", &CreateEventOptions{Subject: "s", Start: time.Now(), End: time.Now(), Attendees: []string{"a@b.com"}})
			return err
		}},
		{"SendMessage", func() error { return c.SendMessage(ctx, "", &SendMessageOptions{Subject: "s", Body: "b"}) }},
		// Draft writes can land in someone else's mailbox now, so the guard
		// matters more than when they could only touch your own.
		{"CreateDraft", func() error {
			_, err := c.CreateDraft(ctx, "", "s", "b", []string{"a@b.com"}, nil, nil, false)
			return err
		}},
		{"CreateDraft into a shared mailbox", func() error {
			_, err := c.CreateDraft(ctx, "shared@example.com", "s", "b", []string{"a@b.com"}, nil, nil, false)
			return err
		}},
		{"DeleteDraft", func() error { return c.DeleteDraft(ctx, "", "id") }},
		{"DeleteDraft from a shared mailbox", func() error { return c.DeleteDraft(ctx, "shared@example.com", "id") }},
	}
	for _, tc := range checks {
		if err := tc.call(); !errors.Is(err, ErrNoWrite) {
			t.Errorf("%s under --no-write = %v, want ErrNoWrite", tc.name, err)
		}
	}
}

func TestNoSendGuardBlocksSends(t *testing.T) {
	c := &Client{noSend: true} // read-only off; only sending blocked
	ctx := context.Background()

	sends := []struct {
		name string
		call func() error
	}{
		{"SendMessage", func() error { return c.SendMessage(ctx, "", &SendMessageOptions{Subject: "s", Body: "b"}) }},
		{"ReplyMessage", func() error { return c.ReplyMessage(ctx, "", "id", &ReplyOptions{Body: "c"}) }},
		{"ForwardMessage", func() error {
			return c.ForwardMessage(ctx, "", "id", &ForwardOptions{To: []string{"a@b.com"}, Comment: "c", IsHTML: false})
		}},
		{"SendDraft", func() error { return c.SendDraft(ctx, "", "id") }},
		{"SendDraft from a shared mailbox", func() error { return c.SendDraft(ctx, "shared@example.com", "id") }},
		{"RespondToEvent", func() error { return c.RespondToEvent(ctx, "", "id", "accept") }},
		{"CreateEvent w/ attendees", func() error {
			_, err := c.CreateEvent(ctx, "", &CreateEventOptions{Subject: "s", Start: time.Now(), End: time.Now(), Attendees: []string{"a@b.com"}})
			return err
		}},
	}
	for _, tc := range sends {
		if err := tc.call(); !errors.Is(err, ErrNoSend) {
			t.Errorf("%s under --no-send = %v, want ErrNoSend", tc.name, err)
		}
	}
}

// Rules and automatic replies send mail later, so --no-send refuses the forms
// that do and allows the ones that don't.
func TestNoSendGuardsDeferredSending(t *testing.T) {
	c := &Client{noSend: true}
	ctx := context.Background()
	if _, err := c.CreateMailRule(ctx, "", "fwd", "a@example.com", "", false, "", false, false, "b@example.com", ""); !errors.Is(err, ErrNoSend) {
		t.Errorf("forwarding rule under --no-send = %v, want ErrNoSend", err)
	}
	for _, status := range []string{"alwaysEnabled", "scheduled"} {
		if err := c.SetAutoReply(ctx, "", status, "away", "", "", "", ""); !errors.Is(err, ErrNoSend) {
			t.Errorf("auto-reply %s under --no-send = %v, want ErrNoSend", status, err)
		}
	}
	// Turning replies off, and a rule that only files mail, notify no one: they
	// get past the send guard and fail later on the nil test client instead.
	if err := guardOutcome(func() error { return c.SetAutoReply(ctx, "", "disabled", "", "", "", "", "") }); errors.Is(err, ErrNoSend) {
		t.Error("disabling auto-replies refused under --no-send")
	}
	if err := guardOutcome(func() error {
		_, err := c.CreateMailRule(ctx, "", "file", "a@example.com", "", false, "", true, false, "", "")
		return err
	}); errors.Is(err, ErrNoSend) {
		t.Error("a non-forwarding rule refused under --no-send")
	}
}

// guardOutcome runs call, turning the nil-client panic past the guards into a
// plain error.
func guardOutcome(call func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("reached Graph")
		}
	}()
	return call()
}
