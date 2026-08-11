// Package webhooks dispatches project events to subscriber URLs (spec §2.1
// `webhooks` table) with signed payloads and a retry/backoff policy for
// transient failures (timeouts, 5xx).
package webhooks

// Event type constants follow the `<ressource>.<verbe>` convention shared
// with global_logs.action (spec §2.1 note).
//
// EventContentUpdate fires for every kind of content mutation -- create,
// edit, delete, publish, unpublish -- rather than one distinct event per
// verb. A webhook subscriber (almost always: "rebuild the site") cares
// about exactly one thing -- "something about this project's content
// changed" -- not which specific CRUD verb caused it, so there's nothing
// to pick between and nothing to get out of sync: this is what broke a
// real deployment (see the migration note in pkg/storage's
// migrateWebhookDeliveryColumns-adjacent history) when a webhook was
// configured to expect a granular event name a workflow's trigger filter
// didn't actually cover. global_logs.action keeps the granular verb
// (content.create/update/delete/publish/unpublish) for the audit trail,
// where the distinction still matters -- only the webhook-dispatch side
// collapses to one event.
const (
	EventContentUpdate = "content.update"
	EventMediaCreate   = "media.create"
	EventMediaDelete   = "media.delete"
)
