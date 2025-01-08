package console

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestHandler_TimeFormat(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{TimeFormat: time.RFC3339Nano, NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	endTime := now.Add(time.Second)
	rec.AddAttrs(slog.Time("endtime", endTime))
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar endtime=%s\n", now.Format(time.RFC3339Nano), endTime.Format(time.RFC3339Nano))
	AssertEqual(t, expected, buf.String())
}

// Handlers should not log the time field if it is zero.
// '- If r.Time is the zero time, ignore the time.'
// https://pkg.go.dev/log/slog@master#Handler
func TestHandler_TimeZero(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{TimeFormat: time.RFC3339Nano, NoColor: true})
	rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "foobar", 0)
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("INF foobar\n")
	AssertEqual(t, expected, buf.String())
}

func TestHandler_NoColor(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar\n", now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())
}

type theStringer struct{}

func (t theStringer) String() string { return "stringer" }

type noStringer struct {
	Foo string
}

var _ slog.LogValuer = &theValuer{}

type theValuer struct {
	word string
}

// LogValue implements the slog.LogValuer interface.
// This only works if the attribute value is a pointer to theValuer:
//
//	slog.Any("field", &theValuer{"word"}
func (v *theValuer) LogValue() slog.Value {
	return slog.StringValue(fmt.Sprintf("The word is '%s'", v.word))
}

func TestHandler_Attr(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	rec.AddAttrs(
		slog.Bool("bool", true),
		slog.Int("int", -12),
		slog.Uint64("uint", 12),
		slog.Float64("float", 3.14),
		slog.String("foo", "bar"),
		slog.Time("time", now),
		slog.Duration("dur", time.Second),
		slog.Group("group", slog.String("foo", "bar"), slog.Group("subgroup", slog.String("foo", "bar"))),
		slog.Any("err", errors.New("the error")),
		slog.Any("stringer", theStringer{}),
		slog.Any("nostringer", noStringer{Foo: "bar"}),
		// Resolve LogValuer items in addition to Stringer items.
		// '- Attr's values should be resolved.'
		// https://pkg.go.dev/log/slog@master#Handler
		// https://pkg.go.dev/log/slog@master#LogValuer
		slog.Any("valuer", &theValuer{"distant"}),
		// Handlers are supposed to avoid logging empty attributes.
		// '- If an Attr's key and value are both the zero value, ignore the Attr.'
		// https://pkg.go.dev/log/slog@master#Handler
		slog.Attr{},
		slog.Any("", nil),
	)
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar bool=true int=-12 uint=12 float=3.14 foo=bar time=%s dur=1s group.foo=bar group.subgroup.foo=bar err=the error stringer=stringer nostringer={bar} valuer=The word is 'distant'\n", now.Format(time.DateTime), now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())
}

// Handlers should not log groups (or subgroups) without fields.
// '- If a group has no Attrs (even if it has a non-empty key), ignore it.'
// https://pkg.go.dev/log/slog@master#Handler
func TestHandler_GroupEmpty(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	rec.AddAttrs(
		slog.Group("group", slog.String("foo", "bar")),
		slog.Group("empty"),
	)
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar group.foo=bar\n", now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())
}

// Handlers should expand groups named "" (the empty string) into the enclosing log record.
// '- If a group's key is empty, inline the group's Attrs.'
// https://pkg.go.dev/log/slog@master#Handler
func TestHandler_GroupInline(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	rec.AddAttrs(
		slog.Group("group", slog.String("foo", "bar")),
		slog.Group("", slog.String("foo", "bar")),
	)
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar group.foo=bar foo=bar\n", now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())
}

