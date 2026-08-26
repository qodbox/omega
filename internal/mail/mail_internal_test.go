package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestComposeRefusesHeaderInjection(t *testing.T) {
	head := compose(Message{
		From:    "app@example.test",
		To:      []string{"cible@example.test"},
		Subject: "Bonjour\r\nBcc: pirate@example.test",
		Text:    "corps",
	})

	headers, _, _ := strings.Cut(head, "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("en-tete injecte via le sujet:\n%s", headers)
		}
	}
}

func TestComposeRefusesInjectionThroughRecipients(t *testing.T) {
	head := compose(Message{
		From:    "app@example.test",
		To:      []string{"cible@example.test\r\nBcc: pirate@example.test"},
		Subject: "Bonjour",
		Text:    "corps",
	})

	headers, _, _ := strings.Cut(head, "\r\n\r\n")
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("en-tete injecte via un destinataire:\n%s", headers)
		}
	}

	if err := (&Mailer{cfg: Config{Driver: "smtp", From: "a@b.test"}}).Send(Message{
		To: []string{"cible@example.test\r\nBcc: pirate@example.test"},
	}); !errors.Is(err, ErrUnsafeAddress) {
		t.Fatalf("adresse avec saut de ligne acceptee: %v", err)
	}
}

func TestComposeKeepsTheBodyIntact(t *testing.T) {
	head := compose(Message{
		From: "app@example.test",
		To:   []string{"cible@example.test"},
		Text: "ligne une\r\nligne deux",
	})

	_, body, found := strings.Cut(head, "\r\n\r\n")
	if !found || body != "ligne une\r\nligne deux" {
		t.Fatalf("le corps a ete altere: %q", body)
	}
}
