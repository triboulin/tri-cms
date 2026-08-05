// Package webhooks dispatches project events to subscriber URLs (spec §2.1
// `webhooks` table) with signed payloads and a retry/backoff policy for
// transient failures (timeouts, 5xx).
package webhooks

// Event type constants follow the `<ressource>.<verbe>` convention shared
// with global_logs.action (spec §2.1 note).
const (
	EventContentCreate    = "content.create"
	EventContentUpdate    = "content.update"
	EventContentDelete    = "content.delete"
	EventContentPublish   = "content.publish"
	EventContentUnpublish = "content.unpublish"
	EventMediaCreate      = "media.create"
	EventMediaDelete      = "media.delete"
)