// A Handler should call Resolve on attribute values in groups.
// https://cs.opensource.google/go/x/exp/+/0dcbfd60:slog/slogtest/slogtest.go
func TestHandler_GroupResolve(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	rec.AddAttrs(
		slog.Group("group", "stringer", theStringer{}, "valuer", &theValuer{"surreal"}),
	)
	AssertNoError(t, h.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar group.stringer=stringer group.valuer=The word is 'surreal'\n", now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())
}

func TestHandler_WithAttr(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	h2 := h.WithAttrs([]slog.Attr{
		slog.Bool("bool", true),
		slog.Int("int", -12),
		slog.Uint64("uint", 12),
		slog.Float64("float", 3.14),
		slog.String("foo", "bar"),
		slog.Time("time", now),
		slog.Duration("dur", time.Second),
		// A Handler should call Resolve on attribute values from WithAttrs.
		// https://cs.opensource.google/go/x/exp/+/0dcbfd60:slog/slogtest/slogtest.go
		slog.Any("stringer", theStringer{}),
		slog.Any("valuer", &theValuer{"awesome"}),
		slog.Group("group",
			slog.String("foo", "bar"),
			slog.Group("subgroup",
				slog.String("foo", "bar"),
			),
			// A Handler should call Resolve on attribute values in groups from WithAttrs.
			// https://cs.opensource.google/go/x/exp/+/0dcbfd60:slog/slogtest/slogtest.go
			"stringer", theStringer{},
			"valuer", &theValuer{"pizza"},
		)})
	AssertNoError(t, h2.Handle(context.Background(), rec))

	expected := fmt.Sprintf("%s INF foobar bool=true int=-12 uint=12 float=3.14 foo=bar time=%s dur=1s stringer=stringer valuer=The word is 'awesome' group.foo=bar group.subgroup.foo=bar group.stringer=stringer group.valuer=The word is 'pizza'\n", now.Format(time.DateTime), now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())

	buf.Reset()
	AssertNoError(t, h.Handle(context.Background(), rec))
	AssertEqual(t, fmt.Sprintf("%s INF foobar\n", now.Format(time.DateTime)), buf.String())
}

func TestHandler_WithGroup(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true})
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", 0)
	rec.Add("int", 12)
	h2 := h.WithGroup("group1").WithAttrs([]slog.Attr{slog.String("foo", "bar")})
	AssertNoError(t, h2.Handle(context.Background(), rec))
	expected := fmt.Sprintf("%s INF foobar group1.foo=bar group1.int=12\n", now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())
	buf.Reset()

	h3 := h2.WithGroup("group2")
	AssertNoError(t, h3.Handle(context.Background(), rec))
	expected = fmt.Sprintf("%s INF foobar group1.foo=bar group1.group2.int=12\n", now.Format(time.DateTime))
	AssertEqual(t, expected, buf.String())

	buf.Reset()
	AssertNoError(t, h.Handle(context.Background(), rec))
	AssertEqual(t, fmt.Sprintf("%s INF foobar int=12\n", now.Format(time.DateTime)), buf.String())
}

func TestHandler_Levels(t *testing.T) {
	levels := map[slog.Level]string{
		slog.LevelDebug - 1: "DBG-1",
		slog.LevelDebug:     "DBG",
		slog.LevelDebug + 1: "DBG+1",
		slog.LevelInfo:      "INF",
		slog.LevelInfo + 1:  "INF+1",
		slog.LevelWarn:      "WRN",
		slog.LevelWarn + 1:  "WRN+1",
		slog.LevelError:     "ERR",
		slog.LevelError + 1: "ERR+1",
	}

	for l := range levels {
		t.Run(l.String(), func(t *testing.T) {
			buf := bytes.Buffer{}
			h := NewHandler(&buf, &HandlerOptions{Level: l, NoColor: true})
			for ll, s := range levels {
				AssertEqual(t, ll >= l, h.Enabled(context.Background(), ll))
				now := time.Now()
				rec := slog.NewRecord(now, ll, "foobar", 0)
				if ll >= l {
					AssertNoError(t, h.Handle(context.Background(), rec))
					AssertEqual(t, fmt.Sprintf("%s %s foobar\n", now.Format(time.DateTime), s), buf.String())
					buf.Reset()
				}
			}
		})
	}
}

func TestHandler_Source(t *testing.T) {
	buf := bytes.Buffer{}
	h := NewHandler(&buf, &HandlerOptions{NoColor: true, AddSource: true})
	h2 := NewHandler(&buf, &HandlerOptions{NoColor: true, AddSource: false})
	pc, file, line, _ := runtime.Caller(0)
	now := time.Now()
	rec := slog.NewRecord(now, slog.LevelInfo, "foobar", pc)
	AssertNoError(t, h.Handle(context.Background(), rec))
	cwd, _ := os.Getwd()
	file, _ = filepath.Rel(cwd, file)
	AssertEqual(t, fmt.Sprintf("%s INF %s:%d > foobar\n", now.Format(time.DateTime), file, line), buf.String())
	buf.Reset()
	AssertNoError(t, h2.Handle(context.Background(), rec))
	AssertEqual(t, fmt.Sprintf("%s INF foobar\n", now.Format(time.DateTime)), buf.String())
	buf.Reset()
	// If the PC is zero then this field and its associated group should not be logged.
	// '- If r.PC is zero, ignore it.'
	// https://pkg.go.dev/log/slog@master#Handler
	rec.PC = 0
	AssertNoError(t, h.Handle(context.Background(), rec))
	AssertEqual(t, fmt.Sprintf("%s INF foobar\n", now.Format(time.DateTime)), buf.String())
}

func TestHandler_Err(t *testing.T) {
	w := writerFunc(func(b []byte) (int, error) { return 0, errors.New("nope") })
	h := NewHandler(w, &HandlerOptions{NoColor: true})
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "foobar", 0)
	AssertError(t, h.Handle(context.Background(), rec))
}

