package bus

import "github.com/google/uuid"

// BroadcastForTenant broadcasts an event. The tenantID parameter is retained
// for call-site compatibility but is unused in v4 single-user mode.
func BroadcastForTenant(pub EventPublisher, name string, _ uuid.UUID, payload any) {
	pub.Broadcast(Event{Name: name, Payload: payload})
}
