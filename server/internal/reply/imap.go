package reply

import (
	"context"
	"fmt"
	"io"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// IMAPClient reads unseen mail from one IMAP mailbox and flags it \Seen —
// the MailReader the Worker drains through (ADR-0059). It dials a fresh TLS
// connection per call rather than holding one open across Ticks, the same
// one-connection-per-operation style mailer.SMTPMailer already uses for
// sending: a self-hosted instance polls infrequently enough that the extra
// round trip is immaterial, and it sidesteps ever having to notice and
// recover a gone-stale long-lived session.
type IMAPClient struct {
	host, port, user, pass string
}

func NewIMAPClient(host, port, user, pass string) *IMAPClient {
	return &IMAPClient{host: host, port: port, user: user, pass: pass}
}

// dial connects, authenticates, and selects INBOX — the preamble every
// IMAPClient operation needs. The caller must Logout the returned Client.
func (c *IMAPClient) dial() (*client.Client, error) {
	cl, err := client.DialTLS(c.host+":"+c.port, nil)
	if err != nil {
		return nil, fmt.Errorf("dial imap: %w", err)
	}
	if err := cl.Login(c.user, c.pass); err != nil {
		cl.Logout()
		return nil, fmt.Errorf("imap login: %w", err)
	}
	if _, err := cl.Select("INBOX", false); err != nil {
		cl.Logout()
		return nil, fmt.Errorf("select inbox: %w", err)
	}
	return cl, nil
}

// FetchUnseen returns every message in INBOX not yet flagged \Seen. Peeks
// the body (BODY.PEEK) rather than plain BODY, so fetching a message never
// implicitly flags it \Seen itself — only Worker.processMessage recognizing
// it as a calendar REPLY does that, via a later, explicit MarkSeen.
func (c *IMAPClient) FetchUnseen(ctx context.Context) ([]RawMessage, error) {
	cl, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer cl.Logout()

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	uids, err := cl.UidSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("search unseen: %w", err)
	}
	if len(uids) == 0 {
		return nil, nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{section.FetchItem()}

	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- cl.UidFetch(seqset, items, messages)
	}()

	var out []RawMessage
	for msg := range messages {
		body := msg.GetBody(section)
		if body == nil {
			continue
		}
		raw, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read message body (uid=%d): %w", msg.Uid, err)
		}
		out = append(out, RawMessage{UID: msg.Uid, Bytes: raw})
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("fetch unseen: %w", err)
	}
	return out, nil
}

// MarkSeen flags uids \Seen. A no-op (no connection made) for an empty
// uids, since Tick only ever calls this once it has at least one message to
// flag.
func (c *IMAPClient) MarkSeen(ctx context.Context, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}

	cl, err := c.dial()
	if err != nil {
		return err
	}
	defer cl.Logout()

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := cl.UidStore(seqset, item, []interface{}{imap.SeenFlag}, nil); err != nil {
		return fmt.Errorf("mark seen: %w", err)
	}
	return nil
}