func TestThemes(t *testing.T) {
	for _, theme := range []Theme{
		NewDefaultTheme(),
		NewBrightTheme(),
		NewDimTheme(),
	} {
		t.Run(theme.Name(), func(t *testing.T) {
			level := slog.LevelInfo
			rec := slog.Record{}
			buf := bytes.Buffer{}
			bufBytes := buf.Bytes()
			now := time.Now()
			timeFormat := time.Kitchen
			index := -1
			toIndex := -1
			var lastField []byte
			h := NewHandler(&buf, &HandlerOptions{
				AddSource:  true,
				TimeFormat: timeFormat,
				Theme:      theme,
			}).WithAttrs([]slog.Attr{{Key: "pid", Value: slog.IntValue(37556)}})
			var pcs [1]uintptr
			runtime.Callers(1, pcs[:])

			checkANSIMod := func(t *testing.T, name string, ansiMod ANSIMod) {
				t.Run(name, func(t *testing.T) {
					index = bytes.IndexByte(bufBytes, '\x1b')
					AssertNotEqual(t, -1, index)
					toIndex = index + len(ansiMod)
					AssertEqual(t, ansiMod, ANSIMod(bufBytes[index:toIndex]))
					bufBytes = bufBytes[toIndex:]
					index = bytes.IndexByte(bufBytes, '\x1b')
					AssertNotEqual(t, -1, index)
					lastField = bufBytes[:index]
					toIndex = index + len(ResetMod)
					AssertEqual(t, ResetMod, ANSIMod(bufBytes[index:toIndex]))
					bufBytes = bufBytes[toIndex:]
				})
			}

			checkLog := func(level slog.Level, attrCount int) {
				t.Run("CheckLog_"+level.String(), func(t *testing.T) {
					println("log: ", string(buf.Bytes()))

					// Timestamp
					if theme.Timestamp() != "" {
						checkANSIMod(t, "Timestamp", theme.Timestamp())
					}

					// Level
					if theme.Level(level) != "" {
						checkANSIMod(t, level.String(), theme.Level(level))
					}

					// Source
					if theme.Source() != "" {
						checkANSIMod(t, "Source", theme.Source())
						checkANSIMod(t, "AttrKey", theme.AttrKey())
					}

					// Message
					if level >= slog.LevelInfo {
						if theme.Message() != "" {
							checkANSIMod(t, "Message", theme.Message())
						}
					} else {
						if theme.MessageDebug() != "" {
							checkANSIMod(t, "MessageDebug", theme.MessageDebug())
						}
					}

					for i := 0; i < attrCount; i++ {
						// AttrKey
						if theme.AttrKey() != "" {
							checkANSIMod(t, "AttrKey", theme.AttrKey())
						}

						if string(lastField) == "error=" {
							// AttrValueError
							if theme.AttrValueError() != "" {
								checkANSIMod(t, "AttrValueError", theme.AttrValueError())
							}
						} else {
							// AttrValue
							if theme.AttrValue() != "" {
								checkANSIMod(t, "AttrValue", theme.AttrValue())
							}
						}
					}
				})
			}

			buf.Reset()
			level = slog.LevelDebug - 1
			rec = slog.NewRecord(now, level, "Access", pcs[0])
			rec.Add("database", "myapp", "host", "localhost:4962")
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 3)

			buf.Reset()
			level = slog.LevelDebug
			rec = slog.NewRecord(now, level, "Access", pcs[0])
			rec.Add("database", "myapp", "host", "localhost:4962")
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 3)

			buf.Reset()
			level = slog.LevelDebug + 1
			rec = slog.NewRecord(now, level, "Access", pcs[0])
			rec.Add("database", "myapp", "host", "localhost:4962")
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 3)

			buf.Reset()
			level = slog.LevelInfo
			rec = slog.NewRecord(now, level, "Starting listener", pcs[0])
			rec.Add("listen", ":8080")
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 2)

			buf.Reset()
			level = slog.LevelInfo + 1
			rec = slog.NewRecord(now, level, "Access", pcs[0])
			rec.Add("method", "GET", "path", "/users", "resp_time", time.Millisecond*10)
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 4)

			buf.Reset()
			level = slog.LevelWarn
			rec = slog.NewRecord(now, level, "Slow request", pcs[0])
			rec.Add("method", "POST", "path", "/posts", "resp_time", time.Second*532)
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 4)

			buf.Reset()
			level = slog.LevelWarn + 1
			rec = slog.NewRecord(now, level, "Slow request", pcs[0])
			rec.Add("method", "POST", "path", "/posts", "resp_time", time.Second*532)
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 4)

			buf.Reset()
			level = slog.LevelError
			rec = slog.NewRecord(now, level, "Database connection lost", pcs[0])
			rec.Add("database", "myapp", "error", errors.New("connection reset by peer"))
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 3)

			buf.Reset()
			level = slog.LevelError + 1
			rec = slog.NewRecord(now, level, "Database connection lost", pcs[0])
			rec.Add("database", "myapp", "error", errors.New("connection reset by peer"))
			h.Handle(context.Background(), rec)
			bufBytes = buf.Bytes()
			checkLog(level, 3)
		})
	}
}
