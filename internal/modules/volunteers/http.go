package volunteers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// The handlers are thin: parse the path and query, call the service, write JSON, and on
// error hand it to httpx.Fail. None checks a permission, starts a goroutine or changes state
// on GET.

// pathID parses the path value name as an ID of type T.
func pathID[T ~[16]byte](r *http.Request, name string) (T, error) {
	id, err := model.ParseID[T](r.PathValue(name))
	if err != nil {
		var zero T
		return zero, fieldProblem("volunteers.pathID", name, validate.CodeMalformed, "must be a UUID", nil)
	}
	return id, nil
}

// queryID parses the optional query value name as an ID of type T (zero when absent).
func queryID[T ~[16]byte](r *http.Request, name string) (T, error) {
	var zero T
	v := r.URL.Query().Get(name)
	if v == "" {
		return zero, nil
	}
	id, err := model.ParseID[T](v)
	if err != nil {
		return zero, fieldProblem("volunteers.queryID", name, validate.CodeMalformed, "must be a UUID", nil)
	}
	return id, nil
}

// queryBool parses the optional query value name ("true"/"false"/"1"/"0").
func queryBool(r *http.Request, name string) (bool, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fieldProblem("volunteers.queryBool", name, validate.CodeMalformed, "must be true or false", nil)
	}
	return b, nil
}

// queryTime parses the optional query value name as RFC 3339.
func queryTime(r *http.Request, name string) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, fieldProblem("volunteers.queryTime", name, validate.CodeMalformed, "must be an RFC 3339 time", nil)
	}
	return t, nil
}

// queryPage parses the limit and after query values. The cursor is not decoded here: the
// service validates it (Invalid malformed on "after").
func queryPage(r *http.Request) (vol.Page, error) {
	var p vol.Page
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return vol.Page{}, fieldProblem("volunteers.queryPage", "limit", validate.CodeMalformed, "must be a number", nil)
		}
		p.Limit = n
	}
	p.After = vol.Cursor(r.URL.Query().Get("after"))
	return p, nil
}

