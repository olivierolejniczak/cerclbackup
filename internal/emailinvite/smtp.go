package emailinvite

import (
	"encoding/json"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPConfig holds credentials and server settings for sending invite emails.
type SMTPConfig struct {
	Host     string // e.g. smtp.gmail.com
	Port     int    // e.g. 587
	Username string
	Password string
	From     string // displayed sender address
}

// Send emails the invite payload to the recipient.
// The OOB 6-word code is NOT included in the email body — the caller shares it
// via a separate channel (SMS, Signal, voice).
func Send(cfg SMTPConfig, to string, p Payload) error {
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("smtp: marshal payload: %w", err)
	}

	subject := fmt.Sprintf("CerclBackup invite — circle \"%s\"", p.Circle)
	message := buildMessage(cfg.From, to, subject, string(body))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	if err := smtp.SendMail(addr, auth, cfg.From, []string{to}, []byte(message)); err != nil {
		return fmt.Errorf("smtp: send: %w", err)
	}
	return nil
}

func buildMessage(from, to, subject, payloadJSON string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("\r\n")
	sb.WriteString("You have been invited to join a CerclBackup circle.\n\n")
	sb.WriteString("To accept, you also need the 6-word code shared with you separately\n")
	sb.WriteString("(via SMS, Signal, or voice -- NOT by email).\n\n")
	sb.WriteString("Run:\n")
	sb.WriteString("  cerclbackup join-email --payload invite.json --words \"word1 word2 ...\"\n\n")
	sb.WriteString("--- BEGIN CERCLBACKUP INVITE ---\n")
	sb.WriteString(payloadJSON)
	sb.WriteString("\n--- END CERCLBACKUP INVITE ---\n")
	return sb.String()
}
