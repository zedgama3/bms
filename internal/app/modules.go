package app

// The modules compiled into this binary (app.md decision 15). A fork adds a module with
// one blank import here, and one line in web/src/modules/index.ts. Only this package
// imports modules.
import (
	_ "github.com/zedgama3/bms/internal/modules/hello"      // registers the module in init (module.Register)
	_ "github.com/zedgama3/bms/internal/modules/volunteers" // registers the module in init (module.Register)
)
