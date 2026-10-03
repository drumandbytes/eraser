package inbox

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
)

// maxMIMEPartBytes bounds each MIME part read: anyone can mail this inbox.
const maxMIMEPartBytes = 10 << 20 // 10MB

// Monitor handles IMAP connection and email monitoring
type Monitor struct {
	config  config.InboxConfig
	client  *client.Client
	brokers map[string]broker.Broker // Map of email domain to broker
}

// Email represents a parsed email from a broker
type Email struct {
	UID        uint32 // IMAP UID for operations like move/delete
	MessageID  string
	From       string
	FromName   string // Sender display name (e.g., "Mail Delivery System")
	FromDomain string
	Subject    string
	Body       string
	HTMLBody   string
	ReceivedAt time.Time
	BrokerID   string // Matched broker ID (if found)
	BrokerName string // Matched broker name (if found)
}

// NewMonitor creates a new inbox monitor
func NewMonitor(cfg config.InboxConfig, brokerList []broker.Broker) *Monitor {
	// Build a map of email domains to brokers for quick lookup
	brokerMap := make(map[string]broker.Broker)
	for _, b := range brokerList {
		if b.Email != "" {
			parts := strings.Split(b.Email, "@")
			if len(parts) == 2 {
				domain := strings.ToLower(parts[1])
				brokerMap[domain] = b
			}
		}
		// Also map by website domain
		if b.Website != "" {
			domain := extractDomain(b.Website)
			if domain != "" {
				brokerMap[domain] = b
			}
		}
	}

	return &Monitor{
		config:  cfg,
		brokers: brokerMap,
	}
}

// extractDomain extracts the domain from a URL
func extractDomain(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "www.")
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return ""
}

// Connect establishes IMAP connection
func (m *Monitor) Connect(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", m.config.Server, m.config.Port)

	log.Printf("Connecting to IMAP server %s...", addr)

	c, err := dialIMAP(addr, m.config.Server, m.config.Port)
	if err != nil {
		return err
	}

	log.Printf("Connected, logging in as %s...", m.config.Email)

	if err := c.Login(m.config.Email, m.config.Password); err != nil {
		_ = c.Logout()
		return fmt.Errorf("failed to login: %w", err)
	}

	m.client = c
	log.Printf("Login successful")
	return nil
}

// dialIMAP connects with TLS from the first byte on 993, STARTTLS on any
// other port. Never falls back to plaintext: LOGIN sends the password.
func dialIMAP(addr, host string, port int) (*client.Client, error) {
	tlsCfg := config.TLSFor(host)
	if config.ImplicitTLS(port) {
		c, err := client.DialTLS(addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
		}
		return c, nil
	}
	c, err := client.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IMAP server: %w", err)
	}
	if ok, err := c.SupportStartTLS(); err != nil || !ok {
		_ = c.Logout()
		return nil, fmt.Errorf("IMAP server on port %d does not offer STARTTLS", port)
	}
	if err := c.StartTLS(tlsCfg); err != nil {
		_ = c.Logout()
		return nil, fmt.Errorf("IMAP STARTTLS failed: %w", err)
	}
	return c, nil
}

// Disconnect closes the IMAP connection
func (m *Monitor) Disconnect() error {
	if m.client != nil {
		return m.client.Logout()
	}
	return nil
}

// uidSearchCtx runs UidSearch in a goroutine so ctx can cancel it (go-imap
// v1.2.1 has no context support). On cancel the command keeps running on the
// shared connection until the server replies, as with WatchForNewEmails.
func (m *Monitor) uidSearchCtx(ctx context.Context, criteria *imap.SearchCriteria) ([]uint32, error) {
	type result struct {
		uids []uint32
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		uids, err := m.client.UidSearch(criteria)
		resultCh <- result{uids, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resultCh:
		return r.uids, r.err
	}
}

// fetchMessagesCtx runs UidFetch in a goroutine, parses messages as they
// stream in, and honors ctx cancellation - same pattern/caveat as
// uidSearchCtx above. bufSize sizes the messages channel so the UidFetch
// goroutine never blocks writing to it even if we stop reading early.
func (m *Monitor) fetchMessagesCtx(ctx context.Context, seqSet *imap.SeqSet, items []imap.FetchItem, section *imap.BodySectionName, bufSize int) ([]Email, error) {
	messages := make(chan *imap.Message, bufSize)
	done := make(chan error, 1)
	go func() {
		done <- m.client.UidFetch(seqSet, items, messages)
	}()

	var emails []Email
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				if err := <-done; err != nil {
					return nil, fmt.Errorf("failed to fetch messages: %w", err)
				}
				return emails, nil
			}
			email, err := m.parseMessage(msg, section)
			if err != nil {
				log.Printf("Warning: failed to parse message: %v", err)
				continue
			}
			if email != nil {
				emails = append(emails, *email)
			}
		}
	}
}

