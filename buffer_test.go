package console

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

func TestBuffer_Append(t *testing.T) {
	b := new(buffer)
	AssertZero(t, b.Len())
	b.AppendString("foobar")
	AssertEqual(t, 6, b.Len())
	b.AppendString("baz")
	AssertEqual(t, 9, b.Len())
	AssertEqual(t, "foobarbaz", b.String())

	b.AppendByte('.')
	AssertEqual(t, 10, b.Len())
	AssertEqual(t, "foobarbaz.", b.String())

	b.AppendBool(true)
	b.AppendBool(false)
	b.AppendFloat(3.14)
	b.AppendInt(42)
	b.AppendUint(12)
	b.Append([]byte("foo"))
	b.AppendDuration(1 * time.Second)
	now := time.Now()
	b.AppendTime(now, time.RFC3339)

	AssertEqual(t, "foobarbaz.truefalse3.144212foo1s"+now.Format(time.RFC3339), b.String())
}

func TestBuffer_WriteTo(t *testing.T) {
	dest := bytes.Buffer{}
	b := new(buffer)
	n, err := b.WriteTo(&dest)
	AssertNoError(t, err)
	AssertZero(t, n)
	b.AppendString("foobar")
	n, err = b.WriteTo(&dest)
	AssertEqual(t, len("foobar"), int(n))
	AssertNoError(t, err)
	AssertEqual(t, "foobar", dest.String())
	AssertZero(t, b.Len())

	t.Run("nilbuffer", func(t *testing.T) {
		// if receiver is nil, do nothing
		dest.Reset()
		c, err := (*buffer)(nil).WriteTo(&dest)
		AssertNoError(t, err)
		AssertZero(t, c)
		AssertZero(t, dest.Len())
	})
}

func TestBuffer_Clone(t *testing.T) {
	b := new(buffer)
	b.AppendString("foobar")
	b2 := b.Clone()
	AssertEqual(t, b.String(), b2.String())
	AssertNotEqual(t, &b.Bytes()[0], &b2.Bytes()[0])
}

func TestBuffer_Copy(t *testing.T) {
	b := new(buffer)
	b.AppendString("foobar")
	b2 := new(buffer)
	b2.copy(b)
	AssertEqual(t, b.String(), b2.String())
	AssertNotEqual(t, &b.Bytes()[0], &b2.Bytes()[0])
}

func TestBuffer_Reset(t *testing.T) {
	b := new(buffer)
	b.AppendString("foobar")
	AssertEqual(t, "foobar", b.String())
	AssertEqual(t, len("foobar"), b.Len())
	bufCap := b.Cap()
	b.Reset()
	AssertZero(t, b.Len())
	AssertEqual(t, bufCap, b.Cap())
}

func TestBuffer_Grow(t *testing.T) {
	b := new(buffer)
	AssertZero(t, b.Cap())
	b.Grow(12)
	AssertGreaterOrEqual(t, 12, b.Cap())
	b.Grow(6)
	AssertGreaterOrEqual(t, 12, b.Cap())
	b.Grow(24)
	AssertGreaterOrEqual(t, 24, b.Cap())
}

func TestBuffer_Clip(t *testing.T) {
	b := new(buffer)
	b.AppendString("foobar")
	b.Grow(12)
	AssertGreaterOrEqual(t, 12, b.Cap())
	b.Clip()
	AssertEqual(t, "foobar", b.String())
	AssertEqual(t, len("foobar"), b.Cap())
}

func TestBuffer_WriteTo_Err(t *testing.T) {
	w := writerFunc(func(b []byte) (int, error) { return 0, errors.New("nope") })
	b := new(buffer)
	b.AppendString("foobar")
	_, err := b.WriteTo(w)
	AssertError(t, err)

	w = writerFunc(func(b []byte) (int, error) { return 0, nil })
	_, err = b.WriteTo(w)
	AssertError(t, err)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Expected io.ErrShortWrite, go %T", err)
	}
}

func BenchmarkBuffer(b *testing.B) {
	data := []byte("foobarbaz")

	b.Run("std", func(b *testing.B) {
		buf := bytes.Buffer{}
		for i := 0; i < b.N; i++ {
			buf.Write(data)
			buf.WriteByte('.')
			buf.Reset()
		}
	})

	b.Run("buffer", func(b *testing.B) {
		buf := buffer{}
		for i := 0; i < b.N; i++ {
			buf.Append(data)
			buf.AppendByte('.')
			buf.Reset()
		}
	})
}
