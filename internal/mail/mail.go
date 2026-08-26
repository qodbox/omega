package mail

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

var ErrUnsafeAddress = errors.New("mail: an address contains a line break")

var ErrNoRecipient = errors.New("mail: the message has no recipient")

type Message struct {
	To      []string
	Cc      []string
	Subject string
	Text    string
	HTML    string
	From    string
	ReplyTo string
}

type Config struct {
	Driver   string
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Timeout  time.Duration
}

type Mailer struct {
	cfg Config
	log zerolog.Logger
}

func New(cfg Config, log zerolog.Logger) *Mailer {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Driver == "" {
		cfg.Driver = "log"
	}
	return &Mailer{cfg: cfg, log: log}
}

func (m *Mailer) Driver() string { return m.cfg.Driver }

func (m *Mailer) Send(message Message) error {
	if len(message.To) == 0 {
		return ErrNoRecipient
	}
	for _, address := range append(append([]string{message.From}, message.To...), message.Cc...) {
		if strings.ContainsAny(address, "\r\n") {
			return ErrUnsafeAddress
		}
	}
	for _, address := range message.To {
		if strings.TrimSpace(address) == "" {
			return ErrNoRecipient
		}
	}
	if message.From == "" {
		message.From = m.cfg.From
	}

	switch m.cfg.Driver {
	case "log":
		m.log.Info().
			Strs("to", message.To).
			Str("subject", message.Subject).
			Msg("mail: not sent, the log driver is active")
		return nil
	case "smtp":
		return m.smtp(message)
	default:
		return fmt.Errorf("mail: unknown driver %q", m.cfg.Driver)
	}
}

func (m *Mailer) smtp(message Message) error {
	address := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))

	connection, err := net.DialTimeout("tcp", address, m.cfg.Timeout)
	if err != nil {
		return err
	}

	client, err := smtp.NewClient(connection, m.cfg.Host)
	if err != nil {
		connection.Close()
		return err
	}
	defer client.Quit()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host}); err != nil {
			return err
		}
	}

	if m.cfg.Username != "" {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(message.From); err != nil {
		return err
	}
	for _, recipient := range append(append([]string{}, message.To...), message.Cc...) {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(compose(message))); err != nil {
		return err
	}
	return writer.Close()
}

func compose(message Message) string {
	var head strings.Builder

	fmt.Fprintf(&head, "From: %s\r\n", headerValue(message.From))
	fmt.Fprintf(&head, "To: %s\r\n", headerList(message.To))
	if len(message.Cc) > 0 {
		fmt.Fprintf(&head, "Cc: %s\r\n", headerList(message.Cc))
	}
	if message.ReplyTo != "" {
		fmt.Fprintf(&head, "Reply-To: %s\r\n", headerValue(message.ReplyTo))
	}
	fmt.Fprintf(&head, "Subject: %s\r\n", headerValue(message.Subject))
	fmt.Fprintf(&head, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	head.WriteString("MIME-Version: 1.0\r\n")

	switch {
	case message.HTML != "" && message.Text != "":
		boundary := mimeBoundary()
		fmt.Fprintf(&head, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary)
		fmt.Fprintf(&head, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", boundary, message.Text)
		fmt.Fprintf(&head, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", boundary, message.HTML)
		fmt.Fprintf(&head, "--%s--\r\n", boundary)
	case message.HTML != "":
		head.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		head.WriteString(message.HTML)
	default:
		head.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		head.WriteString(message.Text)
	}

	return head.String()
}

func mimeBoundary() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "omega-boundary-fallback"
	}
	return "omega-" + hex.EncodeToString(raw)
}

func headerValue(raw string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, raw))
}

func headerList(values []string) string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := headerValue(value); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, ", ")
}
