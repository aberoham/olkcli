package cmd

import (
	"fmt"

	"github.com/rlrghb/olkcli/internal/graphapi"
	"github.com/rlrghb/olkcli/internal/outfmt"
)

type MailMoveCmd struct {
	ID     string `arg:"" help:"Message ID"`
	Folder string `arg:"" help:"Destination folder ID, well-known name, or path (for example Inbox/2026)"`
}

func (c *MailMoveCmd) Run(ctx *RunContext) error {
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}

	if ctx.Flags.DryRun {
		if target == "" {
			fmt.Printf("Would move message %s to folder %s\n", outfmt.Sanitize(c.ID), outfmt.Sanitize(c.Folder))
		} else {
			fmt.Printf("Would move message %s in %s to folder %s\n", outfmt.Sanitize(c.ID),
				describeMailbox(target), outfmt.Sanitize(c.Folder))
		}
		return nil
	}

	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	folderID, err := client.ResolveMailFolderPath(ctx.Ctx, target, c.Folder)
	if err != nil {
		return err
	}
	receipt, err := client.MoveMessageInMailbox(ctx.Ctx, target, c.ID, folderID)
	if err != nil {
		return err
	}

	if ctx.Flags.JSON {
		return ctx.Printer().PrintJSON([]*graphapi.MoveMessageReceipt{receipt}, 1, "")
	}
	if target == "" {
		fmt.Printf("Message moved to %s.\n", outfmt.Sanitize(c.Folder))
	} else {
		fmt.Printf("Message moved in %s to %s.\n", describeMailbox(target), outfmt.Sanitize(c.Folder))
	}
	return nil
}
