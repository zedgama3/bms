package config_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func ExampleLoad() {
	cfg, err := config.Load(context.Background(), []string{
		"BMS_DB_URL=postgres://bms:example-password@db.example:5432/bms",
		"BMS_DATA_DIR=testdata/none",
		"BMS_LOG_LEVEL=debug",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(cfg.HTTP.Addr, cfg.Log.Level == slog.LevelDebug, cfg.DB.URL)
	// Output:
	// 127.0.0.1:8080 true [redacted]
}

func ExampleLoad_problems() {
	_, err := config.Load(context.Background(), []string{
		"BMS_DB_MAX_CONNS=0",
		"BMS_LOG_LEVEL=Info",
		"BMS_DB_MAXCONNS=5",
	})
	for _, fe := range validate.Fields(err) {
		fmt.Printf("%s: %s\n", fe.Field, fe.Reason)
	}
	// Output:
	// BMS_DB_URL: required
	// BMS_DB_MAX_CONNS: must be between 1 and 1000, got 0
	// BMS_LOG_LEVEL: must be one of: debug, info, warn, error
	// BMS_DB_MAXCONNS: unknown configuration variable
}

func ExampleKeys() {
	for _, k := range config.Keys()[:3] {
		fmt.Printf("%s %s required=%v secret=%v\n", k.Name, k.Env, k.Required, k.Secret)
	}
	// Output:
	// instance.name BMS_INSTANCE_NAME required=false secret=false
	// db.url BMS_DB_URL required=true secret=true
	// db.max_conns BMS_DB_MAX_CONNS required=false secret=false
}
