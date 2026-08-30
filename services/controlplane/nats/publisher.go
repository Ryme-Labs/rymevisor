package nats

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

type EventPublisher struct {
	js jetstream.JetStream
}

func NewEventPublisher(js jetstream.JetStream) *EventPublisher {
	return &EventPublisher{js: js}
}

func (p *EventPublisher) Publish(ctx context.Context, subject string, data []byte) error {
	_, err := p.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("nats: publish to %s: %w", subject, err)
	}
	return nil
}

func (p *EventPublisher) PublishVMEvent(ctx context.Context, eventType, vmID string, data []byte) error {


	subject := fmt.Sprintf("events.vm.%s.%s", vmID, eventType)
	return p.Publish(ctx, subject, data)
}

func (p *EventPublisher) PublishNodeEvent(ctx context.Context, eventType, nodeID string, data []byte) error {

	subject := fmt.Sprintf("events.node.%s.%s", nodeID, eventType)
	return p.Publish(ctx, subject, data)
}
