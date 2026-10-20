-- Store.ListOverrides: every override, without its text, for ListTemplates (the language
-- chips and the "not available" group of names no process registers). Read-only; use Query.
-- Not paginated: one row per overridden (template, language), bounded by the registered
-- templates times the languages (tens of rows).
-- Args: none.
-- Columns, in order, sorted by name, language: name text, language text, updated_at
-- timestamptz.
SELECT name, language, updated_at
FROM core.notify_template
ORDER BY name, language
