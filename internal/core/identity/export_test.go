package identity

// SetDummyHook makes s call f once per dummy verification of a reset request, so a test can
// count them. Not safe to call while requests are in flight.
func SetDummyHook(s *Service, f func()) { s.dummyHook = f }