// reply writes v as JSON with status, or fails the request with err.
func reply(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

// noContent answers 204, or fails the request with err.
func noContent(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handler adapts a func to http.Handler.
func handler(f func(w http.ResponseWriter, r *http.Request)) http.Handler { return http.HandlerFunc(f) }

// decode reads the JSON body into T; on failure it fails the request and reports false.
func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var in T
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	return in, true
}

// ok fails the request with err (when set) and reports whether to continue.
func ok(w http.ResponseWriter, r *http.Request, err error) bool {
	if err != nil {
		httpx.Fail(w, r, err)
		return false
	}
	return true
}

// Routes are relative to /api/volunteers (module-volunteers.md, "Routes"). The module's
// permissions are checked in the service, never here.
func (s *Service) Routes() []module.Route {
	in := module.AccessSignedIn
	rt := func(method, pattern string, f func(w http.ResponseWriter, r *http.Request)) module.Route {
		return module.Route{Method: method, Pattern: pattern, Access: in, Handler: handler(f)}
	}
	upload := rt("POST", "/me/qualifications", s.hUploadCertificate)
	upload.MaxBodyBytes, upload.Timeout = files.UploadCeiling, files.UploadTimeout
	cert := rt("GET", "/qualification-records/{record}/certificate", s.hOpenCertificate)
	cert.Timeout = files.ServeTimeout
	return []module.Route{
		rt("GET", "/qualifications", s.hListQualifications),
		rt("POST", "/qualifications", s.hCreateQualification),
		rt("PUT", "/qualifications/{qualification}", s.hUpdateQualification),
		rt("POST", "/qualifications/{qualification}/archive", s.hArchiveQualification),
		rt("GET", "/people/{person}/qualifications", s.hRecordsOf),
		rt("POST", "/people/{person}/qualifications", s.hRecordQualification),
		upload,
		rt("GET", "/qualification-records", s.hPendingRecords),
		rt("POST", "/qualification-records/{record}/verify", s.hVerifyRecord),
		rt("POST", "/qualification-records/{record}/reject", s.hRejectRecord),
		rt("DELETE", "/qualification-records/{record}", s.hRemoveRecord),
		cert,
		rt("GET", "/departments/{department}/positions", s.hListPositions),
		rt("POST", "/departments/{department}/positions", s.hCreatePosition),
		rt("PUT", "/positions/{position}", s.hUpdatePosition),
		rt("POST", "/positions/{position}/archive", s.hArchivePosition),
		rt("PUT", "/positions/{position}/requirements", s.hSetRequirements),
		rt("GET", "/departments/{department}/members", s.hListMembers),
		rt("POST", "/departments/{department}/members", s.hAddMember),
		rt("DELETE", "/departments/{department}/members/{person}", s.hRemoveMember),
		rt("GET", "/me/departments", s.hMyDepartments),
		rt("DELETE", "/me/departments/{department}", s.hLeave),
		rt("GET", "/shifts", s.hListShifts),
		rt("GET", "/shifts/{shift}", s.hGetShift),
		rt("POST", "/departments/{department}/shifts", s.hCreateShift),
		rt("PUT", "/shifts/{shift}", s.hUpdateShift),
		rt("POST", "/shifts/{shift}/cancel", s.hCancelShift),
		rt("POST", "/departments/{department}/shifts/copy", s.hCopyShifts),
		rt("POST", "/shifts/{shift}/signups", s.hSignUp),
		rt("POST", "/shifts/{shift}/assign", s.hAssign),
		rt("DELETE", "/signups/{signup}", s.hCancelSignup),
		rt("GET", "/shifts/{shift}/roster", s.hRoster),
		rt("GET", "/me/schedule", s.hMySchedule),
		rt("GET", "/me/listing", s.hListed),
		rt("PUT", "/me/listing", s.hSetListed),
	}
}

func (s *Service) hListQualifications(w http.ResponseWriter, r *http.Request) {
	dept, err := queryID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	archived, err := queryBool(r, "archived")
	if !ok(w, r, err) {
		return
	}
	items, err := s.ListQualifications(r.Context(), vol.QualificationQuery{Department: dept, IncludeArchived: archived})
	reply(w, r, http.StatusOK, vol.QualificationList{Items: orEmpty(items)}, err)
}

func (s *Service) hCreateQualification(w http.ResponseWriter, r *http.Request) {
	in, good := decode[vol.QualificationInput](w, r)
	if !good {
		return
	}
	out, err := s.CreateQualification(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hUpdateQualification(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.QualificationID](r, "qualification")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.QualificationInput](w, r)
	if !good {
		return
	}
	out, err := s.UpdateQualification(r.Context(), id, in)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hArchiveQualification(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.QualificationID](r, "qualification")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.ArchiveQualification(r.Context(), id))
}

func (s *Service) hRecordsOf(w http.ResponseWriter, r *http.Request) {
	p, err := pathID[model.PersonID](r, "person")
	if !ok(w, r, err) {
		return
	}
	list, err := s.RecordsOf(r.Context(), p)
	reply(w, r, http.StatusOK, list, err)
}

