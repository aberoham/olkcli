package cmd

import (
	"fmt"
	"strings"

	"github.com/rlrghb/olkcli/internal/graphapi"
	"github.com/rlrghb/olkcli/internal/outfmt"
)

// MailFoldersCmd manages mail folders
type MailFoldersCmd struct {
	List   MailFoldersListCmd   `cmd:"" default:"1" help:"List all visible mail folders recursively"`
	Create MailFoldersCreateCmd `cmd:"" help:"Create a mail folder"`
	Rename MailFoldersRenameCmd `cmd:"" help:"Rename a mail folder"`
	Delete MailFoldersDeleteCmd `cmd:"" help:"Delete a mail folder"`
}

// MailFoldersListCmd lists all visible mail folders recursively (default subcommand)
type MailFoldersListCmd struct {
	WellKnown string `help:"Resolve one guarded destination by canonical Graph name (archive, deleteditems, inbox, or junkemail)"`
}

func (c *MailFoldersListCmd) Run(ctx *RunContext) error {
	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}

	var folders []graphapi.MailFolder
	if c.WellKnown != "" {
		var folder *graphapi.MailFolder
		folder, err = client.GetWellKnownMailFolder(ctx.Ctx, target, c.WellKnown)
		if err == nil {
			folders = []graphapi.MailFolder{*folder}
		}
	} else {
		folders, err = client.ListMailFolders(ctx.Ctx, target)
	}
	if err != nil {
		return err
	}

	printer := ctx.Printer()
	if ctx.Flags.JSON {
		return printer.PrintJSON(folders, len(folders), "")
	}

	headers := []string{"ID", "NAME", "TOTAL", "UNREAD"}
	rows := make([][]string, 0, len(folders))
	for _, f := range folders {
		row := []string{
			f.ID,
			f.DisplayName,
			fmt.Sprintf("%d", f.TotalCount),
			fmt.Sprintf("%d", f.UnreadCount),
		}
		if c.WellKnown != "" {
			row = []string{
				f.ID,
				f.DisplayName,
				f.WellKnownName,
				fmt.Sprintf("%d", f.TotalCount),
				fmt.Sprintf("%d", f.UnreadCount),
			}
		}
		rows = append(rows, row)
	}
	if c.WellKnown != "" {
		headers = []string{"ID", "NAME", "WELL-KNOWN", "TOTAL", "UNREAD"}
	}

	return printer.Print(headers, rows, folders, len(folders), "")
}

// validateMailFolderName rejects a display name that could not be addressed
// afterwards. Folder paths are split on "/", so a folder whose own name holds a
// slash is unreachable by path; hint tells the caller what to use instead.
func validateMailFolderName(name, hint string) error {
	if name == "" {
		return fmt.Errorf("folder name cannot be empty")
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("folder name %q cannot contain \"/\": %s", outfmt.Sanitize(name), hint)
	}
	return nil
}

// mailboxSuffix returns the phrase that names a delegated mailbox in a folder
// command's output, and nothing for the signed-in user's own mailbox so that
// output without --mailbox is unchanged.
func mailboxSuffix(preposition, target string) string {
	if target == "" {
		return ""
	}
	return " " + preposition + " " + target
}

// explainUnresolvedFolderPath annotates a failed folder write whose reference
// was passed to Graph unchanged. The resolver leaves a slash-bearing value alone
// when its first component names no top-level folder, because a Graph ID may
// itself contain a slash, so a mistyped path would otherwise surface only as
// Graph's complaint about a malformed ID.
func explainUnresolvedFolderPath(reference, resolved string, err error) error {
	if err == nil || resolved != reference || !strings.Contains(reference, "/") {
		return err
	}
	first, _, _ := strings.Cut(reference, "/")
	return fmt.Errorf("%w\n\n%q was sent to Graph as a folder ID because %q does not name a top-level folder "+
		"in that mailbox. If it was meant as a path, check its first component",
		err, outfmt.Sanitize(reference), outfmt.Sanitize(first))
}

