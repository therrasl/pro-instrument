DROP TRIGGER IF EXISTS rental_status_push_notification ON rental_status_history;
DROP FUNCTION IF EXISTS enqueue_rental_push_notification();
DROP TABLE IF EXISTS push_notification_deliveries;
DROP TABLE IF EXISTS push_notification_events;
DROP TABLE IF EXISTS client_push_tokens;
