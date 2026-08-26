package notify

import (
	"context"

	"omega/internal/broadcast"
	"omega/internal/mail"
)

type Notification interface {
	Channels() []string
}

type MailChannel interface {
	ToMail() mail.Message
}

type BroadcastChannel interface {
	ToBroadcast() (channel string, event string, data any)
}

type Notifier struct {
	mailer *mail.Mailer
	hub    *broadcast.Hub
}

func New(mailer *mail.Mailer, hub *broadcast.Hub) *Notifier {
	return &Notifier{mailer: mailer, hub: hub}
}

func (n *Notifier) Send(ctx context.Context, notification Notification) error {
	for _, channel := range notification.Channels() {
		switch channel {
		case "mail":
			sender, ok := notification.(MailChannel)
			if !ok || n.mailer == nil {
				continue
			}
			if err := n.mailer.Send(sender.ToMail()); err != nil {
				return err
			}
		case "broadcast":
			caster, ok := notification.(BroadcastChannel)
			if !ok || n.hub == nil {
				continue
			}
			name, event, data := caster.ToBroadcast()
			n.hub.Publish(name, event, data)
		}
	}
	return nil
}