// MailFoldersCreateCmd creates a new mail folder
type MailFoldersCreateCmd struct {
	Name   string `help:"Folder name" required:"" short:"n"`
	Parent string `help:"Parent folder ID, well-known name, or path (for example Inbox/2026); omit to create a top-level folder"`
}

// Run creates the folder. A dry run makes no Graph request, so it reports the
// parent as typed and does not check that the parent exists.
func (c *MailFoldersCreateCmd) Run(ctx *RunContext) error {
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}
	if err := validateMailFolderName(c.Name, "name the containing folder with --parent"); err != nil {
		return err
	}

	if ctx.Flags.DryRun {
		under := ""
		if c.Parent != "" {
			under = " under " + outfmt.Sanitize(c.Parent)
		}
		fmt.Printf("Would create folder %q%s%s\n", outfmt.Sanitize(c.Name), under, mailboxSuffix("in", target))
		return nil
	}

	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	parentID := ""
	if c.Parent != "" {
		parentID, err = client.ResolveMailFolderPath(ctx.Ctx, target, c.Parent)
		if err != nil {
			return err
		}
	}
	folder, err := client.CreateMailFolder(ctx.Ctx, target, parentID, c.Name)
	if err != nil {
		return explainUnresolvedFolderPath(c.Parent, parentID, err)
	}

	fmt.Printf("Folder created%s: %s (ID: %s)\n", mailboxSuffix("in", target),
		outfmt.Sanitize(folder.DisplayName), outfmt.Sanitize(folder.ID))
	return nil
}

// MailFoldersRenameCmd renames a mail folder
type MailFoldersRenameCmd struct {
	ID   string `arg:"" help:"Folder ID or path (for example Inbox/2026)"`
	Name string `help:"New folder name" required:"" short:"n"`
}

func (c *MailFoldersRenameCmd) Run(ctx *RunContext) error {
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}
	if err := validateMailFolderName(c.Name, "a rename changes the name only and does not move the folder"); err != nil {
		return err
	}

	if ctx.Flags.DryRun {
		fmt.Printf("Would rename folder %s to %q%s\n", outfmt.Sanitize(c.ID), outfmt.Sanitize(c.Name),
			mailboxSuffix("in", target))
		return nil
	}

	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	folderID, err := client.ResolveMailFolderPath(ctx.Ctx, target, c.ID)
	if err != nil {
		return err
	}
	folder, err := client.RenameMailFolder(ctx.Ctx, target, folderID, c.Name)
	if err != nil {
		return explainUnresolvedFolderPath(c.ID, folderID, err)
	}

	fmt.Printf("Folder renamed%s: %s\n", mailboxSuffix("in", target), outfmt.Sanitize(folder.DisplayName))
	return nil
}

// MailFoldersDeleteCmd deletes a mail folder
type MailFoldersDeleteCmd struct {
	ID string `arg:"" help:"Folder ID or path (for example Inbox/2026)"`
}

func (c *MailFoldersDeleteCmd) Run(ctx *RunContext) error {
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}
	if !ctx.Flags.Force {
		return fmt.Errorf("delete folder %s: use --force to confirm deletion", outfmt.Sanitize(outfmt.Truncate(c.ID, 30)))
	}

	if ctx.Flags.DryRun {
		fmt.Printf("Would delete folder %s%s\n", outfmt.Sanitize(c.ID), mailboxSuffix("from", target))
		return nil
	}

	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	folderID, err := client.ResolveMailFolderPath(ctx.Ctx, target, c.ID)
	if err != nil {
		return err
	}
	if err := client.DeleteMailFolder(ctx.Ctx, target, folderID); err != nil {
		return explainUnresolvedFolderPath(c.ID, folderID, err)
	}

	fmt.Printf("Folder deleted%s.\n", mailboxSuffix("from", target))
	return nil
}