func (s *Service) hRecordQualification(w http.ResponseWriter, r *http.Request) {
	p, err := pathID[model.PersonID](r, "person")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.RecordInput](w, r)
	if !good {
		return
	}
	in.Person = p
	out, err := s.RecordQualification(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

// parseDateField parses an optional YYYY-MM-DD form field.
func parseDateField(field, v string) (vol.Date, error) {
	if v == "" {
		return vol.Date{}, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return vol.Date{}, fieldProblem("volunteers.parseDate", field, validate.CodeMalformed, "must be YYYY-MM-DD", nil)
	}
	return vol.DateOf(t, time.UTC), nil
}

func (s *Service) hUploadCertificate(w http.ResponseWriter, r *http.Request) {
	mp, err := files.ReadUpload(r, "file")
	if !ok(w, r, err) {
		return
	}
	in := vol.UploadInput{Upload: mp}
	if v := mp.Fields["qualification"]; v != "" {
		if in.Qualification, err = model.ParseID[vol.QualificationID](v); err != nil {
			httpx.Fail(w, r, fieldProblem("volunteers.upload", "qualification", validate.CodeMalformed, "must be a UUID", nil))
			return
		}
	}
	if in.AcquiredOn, err = parseDateField("acquired_on", mp.Fields["acquired_on"]); !ok(w, r, err) {
		return
	}
	if in.ExpiresOn, err = parseDateField("expires_on", mp.Fields["expires_on"]); !ok(w, r, err) {
		return
	}
	out, err := s.UploadCertificate(r.Context(), in)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hPendingRecords(w http.ResponseWriter, r *http.Request) {
	dept, err := queryID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	items, err := s.PendingRecords(r.Context(), vol.PendingQuery{Department: dept})
	reply(w, r, http.StatusOK, vol.RecordList{Items: orEmpty(items)}, err)
}

func (s *Service) hVerifyRecord(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.RecordID](r, "record")
	if !ok(w, r, err) {
		return
	}
	var in vol.VerifyInput
	if r.ContentLength != 0 {
		var good bool
		if in, good = decode[vol.VerifyInput](w, r); !good {
			return
		}
	}
	out, err := s.VerifyRecord(r.Context(), id, in)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hRejectRecord(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.RecordID](r, "record")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.RejectRecord(r.Context(), id))
}

func (s *Service) hRemoveRecord(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.RecordID](r, "record")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.RemoveRecord(r.Context(), id))
}

func (s *Service) hOpenCertificate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.RecordID](r, "record")
	if !ok(w, r, err) {
		return
	}
	f, c, err := s.OpenCertificate(r.Context(), id)
	if !ok(w, r, err) {
		return
	}
	defer closeContent(r.Context(), s.logger, c)
	files.Serve(w, r, f, c)
}

func (s *Service) hListPositions(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	archived, err := queryBool(r, "archived")
	if !ok(w, r, err) {
		return
	}
	items, err := s.ListPositions(r.Context(), dept, archived)
	reply(w, r, http.StatusOK, vol.PositionList{Items: orEmpty(items)}, err)
}

func (s *Service) hCreatePosition(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.PositionInput](w, r)
	if !good {
		return
	}
	out, err := s.CreatePosition(r.Context(), dept, in)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hUpdatePosition(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.PositionID](r, "position")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.PositionInput](w, r)
	if !good {
		return
	}
	out, err := s.UpdatePosition(r.Context(), id, in)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hArchivePosition(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.PositionID](r, "position")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.ArchivePosition(r.Context(), id))
}

func (s *Service) hSetRequirements(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.PositionID](r, "position")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.RequirementsInput](w, r)
	if !good {
		return
	}
	out, err := s.SetRequirements(r.Context(), id, in.Qualifications)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hListMembers(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	q := vol.MemberQuery{Department: dept}
	if q.Page, err = queryPage(r); !ok(w, r, err) {
		return
	}
	list, err := s.ListMembers(r.Context(), q)
	reply(w, r, http.StatusOK, list, err)
}

func (s *Service) hAddMember(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.MemberInput](w, r)
	if !good {
		return
	}
	out, err := s.AddMember(r.Context(), dept, in.Person)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hRemoveMember(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	p, err := pathID[model.PersonID](r, "person")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.RemoveMember(r.Context(), dept, p))
}

func (s *Service) hMyDepartments(w http.ResponseWriter, r *http.Request) {
	items, err := s.MyDepartments(r.Context())
	reply(w, r, http.StatusOK, vol.MemberList{Items: orEmpty(items)}, err)
}

func (s *Service) hLeave(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.Leave(r.Context(), dept))
}

