//go:build e2ehang

package app

// This file is compiled into the binary only with `-tags e2ehang` (cmd/bms stays a
// one-liner: the module lives here, with the module list). It registers a module
// whose Stop never returns, so TestBinary_SecondSignal_Terminates can prove that a second
// SIGINT or SIGTERM ends a process stuck in shutdown (app.md F27, decision 1). A release
// build never contains it.

import (
	"context"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/model"
)

func init() {
	module.Register(module.Manifest{Name: "zqhang"}, func(context.Context, module.Deps) (module.Module, error) {
		return hangModule{}, nil
	})
}

type hangModule struct{}

func (hangModule) Start(context.Context) error { return nil }

// Stop ignores its context and blocks for good.
func (hangModule) Stop(context.Context) error { select {} }

func (hangModule) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (hangModule) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}
