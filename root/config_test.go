package root

import "testing"

func TestLoadConfigReadsOCRArgumentVector(t *testing.T) {
	lookup := map[string]string{
		"PORT_SERVER":     "7002",
		"DATABASE_URL":    "postgres://user:pass@localhost/db",
		"BASE_URL":        "http://localhost:7010",
		"MAIL_ADDR":       "localhost:7025",
		"ARTIFACT_ROOT":   "./var/artifacts",
		"OCR_BINARY":      "/usr/local/bin/tesseract",
		"OCR_ARGS_JSON":   `["--psm","6","{input}","stdout"]`,
		"SCHEDULER_BATCH": "0",
	}
	cfg, _, err := loadConfig(func(key string) (string, bool) {
		value, ok := lookup[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OCRArgs) != 4 || cfg.OCRArgs[0] != "--psm" || cfg.OCRArgs[1] != "6" || cfg.OCRArgs[2] != "{input}" || cfg.OCRArgs[3] != "stdout" {
		t.Fatalf("unexpected OCR args: %v", cfg.OCRArgs)
	}
}

func TestLoadConfigRejectsMalformedOCRArgumentVector(t *testing.T) {
	lookup := map[string]string{
		"PORT_SERVER":   "7002",
		"DATABASE_URL":  "postgres://user:pass@localhost/db",
		"BASE_URL":      "http://localhost:7010",
		"MAIL_ADDR":     "localhost:7025",
		"ARTIFACT_ROOT": "./var/artifacts",
		"OCR_ARGS_JSON": `--psm 6 {input} stdout`,
	}
	_, _, err := loadConfig(func(key string) (string, bool) {
		value, ok := lookup[key]
		return value, ok
	})
	if err == nil {
		t.Fatal("malformed OCR_ARGS_JSON must fail configuration")
	}
}

func TestLoadConfigReadsAssistanceArgumentVector(t *testing.T) {
	lookup := map[string]string{
		"PORT_SERVER":          "7002",
		"DATABASE_URL":         "postgres://user:pass@localhost/db",
		"BASE_URL":             "http://localhost:7010",
		"MAIL_ADDR":            "localhost:7025",
		"ARTIFACT_ROOT":        "./var/artifacts",
		"ASSISTANCE_BINARY":    "/usr/local/bin/overwatch-assistance",
		"ASSISTANCE_ARGS_JSON": `["--input","{input}"]`,
	}
	cfg, _, err := loadConfig(func(key string) (string, bool) {
		value, ok := lookup[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AssistanceArgs) != 2 || cfg.AssistanceArgs[0] != "--input" || cfg.AssistanceArgs[1] != "{input}" {
		t.Fatalf("unexpected assistance args: %v", cfg.AssistanceArgs)
	}
}

func TestLoadConfigReadsSynthesisArgumentVector(t *testing.T) {
	lookup := map[string]string{
		"PORT_SERVER":         "7002",
		"DATABASE_URL":        "postgres://user:pass@localhost/db",
		"BASE_URL":            "http://localhost:7010",
		"MAIL_ADDR":           "localhost:7025",
		"ARTIFACT_ROOT":       "./var/artifacts",
		"SYNTHESIS_BINARY":    "/usr/local/bin/overwatch-synthesis",
		"SYNTHESIS_ARGS_JSON": `["--input","{input}"]`,
	}
	cfg, _, err := loadConfig(func(key string) (string, bool) {
		value, ok := lookup[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SynthesisArgs) != 2 || cfg.SynthesisArgs[0] != "--input" || cfg.SynthesisArgs[1] != "{input}" {
		t.Fatalf("unexpected synthesis args: %v", cfg.SynthesisArgs)
	}
}
