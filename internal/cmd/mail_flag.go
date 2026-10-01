package cmd

import (
	"fmt"

	"github.com/rlrghb/olkcli/internal/outfmt"
)

// MailFlagCmd sets the follow-up flag on a message
type MailFlagCmd struct {
	ID     string `arg:"" help:"Message ID"`
	Status string `arg:"" help:"Flag status: flagged|complete|notFlagged" enum:"flagged,complete,notFlagged" default:"flagged"`
}

func (c *MailFlagCmd) Run(ctx *RunContext) error {
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}
	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	if ctx.Flags.DryRun {
		fmt.Printf("Would flag message %s as %s%s\n", outfmt.Sanitize(c.ID), outfmt.Sanitize(c.Status), mailboxSuffix("in", target))
		return nil
	}

	err = client.FlagMessage(ctx.Ctx, target, c.ID, c.Status)
	if err != nil {
		return err
	}

	fmt.Printf("Message flagged as %s.\n", outfmt.Sanitize(c.Status))
	return nil
}
