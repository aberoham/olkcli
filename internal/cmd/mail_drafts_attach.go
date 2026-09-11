package cmd

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rlrghb/olkcli/internal/graphapi"
)

// MailDraftsAttachCmd adds a regular file attachment to an existing draft.
type MailDraftsAttachCmd struct {
	ID   string `arg:"" help:"Draft message ID"`
	File string `arg:"" help:"File to attach (under 3 MB)" type:"existingfile"`
}

func (c *MailDraftsAttachCmd) Run(ctx *RunContext) error {
	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}
	pathInfo, err := os.Stat(c.File)
	if err != nil {
		return fmt.Errorf("reading attachment: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("attachment must be a regular file")
	}
	file, err := os.Open(c.File)
	if err != nil {
		return fmt.Errorf("opening attachment: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("reading attachment: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("attachment must be a regular file")
	}
	if info.Size() >= graphapi.MaxInlineAttachmentBytes {
		return fmt.Errorf("attachment must be under 3 MB")
	}
	content, err := io.ReadAll(io.LimitReader(file, graphapi.MaxInlineAttachmentBytes))
	if err != nil {
		return fmt.Errorf("reading attachment: %w", err)
	}
	if len(content) >= graphapi.MaxInlineAttachmentBytes {
		return fmt.Errorf("attachment must be under 3 MB")
	}
	filename := filepath.Base(c.File)
	contentType := mime.TypeByExtension(filepath.Ext(c.File))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	att := &graphapi.Attachment{Name: filename, ContentType: contentType, Size: int32(len(content))}
	if !ctx.Flags.DryRun {
		client, err := ctx.GraphClient()
		if err != nil {
			return err
		}
		att, err = client.AttachToDraft(ctx.Ctx, target, c.ID, filename, contentType, content)
		if err != nil {
			return err
		}
	}
	receipt := struct {
		ID         string               `json:"id"`
		Mailbox    string               `json:"mailbox"`
		DryRun     bool                 `json:"dryRun"`
		Attachment *graphapi.Attachment `json:"attachment"`
	}{c.ID, target, ctx.Flags.DryRun, att}
	return ctx.Printer().Print([]string{"DRAFT_ID", "MAILBOX", "ATTACHMENT_ID", "NAME", "SIZE", "DRY_RUN"}, [][]string{{
		c.ID, describeMailbox(target), att.ID, att.Name, strconv.FormatInt(int64(att.Size), 10), strconv.FormatBool(ctx.Flags.DryRun),
	}}, receipt, 1, "")
}
