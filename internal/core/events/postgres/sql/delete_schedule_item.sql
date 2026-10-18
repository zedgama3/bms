-- Store.DeleteScheduleItem: RemoveScheduleItem. Use Exec; RowsAffected 0 means NotFound
-- (unknown, or an item of another event: F74). Positions are not renumbered: they only break
-- ties, and gaps are harmless.
-- Args: $1 event_id uuid, $2 id uuid.
DELETE FROM core.event_schedule_item
WHERE event_id = $1 AND id = $2
