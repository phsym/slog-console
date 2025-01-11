package console

import (
	"bytes"
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
	headerBuf := &enc.headerBuf
	middleBuf := &enc.middleBuf
	trailerBuf := &enc.trailerBuf

	enc.writeTimestamp(headerBuf, rec.Time)
	enc.writeLevel(headerBuf, rec.Level)

	headerLen := headerBuf.Len()
	if h.opts.AddSource {
		enc.writeSource(headerBuf, rec.PC, cwd)
	}

	enc.writeMessage(middleBuf, rec.Level, rec.Message)

	middleBuf.copy(&h.context)

	headers := h.headers
	localHeaders := false
	rec.Attrs(func(a slog.Attr) bool {
		idx := slices.IndexFunc(h.opts.Headers, func(s string) bool { return s == a.Key })
		if idx >= 0 {
			if !localHeaders {
				localHeaders = true
				headers = append(enc.headers, h.headers...)
			}
			headers[idx] = a
			return true
		}

		offset := middleBuf.Len()
		enc.writeAttr(middleBuf, a, h.groupPrefix)

		// check if the last attr written has newlines in it
		// if so, move it to the trailerBuf
		lastAttr := (*middleBuf)[offset:]
		if bytes.IndexByte(lastAttr, '\n') >= 0 {
			// todo: consider splitting the key and the value
			// components, so the `key=` can be printed on its
			// own line, and the value will not share any of its
			// lines with anything else.  Like:
			//
			// INF msg key1=val1
			// key2=
			// val2 line 1
			// val2 line 2
			// key3=
			// val3 line 1
			// val3 line 2
			//
			// and maybe consider printing the key for these values
			// differently, like:
			//
			// === key2 ===
			// val2 line1
			// val2 line2
			// === key3 ===
			// val3 line 1
			// val3 line 2
			//
			// Splitting the key and value doesn't work up here in
			// Handle() though, because we don't know where the term
			// control characters are.  Would need to push this
			// multiline handling deeper into encoder, or pass
			// offsets back up from writeAttr()
			//
			// if k, v, ok := bytes.Cut(lastAttr, []byte("=")); ok {
			// trailerBuf.AppendString("=== ")
			// trailerBuf.Append(k[1:])
			// trailerBuf.AppendString(" ===\n")
			// trailerBuf.AppendByte('=')
			// trailerBuf.AppendByte('\n')
			// trailerBuf.AppendString("---------------------\n")
			// trailerBuf.Append(v)
			// trailerBuf.AppendString("\n---------------------\n")
			// trailerBuf.AppendByte('\n')
			// } else {
			// trailerBuf.Append(lastAttr[1:])
			// trailerBuf.AppendByte('\n')
			// }
			trailerBuf.Append(lastAttr)

			// rewind the middle buffer
			*middleBuf = (*middleBuf)[:offset]
		}
		return true
	})

	// add additional headers after the source
	if len(headers) > 0 {
		enc.writeHeaders(headerBuf, headers)
	}

	// connect the sections
	if headerBuf.Len() > headerLen {
		enc.writeHeaderSeparator(headerBuf)
	}

	if trailerBuf.Len() == 0 {
		// if there were no multiline attrs, terminate the line with a newline
		enc.NewLine(middleBuf)
	} else {
		// if there were multiline attrs, write middle <-> trailer separater
		enc.NewLine(trailerBuf)
	}

	if _, err := headerBuf.WriteTo(h.out); err != nil {
		return err
	}
	if _, err := middleBuf.WriteTo(h.out); err != nil {
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
