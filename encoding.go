package console

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var encoderPool = &sync.Pool{
	New: func() any {
		e := new(encoder)
		e.groups = make([]string, 0, 10)
		e.headerBuf = make(buffer, 0, 1024)
		e.middleBuf = make(buffer, 0, 1024)
		e.trailerBuf = make(buffer, 0, 1024)
		e.headers = make([]slog.Attr, 0, 6)
		return e
	},
}

type encoder struct {
	h                                *Handler
	headerBuf, middleBuf, trailerBuf buffer
	groups                           []string
	headers                          []slog.Attr
}

func newEncoder(h *Handler) *encoder {
	e := encoderPool.Get().(*encoder)
	e.h = h
	if h.opts.ReplaceAttr != nil {
		e.groups = append(e.groups, h.groups...)
	}
	return e
}

func (e *encoder) free() {
	if e == nil {
		return
	}
	e.h = nil
	e.headerBuf.Reset()
	e.middleBuf.Reset()
	e.trailerBuf.Reset()
	e.groups = e.groups[:0]
	encoderPool.Put(e)
}

func (e *encoder) NewLine(buf *buffer) {
	buf.AppendByte('\n')
}

func (e *encoder) withColor(b *buffer, c ANSIMod, f func()) {
	if c == "" || e.h.opts.NoColor {
		f()
		return
	}
	b.AppendString(string(c))
	f()
	b.AppendString(string(ResetMod))
}

func (e *encoder) writeColoredTime(w *buffer, t time.Time, format string, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendTime(t, format)
	})
}

func (e *encoder) writeColoredString(w *buffer, s string, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendString(s)
	})
}

func (e *encoder) writeColoredInt(w *buffer, i int64, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendInt(i)
	})
}

func (e *encoder) writeColoredUint(w *buffer, i uint64, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendUint(i)
	})
}

func (e *encoder) writeColoredFloat(w *buffer, i float64, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendFloat(i)
	})
}

func (e *encoder) writeColoredBool(w *buffer, b bool, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendBool(b)
	})
}

func (e *encoder) writeColoredDuration(w *buffer, d time.Duration, c ANSIMod) {
	e.withColor(w, c, func() {
		w.AppendDuration(d)
	})
}

func (e *encoder) writeTimestamp(buf *buffer, tt time.Time) {
	if tt.IsZero() {
		// elide, and skip ReplaceAttr
		return
	}

	if e.h.opts.ReplaceAttr != nil {
		attr := e.h.opts.ReplaceAttr(nil, slog.Time(slog.TimeKey, tt))
		attr.Value = attr.Value.Resolve()

		if attr.Value.Equal(slog.Value{}) {
			// elide
			return
		}

		if attr.Value.Kind() != slog.KindTime {
			// handle all non-time values by printing them like
			// an attr value
			e.writeColoredValue(buf, attr.Value, e.h.opts.Theme.Timestamp())
			buf.AppendByte(' ')
			return
		}

		// most common case
		tt = attr.Value.Time()
		if tt.IsZero() {
			// elide
			return
		}
	}

	e.writeColoredTime(buf, tt, e.h.opts.TimeFormat, e.h.opts.Theme.Timestamp())
	buf.AppendByte(' ')
}

// writeSource returns true if source was written, false if elided
func (e *encoder) writeSource(buf *buffer, pc uintptr, cwd string) {
	src := slog.Source{}

	if pc > 0 {
		frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
		src.Function = frame.Function
		src.File = frame.File
		src.Line = frame.Line
	}

	if e.h.opts.ReplaceAttr != nil {
		attr := e.h.opts.ReplaceAttr(nil, slog.Any(slog.SourceKey, &src))
		attr.Value = attr.Value.Resolve()

		if attr.Value.Equal(slog.Value{}) {
			return
		}

		switch attr.Value.Kind() {
		case slog.KindAny:
			if newsrc, ok := attr.Value.Any().(*slog.Source); ok {
				if newsrc == nil {
					// elide
					return
				}

				src.File = newsrc.File
				src.Line = newsrc.Line
				// replaced prior source fields, proceed with normal source processing
				break
			}
			// source replaced with some other type of value,
			// fallthrough to processing other value types
			fallthrough
		default:
			// handle all non-time values by printing them like
			// an attr value
			e.writeColoredValue(buf, attr.Value, e.h.opts.Theme.Timestamp())
			buf.AppendByte(' ')
			return
		}
	}

	if src.File == "" && src.Line == 0 {
		// elide
		return
	}

	if cwd != "" {
		if ff, err := filepath.Rel(cwd, src.File); err == nil {
			src.File = ff
		}
	}
	e.withColor(buf, e.h.opts.Theme.Source(), func() {
		buf.AppendString(src.File)
		buf.AppendByte(':')
		buf.AppendInt(int64(src.Line))
		buf.AppendByte(' ')
	})
}

func (e *encoder) writeMessage(buf *buffer, level slog.Level, msg string) {
	style := e.h.opts.Theme.Message()
	if level < slog.LevelInfo {
		style = e.h.opts.Theme.MessageDebug()
	}

	if e.h.opts.ReplaceAttr != nil {
		attr := e.h.opts.ReplaceAttr(nil, slog.String(slog.MessageKey, msg))
		attr.Value = attr.Value.Resolve()
		if attr.Value.Equal(slog.Value{}) {
			// elide
			return
		}

		e.writeColoredValue(buf, attr.Value, style)
		return
	}

	e.writeColoredString(buf, msg, style)
}

