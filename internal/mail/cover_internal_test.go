package mail

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type covScript struct {
	hangUpAtGreeting bool
	from             string
	rcpt             string
	data             string
	body             string
}

func covServer(t *testing.T, script covScript) (string, int, func() string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	received := make(chan string, 1)

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			received <- ""
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.SetDeadline(time.Now().Add(10 * time.Second))

		if script.hangUpAtGreeting {
			received <- ""
			return
		}

		write := func(line string) { _, _ = connection.Write([]byte(line + "\r\n")) }
		or := func(value, fallback string) string {
			if value == "" {
				return fallback
			}
			return value
		}

		write("220 localhost ESMTP")
		reader := bufio.NewReader(connection)

		var collected strings.Builder
		inData := false

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			trimmed := strings.TrimRight(line, "\r\n")

			if inData {
				if trimmed == "." {
					inData = false
					write("250 OK")
					continue
				}
				collected.WriteString(trimmed + "\n")
				continue
			}

			switch {
			case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
				write("250-localhost")
				write("250 SIZE 10240000")
			case strings.HasPrefix(trimmed, "MAIL FROM:"):
				write(or(script.from, "250 OK"))
			case strings.HasPrefix(trimmed, "RCPT TO:"):
				write(or(script.rcpt, "250 OK"))
			case trimmed == "DATA":
				write(or(script.data, "354 Go ahead"))
				inData = script.data == ""
			case trimmed == "QUIT":
				write("221 Bye")
				received <- collected.String()
				return
			default:
				write("250 OK")
			}
		}
		received <- collected.String()
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	return "127.0.0.1", port, func() string {
		select {
		case body := <-received:
			return body
		case <-time.After(10 * time.Second):
			t.Error("le serveur de test n'a rien rendu")
			return ""
		}
	}
}

func covMailer(host string, port int, username string) *Mailer {
	return New(Config{
		Driver:   "smtp",
		Host:     host,
		Port:     port,
		Username: username,
		Password: "secret",
		From:     "app@exemple.test",
		Timeout:  5 * time.Second,
	}, zerolog.Nop())
}

func covMessage() Message {
	return Message{To: []string{"client@exemple.test"}, Subject: "Facture", Text: "Bonjour"}
}

func TestSmtpDeliversTheComposedMessage(t *testing.T) {
	host, port, wait := covServer(t, covScript{})

	if err := covMailer(host, port, "").Send(covMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := wait()
	for _, want := range []string{"From: app@exemple.test", "To: client@exemple.test", "Subject: Facture", "Bonjour"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q absent du message recu:\n%s", want, body)
		}
	}
}

func TestSmtpCarriesCcAndReplyTo(t *testing.T) {
	host, port, wait := covServer(t, covScript{})

	message := covMessage()
	message.Cc = []string{"copie@exemple.test"}
	message.ReplyTo = "reponse@exemple.test"

	if err := covMailer(host, port, "").Send(message); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := wait()
	if !strings.Contains(body, "Cc: copie@exemple.test") {
		t.Errorf("Cc absent:\n%s", body)
	}
	if !strings.Contains(body, "Reply-To: reponse@exemple.test") {
		t.Errorf("Reply-To absent:\n%s", body)
	}
}

func TestSmtpKeepsBothBodiesAsMultipart(t *testing.T) {
	host, port, wait := covServer(t, covScript{})

	message := covMessage()
	message.HTML = "<p>Bonjour</p>"

	if err := covMailer(host, port, "").Send(message); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := wait()
	if !strings.Contains(body, "multipart/alternative") {
		t.Errorf("deux corps doivent voyager en multipart:\n%s", body)
	}
	if !strings.Contains(body, "Content-Type: text/plain") {
		t.Errorf("la partie texte a disparu:\n%s", body)
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("la partie HTML a disparu:\n%s", body)
	}
	if !strings.Contains(body, "Bonjour") || !strings.Contains(body, "<p>Bonjour</p>") {
		t.Errorf("un des deux corps est absent:\n%s", body)
	}
}

