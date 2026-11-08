CREATE INDEX reminders_by_status_updated ON reminders (status, updated_at);

INSERT INTO meta (k, v)
SELECT 'reminder_counts', json_object(
  'queued', (SELECT COUNT(*) FROM reminders WHERE status = 'queued'),
  'claimed', (SELECT COUNT(*) FROM reminders WHERE status = 'claimed'),
  'failed24h', (SELECT COUNT(*) FROM reminders WHERE status = 'failed' AND updated_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 day')),
  'expired24h', (SELECT COUNT(*) FROM reminders WHERE status = 'expired' AND updated_at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 day'))
)
WHERE true
ON CONFLICT(k) DO UPDATE SET v = excluded.v;
