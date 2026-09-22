package cmd

import (
	"fmt"
	"strings"

	"github.com/rlrghb/olkcli/internal/graphapi"
	"github.com/rlrghb/olkcli/internal/outfmt"
)

type MailForwardCmd struct {
	ID      string   `arg:"" help:"Message ID to forward"`
	To      []string `help:"Recipient email addresses" required:"" short:"t"`
	CC      []string `help:"CC recipients"`
	Comment string   `help:"Comment to include" short:"c"`
	HTML    bool     `help:"Comment is HTML"`
	Draft   bool     `help:"Create a forward draft instead of sending"`
}

func (c *MailForwardCmd) Run(ctx *RunContext) error {
	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}

	for _, addr := range append(append([]string{}, c.To...), c.CC...) {
		if err := graphapi.ValidateEmail(addr); err != nil {
			return err
		}
	}

	if ctx.Flags.DryRun {
		if c.Draft {
			fmt.Printf("Would create forward draft of message %s in %s\n  To: %s\n",
				outfmt.Sanitize(c.ID), describeMailbox(target), strings.Join(c.To, ", "))
		} else {
			fmt.Printf("Would forward message %s to %s as %s\n",
				outfmt.Sanitize(c.ID), strings.Join(c.To, ", "), describeMailbox(target))
		}
		if len(c.CC) > 0 {
			fmt.Printf("  Cc: %s\n", strings.Join(c.CC, ", "))
		}
		return nil
	}

	opts := &graphapi.ForwardOptions{To: c.To, Cc: c.CC, Comment: c.Comment, IsHTML: c.HTML}
	if c.Draft {
		draft, err := client.CreateForwardDraft(ctx.Ctx, target, c.ID, opts)
		if err != nil {
			return err
		}
		if ctx.Flags.JSON {
			return ctx.Printer().PrintJSON(draft, 1, "")
		}
		fmt.Printf("Forward draft created in %s: %s (ID: %s)\n",
			describeMailbox(target), outfmt.Sanitize(draft.Subject), outfmt.Sanitize(draft.ID))
		return nil
	}

	if err := client.ForwardMessage(ctx.Ctx, target, c.ID, opts); err != nil {
		return err
	}

	if target != "" {
		fmt.Printf("Message forwarded from %s.\n", target)
		return nil
	}
	fmt.Println("Message forwarded.")
	return nil
}
