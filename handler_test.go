package console

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
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

type formatterError struct {
	error
}

func (e *formatterError) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('+') {
		io.WriteString(f, "formatted ")
	}
	io.WriteString(f, e.Error())
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
		slog.Any("formattedError", &formatterError{errors.New("the error")}),
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

	expected := fmt.Sprintf("%s INF foobar bool=true int=-12 uint=12 float=3.14 foo=bar time=%s dur=1s group.foo=bar group.subgroup.foo=bar err=the error formattedError=formatted the error stringer=stringer nostringer={bar} valuer=The word is 'distant'\n", now.Format(time.DateTime), now.Format(time.DateTime))
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

type valuer struct {
	v slog.Value
}

func (v valuer) LogValue() slog.Value {
	return v.v
}

func TestHandler_ReplaceAttr(t *testing.T) {
	pc, file, line, _ := runtime.Caller(0)
	cwd, _ := os.Getwd()
	file, _ = filepath.Rel(cwd, file)
	sourceField := fmt.Sprintf("%s:%d", file, line)

	replaceAttrWith := func(key string, out slog.Attr) func(*testing.T, []string, slog.Attr) slog.Attr {
		return func(t *testing.T, s []string, a slog.Attr) slog.Attr {
			if a.Key == key {
				return out
			}
			return a
		}
	}

	awesomeVal := slog.Any("valuer", valuer{slog.StringValue("awesome")})

	tests := []struct {
		name        string
		replaceAttr func(*testing.T, []string, slog.Attr) slog.Attr
		want        string
		modrec      func(*slog.Record)
		noSource    bool
		groups      []string
	}{
		{
			name: "no replaceattrs",
			want: "2010-05-06 07:08:09 INF " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name: "not called for empty timestamp and disabled source",
			modrec: func(r *slog.Record) {
				r.Time = time.Time{}
			},
			noSource: true,
			want:     "INF foobar size=12 color=red\n",
			replaceAttr: func(t *testing.T, s []string, a slog.Attr) slog.Attr {
				switch a.Key {
				case slog.TimeKey, slog.SourceKey:
					t.Errorf("replaceAttr should not have been called for %v", a)
				}
				return a
			},
		},
		{
			name:   "not called for groups",
			modrec: func(r *slog.Record) { r.Add(slog.Group("l1", slog.String("flavor", "vanilla"))) },
			replaceAttr: func(t *testing.T, s []string, a slog.Attr) slog.Attr {
				if a.Key == "l1" {
					t.Errorf("should not have been called on group attrs, was called on %v", a)
				}
				return a
			},
			want: "2010-05-06 07:08:09 INF " + sourceField + " > foobar size=12 color=red l1.flavor=vanilla\n",
		},
		{
			name:   "groups arg",
			groups: []string{"l1", "l2"},
			modrec: func(r *slog.Record) {
				r.Add(slog.Group("l3", slog.String("flavor", "vanilla")))
				r.Add(slog.Int("weight", 23))
			},
			replaceAttr: func(t *testing.T, s []string, a slog.Attr) slog.Attr {
				wantGroups := []string{"l1", "l2"}
				switch a.Key {
				case slog.TimeKey, slog.SourceKey, slog.MessageKey, slog.LevelKey:
					if len(s) != 0 {
						t.Errorf("for builtin attr %v, expected no groups, got %v", a.Key, s)
					}
				case "flavor":
					wantGroups = []string{"l1", "l2", "l3"}
					fallthrough
				default:
					if !reflect.DeepEqual(wantGroups, s) {
						t.Errorf("for %v attr, expected %v, got %v", a.Key, wantGroups, s)
					}
				}
				return slog.String(a.Key, a.Key)
			},
			want: "time level source > msg l1.l2.size=size l1.l2.color=color l1.l2.l3.flavor=flavor l1.l2.weight=weight\n",
		},
		{
			name:        "clear timestamp",
			replaceAttr: replaceAttrWith(slog.TimeKey, slog.Time(slog.TimeKey, time.Time{})),
			want:        "INF " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace timestamp",
			replaceAttr: replaceAttrWith(slog.TimeKey, slog.Time(slog.TimeKey, time.Date(2000, 2, 3, 4, 5, 6, 0, time.UTC))),
			want:        "2000-02-03 04:05:06 INF " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace timestamp with different kind",
			replaceAttr: replaceAttrWith(slog.TimeKey, slog.String("color", "red")),
			want:        "red INF " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace timestamp with valuer",
			replaceAttr: replaceAttrWith(slog.TimeKey, awesomeVal),
			want:        "awesome INF " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace timestamp with time valuer",
			replaceAttr: replaceAttrWith(slog.TimeKey, slog.Any("valuer", valuer{slog.TimeValue(time.Date(2000, 2, 3, 4, 5, 6, 0, time.UTC))})),
			want:        "2000-02-03 04:05:06 INF " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace level",
			replaceAttr: replaceAttrWith(slog.LevelKey, slog.Any(slog.LevelKey, slog.LevelWarn)),
			want:        "2010-05-06 07:08:09 WRN " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "clear level",
			replaceAttr: replaceAttrWith(slog.LevelKey, slog.Any(slog.LevelKey, nil)),
			want:        "2010-05-06 07:08:09 " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace level with different kind",
			replaceAttr: replaceAttrWith(slog.LevelKey, slog.String("color", "red")),
			want:        "2010-05-06 07:08:09 red " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace level with valuer",
			replaceAttr: replaceAttrWith(slog.LevelKey, awesomeVal),
			want:        "2010-05-06 07:08:09 awesome " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "replace level with level valuer",
			replaceAttr: replaceAttrWith(slog.LevelKey, slog.Any("valuer", valuer{slog.AnyValue(slog.LevelWarn)})),
			want:        "2010-05-06 07:08:09 WRN " + sourceField + " > foobar size=12 color=red\n",
		},
		{
			name:        "clear source",
			replaceAttr: replaceAttrWith(slog.SourceKey, slog.Any(slog.SourceKey, nil)),
			want:        "2010-05-06 07:08:09 INF foobar size=12 color=red\n",
		},
		{
			name: "replace source",
			replaceAttr: replaceAttrWith(slog.SourceKey, slog.Any(slog.SourceKey, &slog.Source{
				File: filepath.Join(cwd, "path", "to", "file.go"),
				Line: 33,
			})),
			want: "2010-05-06 07:08:09 INF path/to/file.go:33 > foobar size=12 color=red\n",
		},
		{
			name:        "replace source with different kind",
			replaceAttr: replaceAttrWith(slog.SourceKey, slog.String("color", "red")),
			want:        "2010-05-06 07:08:09 INF red > foobar size=12 color=red\n",
		},
		{
			name:        "replace source with valuer",
			replaceAttr: replaceAttrWith(slog.SourceKey, awesomeVal),
			want:        "2010-05-06 07:08:09 INF awesome > foobar size=12 color=red\n",
		},
		{
			name: "replace source with source valuer",
			replaceAttr: replaceAttrWith(slog.SourceKey, slog.Any("valuer", valuer{slog.AnyValue(&slog.Source{
				File: filepath.Join(cwd, "path", "to", "file.go"),
				Line: 33,
			})})),
			want: "2010-05-06 07:08:09 INF path/to/file.go:33 > foobar size=12 color=red\n",
		},
		{
			name:   "empty source", // should still be called
			modrec: func(r *slog.Record) { r.PC = 0 },
			replaceAttr: replaceAttrWith(slog.SourceKey, slog.Any(slog.SourceKey, &slog.Source{
				File: filepath.Join(cwd, "path", "to", "file.go"),
				Line: 33,
			})),
			want: "2010-05-06 07:08:09 INF path/to/file.go:33 > foobar size=12 color=red\n",
		},
		{
			name:        "clear message",
			replaceAttr: replaceAttrWith(slog.MessageKey, slog.Any(slog.MessageKey, nil)),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " >  size=12 color=red\n",
		},
		{
			name:        "replace message",
			replaceAttr: replaceAttrWith(slog.MessageKey, slog.String(slog.MessageKey, "barbaz")),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " > barbaz size=12 color=red\n",
		},
		{
			name:        "replace message with different kind",
			replaceAttr: replaceAttrWith(slog.MessageKey, slog.Int(slog.MessageKey, 5)),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " > 5 size=12 color=red\n",
		},
		{
			name:        "replace message with valuer",
			replaceAttr: replaceAttrWith(slog.MessageKey, awesomeVal),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " > awesome size=12 color=red\n",
		},
		{
			name:        "clear attr",
			replaceAttr: replaceAttrWith("size", slog.Attr{}),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " > foobar color=red\n",
		},
		{
			name:        "replace attr",
			replaceAttr: replaceAttrWith("size", slog.String("flavor", "vanilla")),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " > foobar flavor=vanilla color=red\n",
		},
		{
			name:        "replace with group attrs",
			replaceAttr: replaceAttrWith("size", slog.Group("l1", slog.String("flavor", "vanilla"))),
			want:        "2010-05-06 07:08:09 INF " + sourceField + " > foobar l1.flavor=vanilla color=red\n",
		},
		// {
		// 	name: "replace header",
		// }
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf := bytes.Buffer{}

			rec := slog.NewRecord(time.Date(2010, 5, 6, 7, 8, 9, 0, time.UTC), slog.LevelInfo, "foobar", pc)
			rec.Add("size", 12, "color", "red")

			if test.modrec != nil {
				test.modrec(&rec)
			}

			var replaceAttr func([]string, slog.Attr) slog.Attr
			if test.replaceAttr != nil {
				replaceAttr = func(s []string, a slog.Attr) slog.Attr {
					return test.replaceAttr(t, s, a)
				}
			}

			var h slog.Handler = NewHandler(&buf, &HandlerOptions{AddSource: !test.noSource, NoColor: true, ReplaceAttr: replaceAttr})

			for _, group := range test.groups {
				h = h.WithGroup(group)
			}

			AssertNoError(t, h.Handle(context.Background(), rec))

			AssertEqual(t, test.want, buf.String())

		})
	}

}