// fetchMatching fetches, from folder, the emails of the last N days that
// keep accepts. It runs in two passes: envelopes for every message in the
// window first (a few hundred bytes each), then full bodies only for the ones
// keep accepted. Bodies are fetched with BODY.PEEK[], so scanning never marks
// the user's mail as read.
func (m *Monitor) fetchMatching(ctx context.Context, folder string, days int, keep func(Email) bool) ([]Email, error) {
	if m.client == nil {
		return nil, fmt.Errorf("not connected to IMAP server")
	}

	mbox, err := m.client.Select(folder, false)
	if err != nil {
		return nil, fmt.Errorf("failed to select mailbox %s: %w", folder, err)
	}
	if mbox.Messages == 0 {
		return nil, nil
	}

	since := time.Now().AddDate(0, 0, -days)
	criteria := imap.NewSearchCriteria()
	criteria.Since = since

	uids, err := m.uidSearchCtx(ctx, criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to search emails in %s: %w", folder, err)
	}
	if len(uids) == 0 {
		return nil, nil
	}

	// The body section isn't requested in pass 1, so parseMessage's GetBody
	// finds nothing and returns header fields only.
	section := &imap.BodySectionName{Peek: true}
	envelopes, err := m.fetchBatched(ctx, uids, 500, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid}, section)
	if err != nil {
		return nil, err
	}

	var matched []uint32
	for _, e := range envelopes {
		if keep(e) {
			matched = append(matched, e.UID)
		}
	}
	log.Printf("%s: %d of %d emails since %s match", folder, len(matched), len(uids), since.Format("2006-01-02"))

	return m.fetchBatched(ctx, matched, 50, []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, section.FetchItem()}, section)
}

// fetchBatched fetches uids in batches of size. A failed batch is logged and
// skipped so one bad message doesn't sink the whole scan; a cancelled ctx
// aborts.
func (m *Monitor) fetchBatched(ctx context.Context, uids []uint32, size int, items []imap.FetchItem, section *imap.BodySectionName) ([]Email, error) {
	var emails []Email
	for i := 0; i < len(uids); i += size {
		batch := uids[i:min(i+size, len(uids))]
		seqSet := new(imap.SeqSet)
		seqSet.AddNum(batch...)

		batchEmails, err := m.fetchMessagesCtx(ctx, seqSet, items, section, len(batch))
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			log.Printf("Warning: error fetching batch: %v", err)
			continue
		}
		emails = append(emails, batchEmails...)
	}
	return emails, nil
}

func fromBroker(e Email) bool { return e.BrokerID != "" }

// parseMessage converts an IMAP message to our Email struct
func (m *Monitor) parseMessage(msg *imap.Message, section *imap.BodySectionName) (*Email, error) {
	if msg == nil || msg.Envelope == nil {
		return nil, nil
	}

	email := &Email{
		UID:        msg.Uid,
		Subject:    msg.Envelope.Subject,
		ReceivedAt: msg.Envelope.Date,
	}

	if msg.Envelope.MessageId != "" {
		email.MessageID = msg.Envelope.MessageId
	}

	if len(msg.Envelope.From) > 0 {
		from := msg.Envelope.From[0]
		email.From = from.Address()
		email.FromName = from.PersonalName
		if from.HostName != "" {
			email.FromDomain = strings.ToLower(from.HostName)
		}
	}

	// Try to match to a known broker
	if email.FromDomain != "" {
		if b, ok := m.brokers[email.FromDomain]; ok {
			email.BrokerID = b.ID
			email.BrokerName = b.Name
		}
	}

	r := msg.GetBody(section)
	if r == nil {
		return email, nil
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		return email, nil // Return without body on parse error
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			body, _ := io.ReadAll(io.LimitReader(p.Body, maxMIMEPartBytes))

			if strings.HasPrefix(ct, "text/plain") && email.Body == "" {
				email.Body = string(body)
			} else if strings.HasPrefix(ct, "text/html") && email.HTMLBody == "" {
				email.HTMLBody = string(body)
			}
		}
	}

	return email, nil
}

