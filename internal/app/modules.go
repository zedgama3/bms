package app

// The modules compiled into this binary (app.md decision 15). A fork adds a module with
// one blank import here, and one line in web/src/modules/index.ts:
//
//	_ "github.com/zedgama3/bms/internal/modules/hello"
//
// Only this package imports modules. The hello module is added when it is built
// (module-hello.md).
