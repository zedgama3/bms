-- Store.CountScheduleItems: for the MaxScheduleItems check before an insert. Run after
-- get_event_for_update.sql so two inserts cannot both pass. Use QueryRow.
-- Args: $1 event_id uuid. Columns: n bigint.
SELECT count(*) FROM core.event_schedule_item WHERE event_id = $1
