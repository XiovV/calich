// Package mailer sends Email-Channel Reminders over SMTP (ADR-0021).
package mailer

import (
	"fmt"
	"net/smtp"
)

// SMTPMailer sends mail through a configured SMTP transport.
type SMTPMailer struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

func NewSMTPMailer(host, port, user, pass, from string) *SMTPMailer {
	return &SMTPMailer{Host: host, Port: port, User: user, Pass: pass, From: from}
}

// Send delivers a plain-text email to a single recipient.
func (m *SMTPMailer) Send(to, subject, body string) error {
	addr := m.Host + ":" + m.Port
	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	msg := fmt.Appendf(nil, "From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n", m.From, to, subject, body)

	return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
}
