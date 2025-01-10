package console

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
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

	// Headers are a list of attribute keys.  These attributes will be removed from
	// the trailing attr list, and the values will be inserted between
	// the level/source and the message, in the configured order.
	Headers []string

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
	headers     []slog.Attr
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
		headers:     make([]slog.Attr, len(opts.Headers)),
	}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.opts.Level.Level()
}

// Handle implements slog.Handler.
func (h *Handler) Handle(_ context.Context, rec slog.Record) error {
	enc := newEncoder(h)
	headerBuf := &enc.buf
	trailerBuf := &enc.trailerBuf

	enc.writeTimestamp(headerBuf, rec.Time)
	enc.writeLevel(headerBuf, rec.Level)

	var writeHeaderSeparator bool
	if h.opts.AddSource {
		writeHeaderSeparator = enc.writeSource(headerBuf, rec.PC, cwd)
	}

	enc.writeMessage(trailerBuf, rec.Level, rec.Message)

	trailerBuf.copy(&h.context)

	headers := h.headers
	headersChanged := false
	rec.Attrs(func(a slog.Attr) bool {
		idx := slices.IndexFunc(h.opts.Headers, func(s string) bool { return s == a.Key })
		if idx >= 0 {
			if !headersChanged {
				headersChanged = true
				// todo: I think should could be replace now by a preallocated slice in encoder, avoiding allocation
				// todo: this makes one allocation, but only if the headers weren't already
				// satisfied by prior WithAttrs().  Could use a pool of *[]slog.Value, but
				// I'm not sure it's worth it.
				headers = make([]slog.Attr, len(h.opts.Headers))
				copy(headers, h.headers)
			}
			headers[idx] = a
			return true
		}
		enc.writeAttr(trailerBuf, a, h.groupPrefix)
		return true
	})
	enc.NewLine(trailerBuf)

	if len(headers) > 0 {
		if enc.writeHeaders(headerBuf, headers) {
			writeHeaderSeparator = true
		}
	}

	if writeHeaderSeparator {
		enc.writeHeaderSeparator(headerBuf)
	}

	if _, err := headerBuf.WriteTo(h.out); err != nil {
		return err
	}
	if _, err := trailerBuf.WriteTo(h.out); err != nil {
		return err
	}
	enc.free()
	return nil
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	headers := h.extractHeaders(attrs)
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
		headers:     headers,
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
		headers:     h.headers,
	}
}

// extractHeaders scans the attributes for keys specified in Headers.
// If found, their values are saved in a new list.
// The original attribute list will be modified to remove the extracted attributes.
func (h *Handler) extractHeaders(attrs []slog.Attr) (headers []slog.Attr) {
	changed := false
	headers = h.headers
	for i, attr := range attrs {
		idx := slices.IndexFunc(h.opts.Headers, func(s string) bool { return s == attr.Key })
		if idx >= 0 {
			if !changed {
				// make a copy of prefixes:
				headers = make([]slog.Attr, len(h.headers))
				copy(headers, h.headers)
			}
			headers[idx] = attr
			attrs[i] = slog.Attr{} // remove the prefix attribute
			changed = true
		}
	}
	return
}
