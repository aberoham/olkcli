package graphapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// addReplyRecipients sets on patch the Cc and Bcc lists that result from
// adding the caller's addresses to those Graph generated for the reply. A
// PATCH replaces a recipient list outright, so each list is written in full:
// the generated recipients first, then each added address not already
// present. A list with nothing to add is left out of the PATCH.
func addReplyRecipients(patch, generated models.Messageable, opts *CreateReplyDraftOptions) error {
	if len(opts.Cc) > 0 {
		cc, err := withAddedRecipients(generated.GetCcRecipients(), opts.Cc, "Cc")
		if err != nil {
			return err
		}
		patch.SetCcRecipients(cc)
	}
	if len(opts.Bcc) > 0 {
		bcc, err := withAddedRecipients(generated.GetBccRecipients(), opts.Bcc, "Bcc")
		if err != nil {
			return err
		}
		patch.SetBccRecipients(bcc)
	}
	return nil
}

// withAddedRecipients refuses to proceed when Graph omitted the generated
// list, because writing only the added addresses would silently drop the
// recipients a reply-all is meant to reach.
func withAddedRecipients(generated []models.Recipientable, added []string, label string) ([]models.Recipientable, error) {
	if generated == nil {
		return nil, fmt.Errorf("adding %s recipients: Graph did not report the reply's generated %s list, so it cannot be extended safely", label, label)
	}
	extra, err := makeRecipients(added)
	if err != nil {
		return nil, fmt.Errorf("invalid %s recipient: %w", strings.ToLower(label), err)
	}
	seen := make(map[string]struct{}, len(generated)+len(extra))
	merged := make([]models.Recipientable, 0, len(generated)+len(extra))
	for _, recipient := range append(append([]models.Recipientable{}, generated...), extra...) {
		key := strings.ToLower(recipientAddress(recipient))
		if _, exists := seen[key]; exists && key != "" {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, recipient)
	}
	return merged, nil
}

func recipientAddress(recipient models.Recipientable) string {
	if recipient == nil || recipient.GetEmailAddress() == nil {
		return ""
	}
	return derefStr(recipient.GetEmailAddress().GetAddress())
}

// finishPlainReplyDraft adds any requested recipients to a plain reply draft.
// The comment already carries the body, so only the recipient lists change.
func (c *Client) finishPlainReplyDraft(
	ctx context.Context,
	target, draftID string,
	created models.Messageable,
	opts *CreateReplyDraftOptions,
) (*DraftMessage, error) {
	draft := convertDraft(created)
	if len(opts.Cc) == 0 && len(opts.Bcc) == 0 {
		return &draft, nil
	}
	patch := models.NewMessage()
	if err := addReplyRecipients(patch, created, opts); err != nil {
		return nil, err
	}
	updated, err := c.patchReplyDraft(ctx, target, draftID, patch, replyDraftKind)
	if err != nil {
		return nil, err
	}
	draft.Cc = recipientAddresses(firstNonNil(updated.GetCcRecipients(), patch.GetCcRecipients(), created.GetCcRecipients()))
	draft.Bcc = recipientAddresses(firstNonNil(updated.GetBccRecipients(), patch.GetBccRecipients(), created.GetBccRecipients()))
	return &draft, nil
}

func firstNonNil(lists ...[]models.Recipientable) []models.Recipientable {
	for _, list := range lists {
		if list != nil {
			return list
		}
	}
	return nil
}
