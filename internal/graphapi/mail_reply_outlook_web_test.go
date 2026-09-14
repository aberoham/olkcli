package graphapi

import (
	"strings"
	"testing"
	"time"
)

func TestOutlookWebReplySubject(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"graph prefix", "RE: Advisory", "Re: Advisory"},
		{"already web form", "Re: Advisory", "Re: Advisory"},
		{"no prefix", "Advisory", "Advisory"},
		{"prefix without space is left alone", "RE:Advisory", "RE:Advisory"},
		{"prefix mid-subject is left alone", "Fw: RE: Advisory", "Fw: RE: Advisory"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outlookWebReplySubject(tc.in); got != tc.want {
				t.Fatalf("outlookWebReplySubject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

const quotedReplyBody = `<html><body><div>Hi</div><hr><div id="divRplyFwdMsg" dir="ltr">` +
	`<font face="Calibri, sans-serif"><b>From:</b> Maria &lt;m@example.com&gt;<br>` +
	`<b>Sent:</b> Monday, 14 September 2026 07:45:15<br><b>To:</b> Abe<br></font></div>` +
	`<div><div id="divRplyFwdMsg"><b>Sent:</b> Friday, 11 September 2026 16:00:00<br></div></div>` +
	`</body></html>`

func TestOutlookWebQuotedSentLineRewritesOnlyTheOutermostHeader(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London: %v", err)
	}
	sent := time.Date(2026, time.September, 14, 7, 45, 15, 0, time.UTC).In(london)

	got := outlookWebQuotedSentLine(quotedReplyBody, sent)

	want := `<html><body><div>Hi</div><hr><div id="divRplyFwdMsg" dir="ltr">` +
		`<font face="Calibri, sans-serif"><b>From:</b> Maria &lt;m@example.com&gt;<br>` +
		`<b>Sent:</b> 14 September 2026 08:45<br><b>To:</b> Abe<br></font></div>` +
		`<div><div id="divRplyFwdMsg"><b>Sent:</b> Friday, 11 September 2026 16:00:00<br></div></div>` +
		`</body></html>`
	if got != want {
		t.Fatalf("rewritten body =\n%s\nwant\n%s", got, want)
	}
}

func TestOutlookWebQuotedSentLineKeepsTheOffsetGraphRendered(t *testing.T) {
	sent := time.Date(2026, time.September, 14, 8, 45, 15, 0, time.FixedZone("", 3600))
	got := outlookWebQuotedSentLine(`<div id="divRplyFwdMsg"><b>Sent:</b> x<br></div>`, sent)
	want := `<div id="divRplyFwdMsg"><b>Sent:</b> 14 September 2026 08:45<br></div>`
	if got != want {
		t.Fatalf("rewritten body = %q, want %q", got, want)
	}
}

func TestOutlookWebQuotedSentLineLeavesBodyAloneWhenNothingToRewrite(t *testing.T) {
	sent := time.Date(2026, time.September, 14, 7, 45, 15, 0, time.UTC)
	tests := []struct {
		name string
		html string
	}{
		{"no quoted header", `<html><body><div>Hi</div></body></html>`},
		{"header without sent label", `<div id="divRplyFwdMsg"><b>From:</b> x<br></div>`},
		{"sent label never closed", `<div id="divRplyFwdMsg"><b>Sent:</b> Monday`},
		{"sent label only before the header", `<b>Sent:</b> x<br><div id="divRplyFwdMsg"></div>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if hasQuotedSentLine(tc.html) {
				t.Fatalf("hasQuotedSentLine reported a rewritable line")
			}
			if got := outlookWebQuotedSentLine(tc.html, sent); got != tc.html {
				t.Fatalf("body was rewritten:\n%s", got)
			}
		})
	}
}

func TestHasQuotedSentLine(t *testing.T) {
	if !hasQuotedSentLine(quotedReplyBody) {
		t.Fatal("expected a quoted Sent line to be detected")
	}
	if hasQuotedSentLine(`<html><body><div id="quote">Original history</div></body></html>`) {
		t.Fatal("expected no quoted Sent line in a bare quote")
	}
}

func TestLocationForWindowsTimeZone(t *testing.T) {
	tests := []struct {
		name, zone, want string
		wantErr          bool
	}{
		{"windows name", "GMT Standard Time", "Europe/London", false},
		{"windows utc", "UTC", "Etc/UTC", false},
		{"iana passthrough", "Europe/Stockholm", "Europe/Stockholm", false},
		{"empty", "", "", true},
		{"unknown", "Narnia Standard Time", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := locationForWindowsTimeZone(tc.zone)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.zone)
				}
				if !strings.Contains(err.Error(), "--tz") {
					t.Fatalf("error does not point at --tz: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("locationForWindowsTimeZone(%q): %v", tc.zone, err)
			}
			if loc.String() != tc.want {
				t.Fatalf("location = %s, want %s", loc, tc.want)
			}
		})
	}
}

func TestWindowsTimeZonesAllLoad(t *testing.T) {
	for windows, iana := range windowsTimeZones {
		if _, err := time.LoadLocation(iana); err != nil {
			t.Errorf("%q maps to %q, which this Go build cannot load: %v", windows, iana, err)
		}
	}
}

func TestQuotedSentLineStaysInsideTheOuterHeaderBlock(t *testing.T) {
	tests := []struct {
		name string
		html string
	}{
		{"sent after the header closes", `<div id="divRplyFwdMsg"><b>From:</b> x<br></div><b>Sent:</b> later<br>`},
		{"sent only in an older nested header", `<div id="divRplyFwdMsg"><b>From:</b> x<br></div><div><div id="divRplyFwdMsg"><b>Sent:</b> old<br></div></div>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if hasQuotedSentLine(tc.html) {
				t.Fatalf("Sent line outside the outer header was treated as rewritable")
			}
		})
	}
}
