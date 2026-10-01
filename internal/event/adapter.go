// Package event defines the event abstraction decoupling character index
// writes from their transport, as described in docs/sync.md §4.
//
// Producers publish Events; an EventAdapter delivers them to subscribed
// handlers. Two transports ship out of the box: a synchronous in-process
// adapter (internal/event/http, the v0.1 behavior) and Redis Streams
// (internal/event/redis).
package event

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// EventType identifies a character lifecycle event (docs/sync.md §2).
type EventType string

const (
	// EventCharacterCreated fires when a character enters the index.
	EventCharacterCreated EventType = "character.created"
	// EventCharacterUpdated fires on partial updates (level up, rename, ...).
	EventCharacterUpdated EventType = "character.updated"
	// EventCharacterDeleted fires when a character leaves the index.
	EventCharacterDeleted EventType = "character.deleted"
	// EventCharacterMoved fires when a character migrates between servers.
	EventCharacterMoved EventType = "character.moved"
	// EventCharacterLogin fires on login, refreshing last_login_at.
	EventCharacterLogin EventType = "character.login"
)

// Topics are the transport-level channels adapters route events through.
// The Redis adapter maps a topic to a stream name; the HTTP adapter keys
// its in-process handlers by topic.
const (
	// TopicCharacters carries all character.* events.
	TopicCharacters = "atlas.characters"
)

// ErrNoSubscriber is returned by adapters when an event is published to a
// topic nobody subscribed to.
var ErrNoSubscriber = errors.New("event: no subscriber for topic")

// Event is the unit of data flow between game servers and the Atlas
// character index. Field presence is significant: omitted optional fields
// mean "no change" for updated events.
type Event struct {
	// ID is the transport-assigned identifier (e.g. Redis stream entry ID).
	// Producers may leave it empty; adapters fill it in on delivery.
	ID string `json:"id,omitempty"`

	// Type is the event kind ("character.created", ...).
	Type EventType `json:"event"`

	AccountID   int64  `json:"account_id,omitempty"`
	ServerID    string `json:"server_id,omitempty"`
	CharacterID int64  `json:"character_id"`

	// TargetServerID is the destination server for moved events.
	TargetServerID string `json:"target_server,omitempty"`

	// Payload fields. Level/ClassID are pointers so updated events can
	// distinguish "not present" from zero.
	Name     string             `json:"name,omitempty"`
	Level    *int               `json:"level,omitempty"`
	ClassID  *int               `json:"class_id,omitempty"`
	Avatar   *string            `json:"avatar,omitempty"`
	Metadata *map[string]string `json:"metadata,omitempty"`

	// Timestamp is the producer-side occurrence time.
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// TopicFor maps an event type to the topic carrying it. Unknown types have
// no topic and yield an error.
func TopicFor(t EventType) (string, error) {
	switch t {
	case EventCharacterCreated, EventCharacterUpdated, EventCharacterDeleted,
		EventCharacterMoved, EventCharacterLogin:
		return TopicCharacters, nil
	default:
		return "", fmt.Errorf("event: unknown type %q", t)
	}
}

// Handler processes a single event. Returning an error signals the adapter
// to retry or dead-letter per its delivery semantics (at-least-once for
// Redis Streams, exactly-once in-process for HTTP).
type Handler func(ctx context.Context, e *Event) error

// EventAdapter abstracts the message transport behind character index
// writes (docs/sync.md §4).
type EventAdapter interface {
	// Name returns the adapter identifier ("http", "redis", ...).
	Name() string

	// Synchronous reports whether Publish applies the event before
	// returning. Synchronous adapters preserve the v0.1 request/response
	// contract; asynchronous ones only guarantee enqueue.
	Synchronous() bool

	// Publish hands an event to the transport. For synchronous adapters
	// the handler has finished (including its error) when Publish returns.
	Publish(ctx context.Context, e *Event) error

	// Subscribe registers h for all events on topic. For asynchronous
	// adapters Subscribe starts background consumption governed by ctx.
	Subscribe(ctx context.Context, topic string, h Handler) error

	// Ack explicitly confirms an event. Synchronous adapters are always
	// acknowledged; Redis adapters ack via the consumer group.
	Ack(ctx context.Context, e *Event) error

	// Close releases transport resources. It does not wait for in-flight
	// handlers.
	Close() error
}
