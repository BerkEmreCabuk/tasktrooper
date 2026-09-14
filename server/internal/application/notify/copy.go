package notify

import (
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The user-facing copy for the resume push, kept here rather than at the send
// site for the same reason notifiedColumns and columnLabels are kept at the top
// of notify.go: these are sentences a human reads, and they should be editable
// without going near the delivery code.
//
// Turkish, unlocalized, because the iOS client is Turkish-only. That is a fact
// about the client, not a decision this package is making — when a second
// locale exists these become a switch on the device's language, exactly like
// prompt.ClarificationAckMessage.

// pushTypeTaskResumed is the `type` key the iOS client routes on. It is a wire
// contract with a specific screen: an unrecognised type is displayed but opens
// nothing, so this string may not be changed without changing the client.
const pushTypeTaskResumed = "task.resumed"

// taskResumedTitle names the card that started moving again. The key leads
// because it is what the user recognises in a notification stack.
func taskResumedTitle(taskKey string) string {
	return fmt.Sprintf("%s devam ediyor", taskKey)
}

// taskResumedBody says what freed up and that the task is already going again.
//
// Every variant ends the same way — "görev kaldığı yerden sürüyor" — because the
// single most important thing this notification conveys is that nothing is being
// asked of the reader. A park is the one state where a user might reasonably
// think they have to intervene, and they never do: a sweeper released it.
func taskResumedBody(resource string) string {
	switch resource {
	case domain.ResourceClaudeCodeQuota:
		return "Claude kullanım limiti sıfırlandı, görev kaldığı yerden sürüyor."
	case domain.ResourceMobileDevice:
		return "Beklediği test cihazı boşaldı, görev kaldığı yerden sürüyor."
	case domain.ResourceDeployWatch:
		return "Beklediği deploy tamamlandı, görev kaldığı yerden sürüyor."
	case domain.ResourceWorkOrder:
		return "Önce bitmesi gereken işler tamamlandı, görev kaldığı yerden sürüyor."
	default:
		// A resource added later, or one this build does not know. Saying
		// nothing specific is better than saying the wrong thing, and the
		// notification is still worth sending: the task IS moving again.
		return "Beklediği kaynak hazır, görev kaldığı yerden sürüyor."
	}
}
