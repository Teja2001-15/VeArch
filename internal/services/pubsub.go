package services

import (
	"sync"
)

// EventType identifies the kind of Kanban event being broadcast.
type EventType string

const (
	EventCardMoved   EventType = "card_moved"
	EventCardCreated EventType = "card_created"
	EventCardUpdated EventType = "card_updated"
	EventCardDeleted EventType = "card_deleted"
)

// KanbanEvent is the payload published to all board subscribers.
type KanbanEvent struct {
	Type      EventType `json:"type"`
	ProjectID string    `json:"project_id"`
	TaskID    string    `json:"task_id"`
}

// Hub manages per-project subscriber channels for server-sent event broadcasting.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string][]chan KanbanEvent
}

// NewHub creates and returns an initialised Hub.
func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string][]chan KanbanEvent),
	}
}

// Subscribe registers a new listener for the given project room.
// It returns the channel to read events from and a cleanup function that
// the caller MUST invoke (typically via defer) when it is done listening.
func (h *Hub) Subscribe(projectID string) (chan KanbanEvent, func()) {
	ch := make(chan KanbanEvent, 16)

	h.mu.Lock()
	h.rooms[projectID] = append(h.rooms[projectID], ch)
	h.mu.Unlock()

	cleanup := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		subs := h.rooms[projectID]
		for i, c := range subs {
			if c == ch {
				h.rooms[projectID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(h.rooms[projectID]) == 0 {
			delete(h.rooms, projectID)
		}
		close(ch)
	}

	return ch, cleanup
}

// Publish sends an event to every subscriber in the given project room.
// Slow consumers are skipped (non-blocking send) to avoid blocking the publisher.
func (h *Hub) Publish(projectID string, event KanbanEvent) {
	h.mu.RLock()
	subs := make([]chan KanbanEvent, len(h.rooms[projectID]))
	copy(subs, h.rooms[projectID])
	h.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Subscriber too slow — drop the event rather than block
		}
	}
}
