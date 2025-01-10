package console

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

var cwd, _ = os.Getwd()

// HandlerOptions are options for a ConsoleHandler.
// A zero HandlerOptions consists entirely of default values.
// ReplaceAttr works identically to [slog.HandlerOptions.ReplaceAttr]
type HandlerOptions struct {
	// AddSource causes the handler to compute the source code position
	// of the log statement and add a SourceKey attribute to the output.
	AddSource bool

	// Level reports the minimum record level that will be logged.
	// The handler discards records with lower levels.
	// If Level is nil, the handler assumes LevelInfo.
	// The handler calls Level.Level for each record processed;
	// to adjust the minimum level dynamically, use a LevelVar.
	Level slog.Leveler

	// Disable colorized output
	NoColor bool

	// TimeFormat is the format used for time.DateTime
	TimeFormat string

	// Theme defines the colorized output using ANSI escape sequences
	Theme Theme

	// ReplaceAttr is called to rewrite each non-group attribute before it is logged.
	// See [slog.HandlerOptions]
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

type Handler struct {
	opts        HandlerOptions
	out         io.Writer
	groupPrefix string
	groups      []string
	context     buffer
}

var _ slog.Handler = (*Handler)(nil)

// NewHandler creates a Handler that writes to w,
// using the given options.
// If opts is nil, the default options are used.
func NewHandler(out io.Writer, opts *HandlerOptions) *Handler {
	if opts == nil {
		opts = new(HandlerOptions)
	}
	if opts.Level == nil {
		opts.Level = slog.LevelInfo
	}
	if opts.TimeFormat == "" {
		opts.TimeFormat = time.DateTime
	}
	if opts.Theme == nil {
		opts.Theme = NewDefaultTheme()
	}
	return &Handler{
		opts:        *opts, // Copy struct
		out:         out,
		groupPrefix: "",
		context:     nil,
	}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.opts.Level.Level()
}

// Handle implements slog.Handler.
func (h *Handler) Handle(_ context.Context, rec slog.Record) error {
	enc := newEncoder(h)
	buf := &enc.buf

	enc.writeTimestamp(buf, rec.Time)
	enc.writeLevel(buf, rec.Level)
	if h.opts.AddSource {
		enc.writeSource(buf, rec.PC, cwd)
	}
	enc.writeMessage(buf, rec.Level, rec.Message)
	buf.copy(&h.context)
	rec.Attrs(func(a slog.Attr) bool {
		enc.writeAttr(buf, a, h.groupPrefix)
		return true
	})
	enc.NewLine(buf)
	if _, err := buf.WriteTo(h.out); err != nil {
		return err
	}

	enc.free()
	return nil
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newCtx := h.context
	enc := newEncoder(h)
	for _, a := range attrs {
		enc.writeAttr(&newCtx, a, h.groupPrefix)
	}
	newCtx.Clip()
	return &Handler{
		opts:        h.opts,
		out:         h.out,
		groupPrefix: h.groupPrefix,
		context:     newCtx,
		groups:      h.groups,
	}
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	name = strings.TrimSpace(name)
	groupPrefix := name
	if h.groupPrefix != "" {
		groupPrefix = h.groupPrefix + "." + name
	}
	return &Handler{
		opts:        h.opts,
		out:         h.out,
		groupPrefix: groupPrefix,
		context:     h.context,
		groups:      append(h.groups, name),
	}
}
