package mail

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

type capture struct {
	mu       sync.Mutex
	from     string
	to       []string
	body     strings.Builder
	finished chan struct{}
}

func fakeSMTP(t *testing.T) (host string, port int, seen *capture) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	seen = &capture{finished: make(chan struct{})}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

		write("220 localhost ESMTP")
		inData := false

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")

			if inData {
				if trimmed == "." {
					inData = false
					write("250 OK")
					continue
				}
				seen.mu.Lock()
				seen.body.WriteString(trimmed + "\n")
				seen.mu.Unlock()
				continue
			}

			switch {
			case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
				write("250-localhost")
				write("250 SIZE 10240000")
			case strings.HasPrefix(trimmed, "MAIL FROM:"):
				seen.mu.Lock()
				seen.from = trimmed
				seen.mu.Unlock()
				write("250 OK")
			case strings.HasPrefix(trimmed, "RCPT TO:"):
				seen.mu.Lock()
				seen.to = append(seen.to, trimmed)
				seen.mu.Unlock()
				write("250 OK")
			case trimmed == "DATA":
				inData = true
				write("354 Go ahead")
			case trimmed == "QUIT":
				write("221 Bye")
				close(seen.finished)
				return
			default:
				write("250 OK")
			}
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, seen
}

func TestSmtpDriverDeliversTheWholeMessage(t *testing.T) {
	host, port, seen := fakeSMTP(t)

	mailer := New(Config{
		Driver: "smtp",
		Host:   host,
		Port:   port,
		From:   "app@exemple.test",
	}, zerolog.Nop())

	err := mailer.Send(Message{
		To:      []string{"cible@exemple.test"},
		Cc:      []string{"copie@exemple.test"},
		ReplyTo: "reponse@exemple.test",
		Subject: "Bonjour",
		HTML:    "<p>corps</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	<-seen.finished

	seen.mu.Lock()
	defer seen.mu.Unlock()

	if !strings.Contains(seen.from, "app@exemple.test") {
		t.Errorf("MAIL FROM = %q", seen.from)
	}
	if len(seen.to) != 2 {
		t.Errorf("RCPT TO = %v — le Cc doit etre dans l'enveloppe", seen.to)
	}

	body := seen.body.String()
	for _, part := range []string{"Subject: Bonjour", "Reply-To: reponse@exemple.test", "text/html", "<p>corps</p>"} {
		if !strings.Contains(body, part) {
			t.Errorf("le message ne contient pas %q:\n%s", part, body)
		}
	}
}

func TestSmtpDriverReportsAnUnreachableServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	mailer := New(Config{
		Driver: "smtp",
		Host:   "127.0.0.1",
		Port:   port,
		From:   "app@exemple.test",
	}, zerolog.Nop())

	if err := mailer.Send(Message{To: []string{"cible@exemple.test"}}); err == nil {
		t.Fatal("un serveur injoignable n'a produit aucune erreur sur le port " + strconv.Itoa(port))
	}
}
