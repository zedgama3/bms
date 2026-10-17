// Package signals is the in-process publish/subscribe mechanism modules react to each other
// through (D9, D19, D24). A signal is published inside the use case that made the change
// and delivered to its subscribers after that change is committed. It imports lib, model,
// core/db (the after-commit hook) and core/telemetry; core/module imports it for Name.
//
// A payload holds record references and nothing else, so no personal data can travel on a
// signal by construction. Delivery is synchronous, on the publisher's goroutine, in
// subscription order, each handler under its own deadline; a handler's failure is logged,
// counted and traced and never reaches the publisher. Nothing is durable before delivery.
package signals
