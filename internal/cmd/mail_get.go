package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/rlrghb/olkcli/internal/graphapi"
	"github.com/rlrghb/olkcli/internal/outfmt"
)

const (
	mailBodyFormatHTML = "html"
	mailFormatEML      = "eml"
)

type MailGetCmd struct {
	ID     string `arg:"" help:"Message ID"`
	Format string `help:"Output format: full|text|html|eml (eml is the raw RFC 5322 message)" default:"full" enum:"full,text,html,eml"`
	Out    string `help:"With --format eml, write the message to this file instead of stdout" type:"path"`
}

// writeEML exports the message as raw MIME. The bytes are written unaltered,
// because sanitizing them would corrupt the file; for the same reason they
// cannot be wrapped as untrusted content, so an MCP caller must use --out.
func (c *MailGetCmd) writeEML(ctx *RunContext, client *graphapi.Client, target string) error {
	if c.Out == "" && ctx.Flags.WrapUntrusted {
		return fmt.Errorf("--format eml cannot write raw MIME to stdout under --wrap-untrusted; " +
			"pass --out <file> to save it instead")
	}
	content, err := client.GetMessageMIME(ctx.Ctx, target, c.ID)
	if err != nil {
		return err
	}
	if c.Out == "" {
		_, err = os.Stdout.Write(content)
		return err
	}
	saved, err := safeWriteFile(c.Out, content)
	if err != nil {
		return fmt.Errorf("writing %s: %w", c.Out, err)
	}
	fmt.Printf("Saved: %s\n", saved)
	return nil
}

func (c *MailGetCmd) Run(ctx *RunContext) error {
	client, err := ctx.GraphClient()
	if err != nil {
		return err
	}

	target, err := resolveMailboxTarget(ctx.Flags.Mailbox)
	if err != nil {
		return err
	}

	if c.Format == mailFormatEML {
		return c.writeEML(ctx, client, target)
	}
	if c.Out != "" {
		return fmt.Errorf("--out applies only to --format eml")
	}

	preference := graphapi.MessageBodyDefault
	if c.Format != "full" {
		preference, err = graphapi.ParseMessageBodyPreference(c.Format)
		if err != nil {
			return err
		}
	}
	msg, err := client.GetMessage(ctx.Ctx, target, c.ID, preference)
	if err != nil {
		return err
	}

	printer := ctx.Printer()
	if ctx.Flags.JSON {
		return printer.PrintJSON(msg, 1, "")
	}

	loc, _ := ctx.Timezone()
	fmt.Printf("From:    %s\n", outfmt.Sanitize(msg.From))
	fmt.Printf("To:      %s\n", outfmt.Sanitize(strings.Join(msg.To, ", ")))
	fmt.Printf("Subject: %s\n", outfmt.Sanitize(msg.Subject))
	fmt.Printf("Date:    %s\n", outfmt.Sanitize(outfmt.ConvertTime(msg.ReceivedAt, loc)))
	fmt.Printf("Read:    %v\n", msg.IsRead)
	fmt.Println(strings.Repeat("-", 60))

	switch c.Format {
	case "text", "full":
		if msg.Body != "" {
			fmt.Println(outfmt.SanitizeMultiline(msg.Body))
		} else {
			fmt.Println(outfmt.SanitizeMultiline(msg.BodyPreview))
		}
	case mailBodyFormatHTML:
		fmt.Println(outfmt.SanitizeMultiline(msg.Body))
	}

	return nil
}
