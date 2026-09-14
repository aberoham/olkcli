package graphapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/microsoftgraph/msgraph-sdk-go/users"
)

// Graph's createReply renders two details differently from Outlook on the
// web: the subject prefix is upper-cased ("RE:" rather than "Re:") and the
// quoted "Sent:" line carries a weekday, seconds and UTC ("Monday, 14
// September 2026 07:45:15") where the web client writes the mailbox's own
// time zone without either ("14 September 2026 08:45"). Both are visible to
// the recipient, so an HTML reply draft normalises them to match what a
// person would have produced in the web client.
const (
	graphReplySubjectPrefix      = "RE: "
	outlookWebReplySubjectPrefix = "Re: "
	outlookWebSentTimeLayout     = "2 January 2006 15:04"
	quotedHeaderMarker           = `id="divRplyFwdMsg"`
	quotedSentLabel              = "<b>Sent:</b>"
	mailboxTimeZoneHint          = "pass --tz to use a zone of your own instead"
)

// outlookWebReplySubject rewrites Graph's "RE: " prefix to the "Re: " form
// Outlook on the web uses. Any other subject is returned unchanged.
func outlookWebReplySubject(subject string) string {
	if strings.HasPrefix(subject, graphReplySubjectPrefix) {
		return outlookWebReplySubjectPrefix + subject[len(graphReplySubjectPrefix):]
	}
	return subject
}

// quotedSentLineBounds locates the text of the quoted "Sent:" value in the
// outermost divRplyFwdMsg block of an HTML reply body: the run between the
// label and the next tag. Outlook writes the header lines before any nested
// element closes, so the search stops at the first closing div after the
// marker; a Sent label past that point belongs to an older reply deeper in
// the quote, which keeps whatever its author's client wrote, exactly as it
// would in the web client.
func quotedSentLineBounds(html string) (start, end int, ok bool) {
	header := strings.Index(html, quotedHeaderMarker)
	if header < 0 {
		return 0, 0, false
	}
	block := html[header:]
	if closing := strings.Index(block, "</div>"); closing >= 0 {
		block = block[:closing]
	}
	label := strings.Index(block, quotedSentLabel)
	if label < 0 {
		return 0, 0, false
	}
	start = header + label + len(quotedSentLabel)
	tag := strings.IndexByte(block[label+len(quotedSentLabel):], '<')
	if tag < 0 {
		return 0, 0, false
	}
	return start, start + tag, true
}

// hasQuotedSentLine reports whether html carries a quoted "Sent:" line that
// outlookWebQuotedSentLine would rewrite, so the caller can skip reading the
// original message's timestamp when there is nothing to rewrite.
func hasQuotedSentLine(html string) bool {
	_, _, ok := quotedSentLineBounds(html)
	return ok
}

// outlookWebQuotedSentLine replaces the first quoted "Sent:" value with sent
// rendered in its own location using the web client's layout. The body is
// returned unchanged when it has no quoted Sent line.
func outlookWebQuotedSentLine(html string, sent time.Time) string {
	start, end, ok := quotedSentLineBounds(html)
	if !ok {
		return html
	}
	return html[:start] + " " + sent.Format(outlookWebSentTimeLayout) + html[end:]
}

// quotedSentTime returns the original message's sent time in the zone the
// quoted header should show: an explicit location when given, otherwise the
// mailbox's own time-zone setting, which is what Outlook on the web uses.
func (c *Client) quotedSentTime(ctx context.Context, target, messageID string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		var err error
		loc, err = c.mailboxTimeZone(ctx, target)
		if err != nil {
			return time.Time{}, err
		}
	}
	sent, err := c.messageSentTime(ctx, target, messageID)
	if err != nil {
		return time.Time{}, err
	}
	return sent.In(loc), nil
}

// mailboxTimeZone reads the mailbox's configured time zone and resolves it
// to a location. Exchange stores Windows zone names such as "GMT Standard
// Time", so the value goes through the CLDR mapping in windowsTimeZones.
func (c *Client) mailboxTimeZone(ctx context.Context, target string) (*time.Location, error) {
	resp, err := c.targetUser(target).MailboxSettings().Get(ctx, &users.ItemMailboxSettingsRequestBuilderGetRequestConfiguration{
		QueryParameters: &users.ItemMailboxSettingsRequestBuilderGetQueryParameters{Select: []string{"timeZone"}},
	})
	if err != nil {
		return nil, fmt.Errorf("reading the mailbox time zone for the quoted Sent line (%s): %w", mailboxTimeZoneHint, err)
	}
	zone := ""
	if resp != nil {
		zone = derefStr(resp.GetTimeZone())
	}
	return locationForWindowsTimeZone(zone)
}

// locationForWindowsTimeZone resolves a Windows zone name through the CLDR
// table. IANA names are accepted as well, since Exchange can return them for
// mailboxes configured that way.
func locationForWindowsTimeZone(zone string) (*time.Location, error) {
	if zone == "" {
		return nil, fmt.Errorf("reading the mailbox time zone for the quoted Sent line: Graph returned no timeZone (%s)", mailboxTimeZoneHint)
	}
	name, ok := windowsTimeZones[zone]
	if !ok {
		name = zone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("mailbox time zone %q is not a known Windows or IANA zone (%s): %w", zone, mailboxTimeZoneHint, err)
	}
	return loc, nil
}

// messageSentTime reads only the sentDateTime of one message, which Graph
// returns in UTC.
func (c *Client) messageSentTime(ctx context.Context, target, messageID string) (time.Time, error) {
	result, err := c.targetUser(target).Messages().ByMessageId(messageID).Get(ctx, &users.ItemMessagesMessageItemRequestBuilderGetRequestConfiguration{
		Headers:         c.messageIDHeaders(nil),
		QueryParameters: &users.ItemMessagesMessageItemRequestBuilderGetQueryParameters{Select: []string{"sentDateTime"}},
	})
	if err != nil {
		return time.Time{}, c.replyDraftError("reading original message time", target, err)
	}
	if result == nil || result.GetSentDateTime() == nil {
		return time.Time{}, fmt.Errorf("reading original message time: Graph returned no sentDateTime for %s", messageID)
	}
	return *result.GetSentDateTime(), nil
}