func TestSmtpClosesEveryMultipartSection(t *testing.T) {
	host, port, wait := covServer(t, covScript{})

	message := covMessage()
	message.HTML = "<p>Bonjour</p>"

	if err := covMailer(host, port, "").Send(message); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := wait()
	boundary := ""
	for _, line := range strings.Split(body, "\n") {
		if _, after, found := strings.Cut(line, "boundary="); found {
			boundary = strings.TrimSpace(after)
		}
	}
	if boundary == "" {
		t.Fatalf("frontiere absente de l'entete:\n%s", body)
	}

	if strings.Count(body, "--"+boundary) != 3 {
		t.Errorf("il faut deux ouvertures et une fermeture de %q:\n%s", boundary, body)
	}
	if !strings.Contains(body, "--"+boundary+"--") {
		t.Errorf("la fermeture finale manque:\n%s", body)
	}
}

func TestSmtpSendsHtmlAloneWhenThereIsNoText(t *testing.T) {
	host, port, wait := covServer(t, covScript{})

	message := covMessage()
	message.Text = ""
	message.HTML = "<p>Bonjour</p>"

	if err := covMailer(host, port, "").Send(message); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := wait()
	if strings.Contains(body, "multipart") {
		t.Errorf("un seul corps ne doit pas devenir multipart:\n%s", body)
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Errorf("type HTML attendu:\n%s", body)
	}
}

func TestMimeBoundaryDiffersEachTime(t *testing.T) {
	if mimeBoundary() == mimeBoundary() {
		t.Error("deux messages ne doivent pas partager la meme frontiere")
	}
}

func TestSmtpSendsPlainTextWhenThereIsNoHtml(t *testing.T) {
	host, port, wait := covServer(t, covScript{})

	if err := covMailer(host, port, "").Send(covMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := wait()
	if !strings.Contains(body, "Content-Type: text/plain") {
		t.Errorf("type texte attendu:\n%s", body)
	}
}

func TestSmtpReportsARefusedSender(t *testing.T) {
	host, port, _ := covServer(t, covScript{from: "550 expediteur refuse"})

	err := covMailer(host, port, "").Send(covMessage())
	if err == nil {
		t.Fatal("un expediteur refuse doit remonter")
	}
	if !strings.Contains(err.Error(), "550") {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestSmtpReportsARefusedRecipient(t *testing.T) {
	host, port, _ := covServer(t, covScript{rcpt: "550 destinataire inconnu"})

	err := covMailer(host, port, "").Send(covMessage())
	if err == nil {
		t.Fatal("un destinataire refuse doit remonter")
	}
	if !strings.Contains(err.Error(), "550") {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestSmtpReportsARefusedData(t *testing.T) {
	host, port, _ := covServer(t, covScript{data: "554 corps refuse"})

	err := covMailer(host, port, "").Send(covMessage())
	if err == nil {
		t.Fatal("un DATA refuse doit remonter")
	}
	if !strings.Contains(err.Error(), "554") {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestSmtpReportsAuthRefusedByAServerWithoutAuth(t *testing.T) {
	host, port, _ := covServer(t, covScript{})

	err := covMailer(host, port, "moi").Send(covMessage())
	if err == nil {
		t.Fatal("une authentification demandee a un serveur qui ne l'annonce pas doit echouer")
	}
}

func TestSmtpReportsAServerThatHangsUp(t *testing.T) {
	host, port, _ := covServer(t, covScript{hangUpAtGreeting: true})

	if err := covMailer(host, port, "").Send(covMessage()); err == nil {
		t.Fatal("un serveur qui raccroche doit remonter une erreur")
	}
}

func TestSmtpReportsAClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	if err := covMailer("127.0.0.1", port, "").Send(covMessage()); err == nil {
		t.Fatal("un port ferme doit remonter une erreur")
	}
}

func TestNewFillsTheDefaults(t *testing.T) {
	mailer := New(Config{}, zerolog.Nop())

	if mailer.Driver() != "log" {
		t.Errorf("driver par defaut = %q, attendu log", mailer.Driver())
	}

	kept := New(Config{Driver: "smtp", Timeout: 2 * time.Second}, zerolog.Nop())
	if kept.Driver() != "smtp" {
		t.Errorf("driver = %q", kept.Driver())
	}
}