func (e encoder) writeHeaders(buf *buffer, headers []slog.Attr) {
	for _, a := range headers {
		if a.Value.Kind() != slog.KindGroup && e.h.opts.ReplaceAttr != nil {
			a = e.h.opts.ReplaceAttr(nil, a)
			a.Value = a.Value.Resolve()
		}
		if a.Value.Equal(slog.Value{}) {
			continue
		}
		e.writeColoredValue(buf, a.Value, e.h.opts.Theme.Source())
		buf.AppendByte(' ')
	}
}

func (e encoder) writeHeaderSeparator(buf *buffer) {
	e.writeColoredString(buf, "> ", e.h.opts.Theme.AttrKey())
}

func (e *encoder) writeAttr(buf *buffer, a slog.Attr, group string) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() != slog.KindGroup && e.h.opts.ReplaceAttr != nil {
		a = e.h.opts.ReplaceAttr(e.groups, a)
		a.Value = a.Value.Resolve()
	}
	// Elide empty Attrs.
	if a.Equal(slog.Attr{}) {
		return
	}

	value := a.Value

	if value.Kind() == slog.KindGroup {
		subgroup := a.Key
		if group != "" {
			subgroup = group + "." + a.Key
		}
		if e.h.opts.ReplaceAttr != nil {
			e.groups = append(e.groups, a.Key)
		}
		for _, attr := range value.Group() {
			e.writeAttr(buf, attr, subgroup)
		}
		if e.h.opts.ReplaceAttr != nil {
			e.groups = e.groups[:len(e.groups)-1]
		}
		return
	}

	buf.AppendByte(' ')

	e.withColor(buf, e.h.opts.Theme.AttrKey(), func() {
		if group != "" {
			buf.AppendString(group)
			buf.AppendByte('.')
		}
		buf.AppendString(a.Key)
		buf.AppendByte('=')
	})

	style := e.h.opts.Theme.AttrValue()
	if value.Kind() == slog.KindAny {
		if _, ok := value.Any().(error); ok {
			style = e.h.opts.Theme.AttrValueError()
		}
	}
	e.writeColoredValue(buf, value, style)
}

func (e *encoder) writeColoredValue(buf *buffer, value slog.Value, style ANSIMod) {
	switch value.Kind() {
	case slog.KindInt64:
		e.writeColoredInt(buf, value.Int64(), style)
	case slog.KindBool:
		e.writeColoredBool(buf, value.Bool(), style)
	case slog.KindFloat64:
		e.writeColoredFloat(buf, value.Float64(), style)
	case slog.KindTime:
		e.writeColoredTime(buf, value.Time(), e.h.opts.TimeFormat, style)
	case slog.KindUint64:
		e.writeColoredUint(buf, value.Uint64(), style)
	case slog.KindDuration:
		e.writeColoredDuration(buf, value.Duration(), style)
	case slog.KindAny:
		switch v := value.Any().(type) {
		case error:
			if _, ok := v.(fmt.Formatter); ok {
				e.withColor(buf, style, func() {
					fmt.Fprintf(buf, "%+v", v)
				})
			} else {
				e.writeColoredString(buf, v.Error(), style)
			}
			return
		case fmt.Stringer:
			e.writeColoredString(buf, v.String(), style)
			return
		}
		fallthrough
	case slog.KindString:
		fallthrough
	default:
		e.writeColoredString(buf, value.String(), style)
	}
}

func (e *encoder) writeLevel(buf *buffer, l slog.Level) {
	var val slog.Value
	var writeVal bool

	if e.h.opts.ReplaceAttr != nil {
		attr := e.h.opts.ReplaceAttr(nil, slog.Any(slog.LevelKey, l))
		attr.Value = attr.Value.Resolve()

		if attr.Value.Equal(slog.Value{}) {
			// elide
			return
		}

		val = attr.Value
		writeVal = true

		if val.Kind() == slog.KindAny {
			if ll, ok := val.Any().(slog.Level); ok {
				// generally, we'll write the returned value, except in one
				// case: when the resolved value is itself a slog.Level
				l = ll
				writeVal = false
			}
		}
	}

	var style ANSIMod
	var str string
	var delta int
	switch {
	case l >= slog.LevelError:
		style = e.h.opts.Theme.LevelError()
		str = "ERR"
		delta = int(l - slog.LevelError)
	case l >= slog.LevelWarn:
		style = e.h.opts.Theme.LevelWarn()
		str = "WRN"
		delta = int(l - slog.LevelWarn)
	case l >= slog.LevelInfo:
		style = e.h.opts.Theme.LevelInfo()
		str = "INF"
		delta = int(l - slog.LevelInfo)
	case l >= slog.LevelDebug:
		style = e.h.opts.Theme.LevelDebug()
		str = "DBG"
		delta = int(l - slog.LevelDebug)
	default:
		style = e.h.opts.Theme.LevelDebug()
		str = "DBG"
		delta = int(l - slog.LevelDebug)
	}
	if writeVal {
		e.writeColoredValue(buf, val, style)
	} else {
		if delta != 0 {
			str = fmt.Sprintf("%s%+d", str, delta)
		}
		e.writeColoredString(buf, str, style)
	}
	buf.AppendByte(' ')
}
