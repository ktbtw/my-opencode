-- Phase 5 runbook only. Do not execute until ARCHIVE_HIGH_FREQ_EVENTS=0 has
-- been stable for a full release and agent-history fallback is verified.
-- Run in small batches during a low-traffic window.

-- 1. Count candidates
SELECT event_type, COUNT(*) AS cnt, MIN(sent_at) AS oldest, MAX(sent_at) AS newest
FROM task_events
WHERE event_type IN ('delta', 'progress')
  AND sent_at < UTC_TIMESTAMP() - INTERVAL 7 DAY
GROUP BY event_type;

-- 2. Repeat until row count is 0
-- DELETE FROM task_events
-- WHERE event_type IN ('delta', 'progress')
--   AND sent_at < UTC_TIMESTAMP() - INTERVAL 7 DAY
-- LIMIT 5000;
