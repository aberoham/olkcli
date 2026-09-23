package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rlrghb/olkcli/internal/graphapi"
	"github.com/rlrghb/olkcli/internal/outfmt"
)

// unchangedPreview marks a field the update leaves alone in table output.
const unchangedPreview = "(unchanged)"

// MailDraftsUpdateCmd replaces only the recipient lists, subject or body
// supplied by the caller.
type MailDraftsUpdateCmd struct {
	ID      string   `arg:"" help:"Draft message ID"`
	To      []string `help:"Replace To recipients; pass an empty string to clear" short:"t"`
	CC      []string `help:"Replace CC recipients; pass an empty string to clear"`
	BCC     []string `help:"Replace BCC recipients; pass an empty string to clear"`
	Subject *string  `help:"Replace the subject" short:"s"`
	Body    *string  `help:"Replace the whole body, including any quoted reply or forward history" short:"b"`
	HTML    bool     `help:"Treat --body as HTML"`
}

func (c *MailDraftsUpdateCmd) Run(ctx *RunContext) error {
	if err := c.validate(); err != nil {
		return err
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
	content := graphapi.DraftContent{Subject: c.Subject, Body: c.Body, IsHTML: c.HTML}
	if !ctx.Flags.DryRun {
		client, err := ctx.GraphClient()
		if err != nil {
			return err
		}
		if err := client.UpdateDraft(ctx.Ctx, target, c.ID, recipients, content); err != nil {
			return err
		}
	}
	receipt := struct {
		ID           string                   `json:"id"`
		Mailbox      string                   `json:"mailbox"`
		DryRun       bool                     `json:"dryRun"`
		Recipients   graphapi.DraftRecipients `json:"recipients"`
		Subject      *string                  `json:"subject" untrusted:"true"`
		BodyReplaced bool                     `json:"bodyReplaced"`
	}{c.ID, target, ctx.Flags.DryRun, recipients, c.Subject, c.Body != nil}
	return ctx.Printer().Print([]string{"ID", "MAILBOX", "TO", "CC", "BCC", "SUBJECT", "BODY", "DRY_RUN"}, [][]string{{
		c.ID, describeMailbox(target), recipientPreview(recipients.To), recipientPreview(recipients.CC), recipientPreview(recipients.BCC),
		subjectPreview(c.Subject), bodyPreview(c.Body, c.HTML), strconv.FormatBool(ctx.Flags.DryRun),
	}}, receipt, 1, "")
}

func (c *MailDraftsUpdateCmd) validate() error {
	if c.To == nil && c.CC == nil && c.BCC == nil && c.Subject == nil && c.Body == nil {
		return fmt.Errorf("nothing to update: give at least one of --to, --cc, --bcc, --subject or --body")
	}
	if c.HTML && c.Body == nil {
		return fmt.Errorf("--html applies to --body; give --body as well")
	}
	if c.Body != nil && len(*c.Body) > maxBodySize {
		return fmt.Errorf("draft body exceeds maximum size of 4MB")
	}
	return nil
}

func subjectPreview(subject *string) string {
	if subject == nil {
		return unchangedPreview
	}
	return outfmt.Sanitize(*subject)
}

func bodyPreview(body *string, isHTML bool) string {
	switch {
	case body == nil:
		return unchangedPreview
	case isHTML:
		return "(replaced, HTML)"
	default:
		return "(replaced, text)"
	}
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
		return unchangedPreview
	}
	if len(values) == 0 {
		return "(clear)"
	}
	return strings.Join(values, ", ")
}
