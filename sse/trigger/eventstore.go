package sse

import (
	"container/list"
	"sync"
	"time"

	"github.com/project-flogo/core/support/log"
)

// EventStore manages event storage and replay functionality
type EventStore struct {
	maxSize    int
	ttl        time.Duration
	events     *list.List
	eventIndex map[string]*list.Element
	mutex      sync.RWMutex
	logger     log.Logger
}

// StoredEvent represents an event stored in the event store
type StoredEvent struct {
	Event     *SSEEvent
	Timestamp time.Time
}

// NewEventStore creates a new event store
func NewEventStore(maxSize int, ttl time.Duration, logger log.Logger) *EventStore {
	store := &EventStore{
		maxSize:    maxSize,
		ttl:        ttl,
		events:     list.New(),
		eventIndex: make(map[string]*list.Element),
		logger:     logger,
	}

	// Start cleanup routine
	go store.cleanupRoutine()

	return store
}

// AddEvent adds an event to the store
func (es *EventStore) AddEvent(event *SSEEvent) {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	// Create stored event
	storedEvent := &StoredEvent{
		Event:     event,
		Timestamp: time.Now(),
	}

	// Add to list
	element := es.events.PushBack(storedEvent)

	// Add to index if event has ID
	if event.ID != "" {
		es.eventIndex[event.ID] = element
	}

	// Remove oldest events if we exceed max size
	for es.events.Len() > es.maxSize {
		es.removeOldest()
	}
}

// GetEventsSince returns events since the specified event ID
func (es *EventStore) GetEventsSince(lastEventID string) []*SSEEvent {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	var events []*SSEEvent

	if lastEventID == "" {
		// Return all events if no last event ID
		for e := es.events.Front(); e != nil; e = e.Next() {
			if storedEvent, ok := e.Value.(*StoredEvent); ok {
				events = append(events, storedEvent.Event)
			}
		}
		return events
	}

	// Find the element with the last event ID
	element, exists := es.eventIndex[lastEventID]
	if !exists {
		// If event ID not found, return all events
		for e := es.events.Front(); e != nil; e = e.Next() {
			if storedEvent, ok := e.Value.(*StoredEvent); ok {
				events = append(events, storedEvent.Event)
			}
		}
		return events
	}

	// Return events after the specified event ID
	for e := element.Next(); e != nil; e = e.Next() {
		if storedEvent, ok := e.Value.(*StoredEvent); ok {
			events = append(events, storedEvent.Event)
		}
	}

	return events
}

// GetAllEvents returns all stored events
func (es *EventStore) GetAllEvents() []*SSEEvent {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	var events []*SSEEvent
	for e := es.events.Front(); e != nil; e = e.Next() {
		if storedEvent, ok := e.Value.(*StoredEvent); ok {
			events = append(events, storedEvent.Event)
		}
	}

	return events
}

// GetEventCount returns the number of stored events
func (es *EventStore) GetEventCount() int {
	es.mutex.RLock()
	defer es.mutex.RUnlock()
	return es.events.Len()
}

// Clear removes all stored events
func (es *EventStore) Clear() {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	es.events.Init()
	es.eventIndex = make(map[string]*list.Element)
}

// removeOldest removes the oldest event from the store
func (es *EventStore) removeOldest() {
	if es.events.Len() == 0 {
		return
	}

	// Get the front element
	element := es.events.Front()
	if element == nil {
		return
	}

	// Remove from index if event has ID
	if storedEvent, ok := element.Value.(*StoredEvent); ok {
		if storedEvent.Event.ID != "" {
			delete(es.eventIndex, storedEvent.Event.ID)
		}
	}

	// Remove from list
	es.events.Remove(element)
}

// cleanupRoutine periodically removes expired events
func (es *EventStore) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Minute) // Run cleanup every minute
	defer ticker.Stop()

	for range ticker.C {
		es.cleanupExpiredEvents()
	}
}

// cleanupExpiredEvents removes events that have exceeded their TTL
func (es *EventStore) cleanupExpiredEvents() {
	if es.ttl <= 0 {
		return // No TTL configured
	}

	es.mutex.Lock()
	defer es.mutex.Unlock()

	now := time.Now()
	var toRemove []*list.Element

	// Find expired events
	for e := es.events.Front(); e != nil; e = e.Next() {
		if storedEvent, ok := e.Value.(*StoredEvent); ok {
			if now.Sub(storedEvent.Timestamp) > es.ttl {
				toRemove = append(toRemove, e)
			} else {
				// Since events are added chronologically, we can break here
				break
			}
		}
	}

	// Remove expired events
	for _, element := range toRemove {
		if storedEvent, ok := element.Value.(*StoredEvent); ok {
			if storedEvent.Event.ID != "" {
				delete(es.eventIndex, storedEvent.Event.ID)
			}
		}
		es.events.Remove(element)
	}

	if len(toRemove) > 0 {
		es.logger.Debugf("Cleaned up %d expired events from event store", len(toRemove))
	}
}
