package config_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// config.md F34: files.store other than disk or s3 (postgres included) is Invalid under
// BMS_FILES_STORE, with the one_of code, from Load and from Validate.
func TestLoad_FilesStoreUnknown_ReturnsInvalid(t *testing.T) {
	for _, v := range []string{"postgres", "Disk", "S3", "gcs", marker} {
		t.Run("load/"+v, func(t *testing.T) {
			_, err := load(t, env("BMS_FILES_STORE="+v)...)
			f := wantInvalidOne(t, err, "BMS_FILES_STORE")
			if f.Code != validate.CodeOneOf {
				t.Errorf("code = %q, want %q", f.Code, validate.CodeOneOf)
			}
			noLeak(t, err, marker)
		})
	}
	for _, v := range []config.FilesStore{"postgres", "", "gcs"} {
		t.Run("validate/"+string(v), func(t *testing.T) {
			c := valid()
			c.Files.Store = v
			err := c.Validate()
			if !errs.Is(err, errs.Invalid) || findField(err, "BMS_FILES_STORE") == nil {
				t.Fatalf("Validate = %v (fields %v), want Invalid under BMS_FILES_STORE", err, fieldNames(err))
			}
		})
	}
	if _, err := load(t, env("BMS_FILES_STORE=disk")...); err != nil {
		t.Errorf("Load with store disk: %v", err)
	}
}

// config.md F35: with files.store s3, each unset endpoint, bucket, access key and secret
// key is its own FieldError naming the variable.
func TestValidate_FilesS3MissingKeys_ReportsEachVariable(t *testing.T) {
	all := []string{"BMS_FILES_S3_ENDPOINT", "BMS_FILES_S3_BUCKET", "BMS_FILES_S3_ACCESS_KEY", "BMS_FILES_S3_SECRET_KEY"}

	t.Run("all missing", func(t *testing.T) {
		c := valid()
		c.Files.Store = config.FilesS3
		err := c.Validate()
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("Validate = %v, want Invalid", err)
		}
		for _, name := range all {
			if findField(err, name) == nil {
				t.Errorf("no FieldError for %s (got %v)", name, fieldNames(err))
			}
		}
	})
	t.Run("all missing via Load", func(t *testing.T) {
		_, err := load(t, env("BMS_FILES_STORE=s3")...)
		if !errs.Is(err, errs.Invalid) {
			t.Fatalf("Load = %v, want Invalid", err)
		}
		for _, name := range all {
			if findField(err, name) == nil {
				t.Errorf("no FieldError for %s (got %v)", name, fieldNames(err))
			}
		}
	})
	t.Run("only the missing one", func(t *testing.T) {
		full := []string{
			"BMS_FILES_STORE=s3",
			"BMS_FILES_S3_ENDPOINT=https://s3.example.test",
			"BMS_FILES_S3_BUCKET=bms-files",
			"BMS_FILES_S3_ACCESS_KEY=ak-" + marker,
			"BMS_FILES_S3_SECRET_KEY=sk-" + marker,
		}
		if _, err := load(t, env(full...)...); err != nil {
			t.Fatalf("complete s3 config: %v", err)
		}
		for i, name := range all {
			rest := append(append([]string{}, full[:i+1]...), full[i+2:]...)
			_, err := load(t, env(rest...)...)
			requireInvalidOne(t, err, name)
			noLeak(t, err, marker)
		}
	})
	t.Run("disk ignores unset s3 keys", func(t *testing.T) {
		if _, err := load(t, env("BMS_FILES_STORE=disk")...); err != nil {
			t.Fatalf("Load: %v", err)
		}
	})
}
