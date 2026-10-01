package http

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihairu/atlas/internal/event"
)

func TestSynchronousDispatch(t *testing.T) {
	a := New()
	ctx := context.Background()
	if err := a.Subscribe(ctx, event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		// Mutation proves the handler ran before Publish returned.
		e.ID = "applied"
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	e := &event.Event{Type: event.EventCharacterCreated, CharacterID: 1}
	if err := a.Publish(ctx, e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if e.ID != "applied" {
		t.Fatalf("handler did not run synchronously")
	}
	if !a.Synchronous() || a.Name() != "http" {
		t.Fatalf("expected synchronous http adapter")
	}
}

func TestPublishErrorPropagates(t *testing.T) {
	a := New()
	want := errors.New("boom")
	_ = a.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, _ *event.Event) error {
		return want
	})

	if err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterLogin, CharacterID: 1}); !errors.Is(err, want) {
		t.Fatalf("expected handler error, got %v", err)
	}
}

func TestPublishWithoutSubscriber(t *testing.T) {
	a := New()
	err := a.Publish(context.Background(), &event.Event{Type: event.EventCharacterDeleted, CharacterID: 1})
	if err == nil {
		t.Fatal("expected ErrNoSubscriber, got nil")
	}
}

func TestAckAndCloseAreNoop(t *testing.T) {
	a := New()
	ctx := context.Background()
	if err := a.Ack(ctx, &event.Event{}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
