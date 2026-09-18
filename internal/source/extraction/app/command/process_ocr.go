package command

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	"github.com/0xsj/overwatch-backend/pkg/execx"
)

const OCRInputPlaceholder = "{input}"

type ProcessImageOCRConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessImageOCR adapts an installed OCR executable without invoking a shell.
// Args may contain {input}; if omitted, the private input path is prepended.
// The default argument shape is compatible with tesseract's stdout mode:
// `{input} stdout`.
type ProcessImageOCR struct {
	config ProcessImageOCRConfig
}

func NewProcessImageOCR(config ProcessImageOCRConfig) ProcessImageOCR {
	if len(config.Args) == 0 {
		config.Args = []string{OCRInputPlaceholder, "stdout"}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = domain.MaxTextBytes
	}
	return ProcessImageOCR{config: config}
}

func (p ProcessImageOCR) Extract(ctx context.Context, retained RetainedCapture) (string, error) {
	if p.config.Binary == "" {
		return "", ErrOCRUnavailable
	}
	input, err := os.CreateTemp("", "overwatch-ocr-")
	if err != nil {
		return "", fmt.Errorf("prepare OCR input: %w", err)
	}
	path := input.Name()
	defer os.Remove(path)
	if _, err := input.Write(retained.Bytes); err != nil {
		_ = input.Close()
		return "", fmt.Errorf("write OCR input: %w", err)
	}
	if err := input.Close(); err != nil {
		return "", fmt.Errorf("close OCR input: %w", err)
	}

	args := append([]string(nil), p.config.Args...)
	found := false
	for index, arg := range args {
		if arg == OCRInputPlaceholder {
			args[index] = path
			found = true
		}
	}
	if !found {
		args = append([]string{path}, args...)
	}
	argv := append([]string{p.config.Binary}, args...)
	result, err := execx.Spawn(ctx, argv, execx.Policy{Timeout: p.config.Timeout, MaxOutput: p.config.MaxOutput})
	if err != nil || result.Outcome == execx.Unavailable {
		if result.Reason != "" {
			return "", fmt.Errorf("%w: %s", ErrOCRUnavailable, result.Reason)
		}
		return "", fmt.Errorf("%w: %v", ErrOCRUnavailable, err)
	}
	if result.Outcome == execx.TimedOut {
		return "", fmt.Errorf("OCR process timed out: %s", result.Reason)
	}
	if result.StdoutTruncated {
		return "", fmt.Errorf("OCR process output exceeded %d bytes", p.config.MaxOutput)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("OCR process exited with code %d", result.ExitCode)
	}
	return string(result.Stdout), nil
}
