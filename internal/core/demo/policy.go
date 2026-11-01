package demo

import "github.com/zedgama3/bms/internal/core/files"

// FileCeiling is the demo's upload caps as a files.Policy: MaxUploadBytes per file,
// MaxQuotaBytes site-wide (also applied when the quota setting is unlimited) and
// MaxPerRecord files per record. app passes it as files.Options.Ceiling in demo mode, and
// files lowers its settings-backed policy to it, never raises it. The caps are constants,
// not settings, so a demo settings-admin cannot lift them. Callers must not modify it.
var FileCeiling = files.Policy{MaxBytes: MaxUploadBytes, QuotaBytes: MaxQuotaBytes, MaxPerRecord: MaxPerRecord}
