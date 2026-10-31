package telemetry

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// The OTEL_* variables this package reads and validates (Behavior: "What is read from the
// environment"). The SDK reads the rest itself.
const (
	envSDKDisabled   = "OTEL_SDK_DISABLED"
	envEndpoint      = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envProtocol      = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envSampler       = "OTEL_TRACES_SAMPLER"
	envSamplerArg    = "OTEL_TRACES_SAMPLER_ARG"
	envServiceName   = "OTEL_SERVICE_NAME"
	envResourceAttrs = "OTEL_RESOURCE_ATTRIBUTES"

	protocolHTTPProtobuf = "http/protobuf"

	samplerAlwaysOn  = "always_on"
	samplerAlwaysOff = "always_off"
	samplerRatio     = "traceidratio"
)

// signalNames are the three signals, in the order they are built.
var signalNames = [...]string{"traces", "metrics", "logs"}

// envConfig is what the environment decided.
type envConfig struct {
	endpoints   map[string]string // signal -> full URL; absent: the signal does not export
	sampler     string
	ratio       float64
	serviceName string
	resource    map[string]string
}

// fieldErr builds an Invalid error holding one validate.FieldError.
func fieldErr(op, field, code, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Reason: reason})
}

func fieldErrParams(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// readEnv reads and validates the OTEL_* variables through get (F6–F9). Every problem is
// collected, each naming its variable; no reason echoes a value an operator typed.
func readEnv(get func(string) (string, bool)) (envConfig, error) {
	const op = "telemetry.New"
	acc := validate.Errors{Op: op}
	lookup := func(k string) string {
		v, ok := get(k)
		if !ok {
			return ""
		}
		return strings.TrimSpace(v)
	}
	cfg := envConfig{endpoints: map[string]string{}, sampler: samplerAlwaysOn, ratio: 1}

	disabled := false
	if v := lookup(envSDKDisabled); v != "" {
		switch strings.ToLower(v) {
		case "true":
			disabled = true
		case "false":
		default:
			acc.Add(fieldErrParams(op, envSDKDisabled, validate.CodeOneOf,
				map[string]string{"options": "true, false"}, "must be true or false"))
		}
	}

	protocolVars := []string{envProtocol}
	for _, s := range signalNames {
		protocolVars = append(protocolVars, "OTEL_EXPORTER_OTLP_"+strings.ToUpper(s)+"_PROTOCOL")
	}
	for _, name := range protocolVars {
		if v := lookup(name); v != "" && v != protocolHTTPProtobuf {
			acc.Add(fieldErrParams(op, name, validate.CodeOneOf,
				map[string]string{"options": protocolHTTPProtobuf}, "only http/protobuf is supported"))
		}
	}

	base := lookup(envEndpoint)
	baseOK := false
	if base != "" {
		if err := validate.URL(envEndpoint, base, "http", "https"); err != nil {
			acc.Add(err)
		} else {
			baseOK = true
		}
	}
	for _, s := range signalNames {
		name := "OTEL_EXPORTER_OTLP_" + strings.ToUpper(s) + "_ENDPOINT"
		if v := lookup(name); v != "" {
			if err := validate.URL(name, v, "http", "https"); err != nil {
				acc.Add(err)
			} else if !disabled {
				cfg.endpoints[s] = v
			}
			continue
		}
		if baseOK && !disabled {
			cfg.endpoints[s] = strings.TrimRight(base, "/") + "/v1/" + s
		}
	}

	if v := lookup(envSampler); v != "" {
		switch v {
		case samplerAlwaysOn, samplerAlwaysOff:
			cfg.sampler = v
		case samplerRatio:
			cfg.sampler = v
			arg := lookup(envSamplerArg)
			f, err := strconv.ParseFloat(arg, 64)
			if arg == "" || err != nil || math.IsNaN(f) || f < 0 || f > 1 {
				acc.Add(fieldErrParams(op, envSamplerArg, validate.CodeRange,
					map[string]string{"min": "0", "max": "1"}, "traceidratio needs a number between 0 and 1"))
			} else {
				cfg.ratio = f
			}
		default:
			acc.Add(fieldErrParams(op, envSampler, validate.CodeOneOf,
				map[string]string{"options": "always_on, always_off, traceidratio"},
				"unsupported sampler; must be one of: always_on, always_off, traceidratio"))
		}
	}

	cfg.serviceName = lookup(envServiceName)
	cfg.resource = parseResourceAttrs(lookup(envResourceAttrs))
	if err := acc.Err(); err != nil {
		return envConfig{}, fmt.Errorf("telemetry: environment: %w", err)
	}
	return cfg, nil
}

// parseResourceAttrs parses OTEL_RESOURCE_ATTRIBUTES ("k1=v1,k2=v2", values
// percent-decoded) as the SDK does: a malformed pair is skipped.
func parseResourceAttrs(s string) map[string]string {
	out := map[string]string{}
	if s == "" {
		return out
	}
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(pair, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		if dec, err := url.PathUnescape(strings.TrimSpace(v)); err == nil {
			v = dec
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}
