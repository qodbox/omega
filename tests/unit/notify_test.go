package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"omega/internal/broadcast"
	"omega/internal/mail"
	"omega/internal/notify"
)

type stubNotification struct {
	channels  []string
	message   mail.Message
	mailed    int
	broadcast int
}

func (s *stubNotification) Channels() []string { return s.channels }

func (s *stubNotification) ToMail() mail.Message {
	s.mailed++
	return s.message
}

func (s *stubNotification) ToBroadcast() (string, string, any) {
	s.broadcast++
	return "orders", "created", map[string]string{"id": "1"}
}

type plainNotification struct{ channels []string }

func (p plainNotification) Channels() []string { return p.channels }

func testMailer() *mail.Mailer {
	return mail.New(mail.Config{Driver: "log", From: "omega@test"}, zerolog.Nop())
}

func validMessage() mail.Message {
	return mail.Message{To: []string{"user@test"}, Subject: "Hi", Text: "Body"}
}

func TestNotifyDeliversOnEveryDeclaredChannel(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := &stubNotification{channels: []string{"mail", "broadcast"}, message: validMessage()}

	if err := notify.New(testMailer(), hub).Send(context.Background(), note); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if note.mailed != 1 {
		t.Errorf("ToMail appele %d fois, attendu 1", note.mailed)
	}
	if note.broadcast != 1 {
		t.Errorf("ToBroadcast appele %d fois, attendu 1", note.broadcast)
	}
}

func TestNotifyOnlyUsesDeclaredChannels(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := &stubNotification{channels: []string{"mail"}, message: validMessage()}

	if err := notify.New(testMailer(), hub).Send(context.Background(), note); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if note.broadcast != 0 {
		t.Errorf("broadcast non declare mais appele %d fois", note.broadcast)
	}
}

func TestNotifySkipsChannelTheNotificationCannotServe(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := plainNotification{channels: []string{"mail", "broadcast"}}

	if err := notify.New(testMailer(), hub).Send(context.Background(), note); err != nil {
		t.Fatalf("une notification sans ToMail ni ToBroadcast doit etre ignoree, pas echouer: %v", err)
	}
}

func TestNotifySurvivesMissingServices(t *testing.T) {
	note := &stubNotification{channels: []string{"mail", "broadcast"}, message: validMessage()}

	if err := notify.New(nil, nil).Send(context.Background(), note); err != nil {
		t.Fatalf("Send sans mailer ni hub: %v", err)
	}
	if note.mailed != 0 || note.broadcast != 0 {
		t.Errorf("services absents mais canaux servis (mail=%d broadcast=%d)", note.mailed, note.broadcast)
	}
}

func TestNotifyReturnsMailFailure(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := &stubNotification{channels: []string{"mail"}, message: mail.Message{Subject: "Sans destinataire"}}

	err := notify.New(testMailer(), hub).Send(context.Background(), note)
	if !errors.Is(err, mail.ErrNoRecipient) {
		t.Fatalf("erreur du mailer attendue, obtenu %v", err)
	}
}

func TestNotifyStopsAtFirstFailure(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := &stubNotification{
		channels: []string{"mail", "broadcast"},
		message:  mail.Message{Subject: "Sans destinataire"},
	}

	if err := notify.New(testMailer(), hub).Send(context.Background(), note); err == nil {
		t.Fatal("erreur attendue")
	}
	if note.broadcast != 0 {
		t.Errorf("le canal suivant a ete servi malgre l'echec du precedent (%d fois)", note.broadcast)
	}
}

func TestNotifyIgnoresUnknownChannel(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := &stubNotification{channels: []string{"email", "sms"}, message: validMessage()}

	if err := notify.New(testMailer(), hub).Send(context.Background(), note); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if note.mailed != 0 || note.broadcast != 0 {
		t.Errorf("un canal inconnu ne doit rien declencher (mail=%d broadcast=%d)", note.mailed, note.broadcast)
	}
}

func TestNotifyWithoutChannelDoesNothing(t *testing.T) {
	hub := broadcast.New()
	defer hub.Shutdown()

	note := &stubNotification{message: validMessage()}

	if err := notify.New(testMailer(), hub).Send(context.Background(), note); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if note.mailed != 0 {
		t.Errorf("aucun canal declare mais ToMail appele %d fois", note.mailed)
	}
}
