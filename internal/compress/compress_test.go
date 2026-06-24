package compress_test

import (
	"bytes"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/compress"
)

func TestRoundtrip(t *testing.T) {
	original := bytes.Repeat([]byte("cerclbackup test data "), 200)
	compressed, err := compress.Compress(original)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if len(compressed) >= len(original) {
		t.Errorf("compressed (%d) >= original (%d); zstd should shrink repetitive data",
			len(compressed), len(original))
	}
	recovered, err := compress.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if !bytes.Equal(original, recovered) {
		t.Error("roundtrip mismatch")
	}
}

func TestEmptyInput(t *testing.T) {
	c, err := compress.Compress(nil)
	if err != nil {
		t.Fatalf("Compress(nil): %v", err)
	}
	d, err := compress.Decompress(c)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if len(d) != 0 {
		t.Errorf("expected empty output, got %d bytes", len(d))
	}
}

func TestMaybeDecompressPassthrough(t *testing.T) {
	raw := []byte("not compressed")
	out, err := compress.MaybeDecompress(raw, false)
	if err != nil {
		t.Fatalf("MaybeDecompress(false): %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Error("MaybeDecompress with compressed=false must return src unchanged")
	}
}

func TestMaybeDecompressDecompresses(t *testing.T) {
	original := []byte("hello cerclbackup world")
	c, _ := compress.Compress(original)
	out, err := compress.MaybeDecompress(c, true)
	if err != nil {
		t.Fatalf("MaybeDecompress(true): %v", err)
	}
	if !bytes.Equal(out, original) {
		t.Errorf("got %q, want %q", out, original)
	}
}

func TestConcurrentSafety(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1024)
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			c, err := compress.Compress(data)
			if err != nil {
				t.Errorf("concurrent Compress: %v", err)
				return
			}
			if _, err := compress.Decompress(c); err != nil {
				t.Errorf("concurrent Decompress: %v", err)
			}
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}
