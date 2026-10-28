package session

import frameworkevent "github.com/RedHuang-0622/Seele/event"

// SessionIDFromEvent returns the session identity carried by the framework
// event's agent.runtime location. It is the single criterion shared by event
// persistence and observation readers.
func SessionIDFromEvent(event frameworkevent.Event) string {
	return SessionIDFromLocations(event.Locations)
}

// SessionIDFromLocations is the location-slice form of SessionIDFromEvent.
func SessionIDFromLocations(locations []frameworkevent.Location) string {
	for _, location := range locations {
		if location.Kind != "agent.runtime" {
			continue
		}
		if sessionID := location.IDs["session_id"]; sessionID != "" {
			return sessionID
		}
	}
	return ""
}