func TestHandler_Headers(t *testing.T) {
	pc, file, line, _ := runtime.Caller(0)
	cwd, _ := os.Getwd()
	file, _ = filepath.Rel(cwd, file)
	sourceField := fmt.Sprintf("%s:%d", file, line)

	tests := []struct {
		name       string
		opts       HandlerOptions
		attrs      []slog.Attr
		withAttrs  []slog.Attr
		withGroups []string
		want       string
	}{
		{
			name:  "no headers",
			attrs: []slog.Attr{slog.String("foo", "bar")},
			want:  "INF with headers foo=bar\n",
		},
		{
			name: "one header",
			opts: HandlerOptions{Headers: []string{"foo"}},
			attrs: []slog.Attr{
				slog.String("foo", "bar"),
				slog.String("bar", "baz"),
			},
			want: "INF bar > with headers bar=baz\n",
		},
		{
			name: "two headers",
			opts: HandlerOptions{Headers: []string{"foo", "bar"}},
			attrs: []slog.Attr{
				slog.String("foo", "bar"),
				slog.String("bar", "baz"),
			},
			want: "INF bar baz > with headers\n",
		},
		{
			name:  "missing headers",
			opts:  HandlerOptions{Headers: []string{"foo", "bar"}},
			attrs: []slog.Attr{slog.String("bar", "baz"), slog.String("baz", "foo")},
			want:  "INF baz > with headers baz=foo\n",
		},
		{
			name: "missing all headers",
			opts: HandlerOptions{Headers: []string{"foo", "bar"}},
			want: "INF with headers\n",
		},
		{
			name: "header and source",
			opts: HandlerOptions{Headers: []string{"foo"}, AddSource: true},
			attrs: []slog.Attr{
				slog.String("foo", "bar"),
				slog.String("bar", "baz"),
			},
			want: "INF " + sourceField + " bar > with headers bar=baz\n",
		},
		{
			name: "withattrs",
			opts: HandlerOptions{Headers: []string{"foo"}},
			attrs: []slog.Attr{

				slog.String("bar", "baz"),
			},
			withAttrs: []slog.Attr{
				slog.String("foo", "bar"),
			},
			want: "INF bar > with headers bar=baz\n",
		},
		{
			name: "withgroup",
			opts: HandlerOptions{Headers: []string{"foo", "bar"}},
			attrs: []slog.Attr{
				slog.String("bar", "baz"),
				slog.String("baz", "foo"),
			},
			withGroups: []string{"group"},
			withAttrs: []slog.Attr{
				slog.String("foo", "bar"),
			},
			want: "INF bar baz > with headers group.baz=foo\n",
		},
		// {
		// 	name: "resolver header",
		// }
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf := bytes.Buffer{}

			opts := &test.opts
			opts.NoColor = true
			var h slog.Handler = NewHandler(&buf, &test.opts)
			if test.withAttrs != nil {
				h = h.WithAttrs(test.withAttrs)
			}
			for _, g := range test.withGroups {
				h = h.WithGroup(g)
			}

			rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "with headers", pc)

			rec.AddAttrs(test.attrs...)

			AssertNoError(t, h.Handle(context.Background(), rec))
			AssertEqual(t, test.want, buf.String())
		})
	}

	t.Run("withAttrs state keeping", func(t *testing.T) {
		// test to make sure the way that WithAttrs() copies the cached headers doesn't leak
		// headers back to the parent handler or to subsequent Handle() calls (i.e. ensure that
		// the headers slice is copied at the right times).

		buf := bytes.Buffer{}
		h := NewHandler(&buf, &HandlerOptions{
			Headers:    []string{"foo", "bar"},
			TimeFormat: "0",
			NoColor:    true,
		})

		assertLog := func(t *testing.T, handler slog.Handler, want string, attrs ...slog.Attr) {
			buf.Reset()
			rec := slog.NewRecord(time.Time{}, slog.LevelInfo, "with headers", pc)

			rec.AddAttrs(attrs...)

			AssertNoError(t, handler.Handle(context.Background(), rec))
			AssertEqual(t, want, buf.String())
		}

		assertLog(t, h, "INF bar > with headers\n", slog.String("foo", "bar"))

		h2 := h.WithAttrs([]slog.Attr{slog.String("foo", "baz")})
		assertLog(t, h2, "INF baz > with headers\n")

		h3 := h2.WithAttrs([]slog.Attr{slog.String("foo", "buz")})
		assertLog(t, h3, "INF buz > with headers\n")
		// creating h3 should not have affected h2
		assertLog(t, h2, "INF baz > with headers\n")

		// overriding attrs shouldn't affect the handler
		assertLog(t, h2, "INF biz > with headers\n", slog.String("foo", "biz"))
		assertLog(t, h2, "INF baz > with headers\n")

	})
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
