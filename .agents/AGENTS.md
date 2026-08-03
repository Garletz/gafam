# Deployment Rules

- **Cloudflare Frontend**: To deploy the web interface to Cloudflare, you MUST compile the SvelteKit project first. Execute `npm run build && npx wrangler deploy` from within the `frontend/` directory. Do not pass a project name explicitly.

# Sync Architecture (SMS / MMS / Contacts)

- **Contacts**: `ContactSync.kt` (APK) pushes a full snapshot → `syncContactsHandler` (api.go) upserts + deletes missing (authoritative replace). Triggers: pairing, toggle, service start, periodic 30 min in `RelayForegroundService`, and `ContentObserver` on `ContactsContract`.
- **SMS inbound**: `SmsReceiver`/`SmsDeliverReceiver` → `POST /api/auth/sms/`. SMS sent from native apps are caught by a `ContentObserver` on `content://sms` → `SmsHistorySync`.
- **SMS outbound (web)**: outbox poll 1s → `SmsManager` with a sent `PendingIntent` → `SmsSentReceiver` reports to `POST /api/auth/sms/status` (marks `gafam_sms.status` = sent/failed, deletes outbox row). In-flight guard in the service prevents re-sends. 45 s fallback assumes sent.
- **MMS**: `MmsSync.kt` reads `content://mms(/part)` → `POST /api/auth/mms/sync` (media as base64). Web: `GET /api/web/mms` + `/api/web/mms/part/{id}` (encrypted base64 → blob URL, click-to-load). RCS media is NOT covered (stays in Google Messages — see manifest 16).
- **RCS text**: `EmailNotificationListener` also watches Google/Samsung Messages notifications (MessagingStyle, last incoming message) and relays text as inbound SMS with `channel: "rcs"`.