func (s *Service) hListShifts(w http.ResponseWriter, r *http.Request) {
	var q vol.ShiftQuery
	var err error
	if q.Department, err = queryID[model.DepartmentID](r, "department"); !ok(w, r, err) {
		return
	}
	if q.Event, err = queryID[model.EventID](r, "event"); !ok(w, r, err) {
		return
	}
	if q.Position, err = queryID[vol.PositionID](r, "position"); !ok(w, r, err) {
		return
	}
	if q.Within.Start, err = queryTime(r, "from"); !ok(w, r, err) {
		return
	}
	if q.Within.End, err = queryTime(r, "to"); !ok(w, r, err) {
		return
	}
	if q.IncludeCanceled, err = queryBool(r, "canceled"); !ok(w, r, err) {
		return
	}
	if q.EligibleOnly, err = queryBool(r, "eligible"); !ok(w, r, err) {
		return
	}
	if q.Page, err = queryPage(r); !ok(w, r, err) {
		return
	}
	list, err := s.ListShifts(r.Context(), q)
	reply(w, r, http.StatusOK, list, err)
}

func (s *Service) hGetShift(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.ShiftID](r, "shift")
	if !ok(w, r, err) {
		return
	}
	out, err := s.GetShift(r.Context(), id)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hCreateShift(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.ShiftInput](w, r)
	if !good {
		return
	}
	out, err := s.CreateShift(r.Context(), dept, in)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hUpdateShift(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.ShiftID](r, "shift")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.ShiftInput](w, r)
	if !good {
		return
	}
	out, err := s.UpdateShift(r.Context(), id, in)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hCancelShift(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.ShiftID](r, "shift")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.CancelShift(r.Context(), id))
}

func (s *Service) hCopyShifts(w http.ResponseWriter, r *http.Request) {
	dept, err := pathID[model.DepartmentID](r, "department")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.CopyInput](w, r)
	if !good {
		return
	}
	n, err := s.CopyShifts(r.Context(), dept, in.From, in.To)
	reply(w, r, http.StatusOK, vol.CopyResult{Created: n}, err)
}

func (s *Service) hSignUp(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.ShiftID](r, "shift")
	if !ok(w, r, err) {
		return
	}
	out, err := s.SignUp(r.Context(), id)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hAssign(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.ShiftID](r, "shift")
	if !ok(w, r, err) {
		return
	}
	in, good := decode[vol.AssignInput](w, r)
	if !good {
		return
	}
	out, err := s.Assign(r.Context(), id, in)
	reply(w, r, http.StatusCreated, out, err)
}

func (s *Service) hCancelSignup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.SignupID](r, "signup")
	if !ok(w, r, err) {
		return
	}
	noContent(w, r, s.CancelSignup(r.Context(), id))
}

func (s *Service) hRoster(w http.ResponseWriter, r *http.Request) {
	id, err := pathID[vol.ShiftID](r, "shift")
	if !ok(w, r, err) {
		return
	}
	out, err := s.Roster(r.Context(), id)
	reply(w, r, http.StatusOK, out, err)
}

func (s *Service) hMySchedule(w http.ResponseWriter, r *http.Request) {
	var q vol.ScheduleQuery
	var err error
	if q.Within.Start, err = queryTime(r, "from"); !ok(w, r, err) {
		return
	}
	if q.Within.End, err = queryTime(r, "to"); !ok(w, r, err) {
		return
	}
	if q.Past, err = queryBool(r, "past"); !ok(w, r, err) {
		return
	}
	if q.Page, err = queryPage(r); !ok(w, r, err) {
		return
	}
	list, err := s.MySchedule(r.Context(), q)
	reply(w, r, http.StatusOK, list, err)
}

func (s *Service) hListed(w http.ResponseWriter, r *http.Request) {
	listed, err := s.Listed(r.Context())
	reply(w, r, http.StatusOK, vol.Listing{Listed: listed}, err)
}

func (s *Service) hSetListed(w http.ResponseWriter, r *http.Request) {
	in, good := decode[vol.ListingInput](w, r)
	if !good {
		return
	}
	if err := s.SetListed(r.Context(), in.Listed); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	reply(w, r, http.StatusOK, vol.Listing(in), nil)
}

// closeContent closes a file's content after it was served; a failure is logged (no name).
func closeContent(ctx context.Context, log *slog.Logger, c io.Closer) {
	if err := c.Close(); err != nil {
		log.WarnContext(ctx, "a served certificate could not be closed")
	}
}