// FetchBrokerEmails fetches only emails from known broker domains
func (m *Monitor) FetchBrokerEmails(ctx context.Context, days int) ([]Email, error) {
	return m.fetchMatching(ctx, m.config.Folder, days, fromBroker)
}

// FetchBrokerEmailsFromFolder fetches broker emails from a specific folder
func (m *Monitor) FetchBrokerEmailsFromFolder(ctx context.Context, folder string, days int) ([]Email, error) {
	return m.fetchMatching(ctx, folder, days, fromBroker)
}

// Deliberately not the classifier's bounceSenders: that list includes
// "noreply", which would pull every newsletter's body into a bounce scan.
var (
	bounceFetchSenders = []string{
		"mailer-daemon", "postmaster", "mail delivery",
		"mail delivery system", "mail delivery subsystem",
		"mailerdaemon", "mailsystem",
	}
	bounceFetchSubjects = []string{
		"undeliverable", "delivery failed", "delivery status notification",
		"returned mail", "mail delivery failed", "delivery failure",
		"message not delivered", "could not be delivered",
	}
)

// looksLikeBounce reports whether an email looks like a bounce/undeliverable
// notification, judged from sender and subject only (both in the envelope).
func looksLikeBounce(e Email) bool {
	fromLower := strings.ToLower(e.From)
	fromNameLower := strings.ToLower(e.FromName)
	for _, sender := range bounceFetchSenders {
		if strings.Contains(fromLower, sender) || strings.Contains(fromNameLower, sender) {
			return true
		}
	}
	subjectLower := strings.ToLower(e.Subject)
	for _, pattern := range bounceFetchSubjects {
		if strings.Contains(subjectLower, pattern) {
			return true
		}
	}
	return false
}

// FetchBounceEmails fetches emails that look like bounce/undeliverable notifications
func (m *Monitor) FetchBounceEmails(ctx context.Context, days int) ([]Email, error) {
	return m.fetchMatching(ctx, m.config.Folder, days, looksLikeBounce)
}

// WatchForNewEmails monitors for new emails (blocking)
func (m *Monitor) WatchForNewEmails(ctx context.Context, callback func(Email)) error {
	if m.client == nil {
		return fmt.Errorf("not connected to IMAP server")
	}

	// Select mailbox
	_, err := m.client.Select(m.config.Folder, false)
	if err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	// The client delivers unilateral responses (EXISTS after the SELECT in
	// FetchBrokerEmails, for one) on Updates and blocks until they're read,
	// so draining it here in the loop deadlocked on the first new mail. A
	// separate reader keeps the connection moving and just flags new mail.
	// ponytail: the drain goroutine lives as long as the connection; fine
	// for one watch per Monitor.
	updates := make(chan client.Update, 16)
	newMail := make(chan struct{}, 1)
	go func() {
		for u := range updates {
			if _, ok := u.(*client.MailboxUpdate); ok {
				select {
				case newMail <- struct{}{}:
				default:
				}
			}
		}
	}()
	m.client.Updates = updates

	stop := make(chan struct{})
	idleDone := make(chan error, 1)

	go func() {
		idleDone <- m.client.Idle(stop, nil)
	}()

	log.Printf("Watching for new emails (press Ctrl+C to stop)...")

	for {
		select {
		case <-ctx.Done():
			close(stop)
			<-idleDone // wait for the Idle() goroutine to actually exit before
			// this function returns, so the caller doesn't touch m.client
			// concurrently with the IMAP connection still being in use
			return ctx.Err()
		case <-newMail:
			log.Printf("New mail detected")
			close(stop)
			<-idleDone

			emails, err := m.FetchBrokerEmails(ctx, 1)
			if err != nil {
				log.Printf("Error fetching new email: %v", err)
			}
			for _, email := range emails {
				callback(email)
			}

			// The fetch's own SELECT reports EXISTS too; that's covered by
			// the fetch just done, so don't let it bounce IDLE straight away.
			select {
			case <-newMail:
			default:
			}

			// Restart IDLE
			stop = make(chan struct{})
			go func() {
				idleDone <- m.client.Idle(stop, nil)
			}()
		case err := <-idleDone:
			if err != nil {
				return fmt.Errorf("IDLE error: %w", err)
			}
		}
	}
}

