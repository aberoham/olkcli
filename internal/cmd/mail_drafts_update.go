package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rlrghb/olkcli/internal/graphapi"
)

// MailDraftsUpdateCmd replaces only the recipient lists supplied by the caller.
type MailDraftsUpdateCmd struct {
	ID  string   `arg:"" help:"Draft message ID"`
	To  []string `help:"Replace To recipients; pass an empty string to clear" short:"t"`
	CC  []string `help:"Replace CC recipients; pass an empty string to clear"`
	BCC []string `help:"Replace BCC recipients; pass an empty string to clear"`
}

func (c *MailDraftsUpdateCmd) Run(ctx *RunContext) error {
	if c.To == nil && c.CC == nil && c.BCC == nil {
		return fmt.Errorf("nothing to update: give at least one of --to, --cc or --bcc")
	}
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}
	recipients := graphapi.DraftRecipients{To: dropBlank(c.To), CC: dropBlank(c.CC), BCC: dropBlank(c.BCC)}
	for _, list := range [][]string{recipients.To, recipients.CC, recipients.BCC} {
		for _, address := range list {
			if err := graphapi.ValidateEmail(address); err != nil {
				return err
			}
		}
	}
	if !ctx.Flags.DryRun {
		client, err := ctx.GraphClient()
		if err != nil {
			return err
		}
		if err := client.UpdateDraftRecipients(ctx.Ctx, target, c.ID, recipients); err != nil {
			return err
		}
	}
	receipt := struct {
		ID         string                   `json:"id"`
		Mailbox    string                   `json:"mailbox"`
		DryRun     bool                     `json:"dryRun"`
		Recipients graphapi.DraftRecipients `json:"recipients"`
	}{c.ID, target, ctx.Flags.DryRun, recipients}
	return ctx.Printer().Print([]string{"ID", "MAILBOX", "TO", "CC", "BCC", "DRY_RUN"}, [][]string{{
		c.ID, describeMailbox(target), recipientPreview(recipients.To), recipientPreview(recipients.CC), recipientPreview(recipients.BCC), strconv.FormatBool(ctx.Flags.DryRun),
	}}, receipt, 1, "")
}

// Preserve nil (omitted) versus empty (explicitly clear).
func dropBlank(values []string) []string {
	if values == nil {
		return nil
	}
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			kept = append(kept, s)
		}
	}
	return kept
}

func recipientPreview(values []string) string {
	if values == nil {
		return "(unchanged)"
	}
	if len(values) == 0 {
		return "(clear)"
	}
	return strings.Join(values, ", ")
}