// EnsureFolderExists creates a folder/label if it doesn't already exist
func (m *Monitor) EnsureFolderExists(name string) error {
	if m.client == nil {
		return fmt.Errorf("not connected to IMAP server")
	}

	// List existing folders to check if it exists
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- m.client.List("", "*", mailboxes)
	}()

	exists := false
	for mbox := range mailboxes {
		if strings.EqualFold(mbox.Name, name) {
			exists = true
		}
	}

	if err := <-done; err != nil {
		return fmt.Errorf("failed to list folders: %w", err)
	}

	if exists {
		log.Printf("Folder '%s' already exists", name)
		return nil
	}

	if err := m.client.Create(name); err != nil {
		return fmt.Errorf("failed to create folder '%s': %w", name, err)
	}

	log.Printf("Created folder '%s'", name)
	return nil
}

// deletedUIDsBesides returns the UIDs currently flagged \Deleted in the
// selected mailbox that are NOT in ours - used by ArchiveEmails to detect
// (not prevent) the Expunge(nil)-removes-everything hazard documented there.
func (m *Monitor) deletedUIDsBesides(ours []uint32) ([]uint32, error) {
	criteria := imap.NewSearchCriteria()
	criteria.WithFlags = []string{imap.DeletedFlag}

	allDeleted, err := m.client.UidSearch(criteria)
	if err != nil {
		return nil, err
	}

	ourUIDs := make(map[uint32]bool, len(ours))
	for _, uid := range ours {
		ourUIDs[uid] = true
	}

	var unexpected []uint32
	for _, uid := range allDeleted {
		if !ourUIDs[uid] {
			unexpected = append(unexpected, uid)
		}
	}
	return unexpected, nil
}

// ArchiveEmails moves multiple emails to the archive folder
func (m *Monitor) ArchiveEmails(uids []uint32, folder string) error {
	if m.client == nil {
		return fmt.Errorf("not connected to IMAP server")
	}

	if len(uids) == 0 {
		return nil
	}

	// Re-select INBOX to ensure we're in the right mailbox
	if _, err := m.client.Select(m.config.Folder, false); err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)

	// Try MOVE first (RFC 6851) - this is most efficient
	if err := m.client.UidMove(seqSet, folder); err != nil {
		log.Printf("MOVE not supported, falling back to COPY+DELETE: %v", err)

		// Fallback to COPY + DELETE if MOVE not supported
		if err := m.client.UidCopy(seqSet, folder); err != nil {
			return fmt.Errorf("failed to copy emails to '%s': %w", folder, err)
		}

		item := imap.FormatFlagsOp(imap.AddFlags, true)
		flags := []interface{}{imap.DeletedFlag}
		if err := m.client.UidStore(seqSet, item, flags, nil); err != nil {
			return fmt.Errorf("failed to mark emails as deleted: %w", err)
		}

		// go-imap v1.2.1 has no UID EXPUNGE, and EXPUNGE removes every
		// \Deleted message, not just ours, so a stray flag can lose mail. We
		// can't close that race here; instead warn if other UIDs are already
		// flagged, and check the expunged count afterwards.
		if unexpected, err := m.deletedUIDsBesides(uids); err != nil {
			log.Printf("Warning: could not verify \\Deleted flag scope before expunge: %v", err)
		} else if len(unexpected) > 0 {
			log.Printf("WARNING: %d message(s) besides the %d just archived are already flagged \\Deleted and will ALSO be removed by this expunge (UIDs: %v) - go-imap v1.2.1 has no UID EXPUNGE to scope this call", len(unexpected), len(uids), unexpected)
		}

		expunged := make(chan uint32, len(uids)+8)
		expungeDone := make(chan error, 1)
		go func() {
			expungeDone <- m.client.Expunge(expunged)
		}()

		var expungedCount int
		for range expunged {
			expungedCount++
		}

		if err := <-expungeDone; err != nil {
			return fmt.Errorf("failed to expunge deleted emails: %w", err)
		}

		if expungedCount != len(uids) {
			log.Printf("WARNING: expunge removed %d message(s) but this operation only intended to remove %d - other \\Deleted-flagged mail may have been removed too", expungedCount, len(uids))
		}
	}

	log.Printf("Archived %d emails to '%s'", len(uids), folder)
	return nil
}
