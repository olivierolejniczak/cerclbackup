package wire

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxMsgSize = 64 * 1024 * 1024 // 64 MB hard cap

// WriteMsg serialises v as JSON and writes it with a 4-byte big-endian length prefix.
func WriteMsg(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("wire: marshal: %w", err)
	}
	if len(payload) > maxMsgSize {
		return fmt.Errorf("wire: message too large (%d bytes)", len(payload))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("wire: write header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("wire: write payload: %w", err)
	}
	return nil
}

// ReadMsg reads a length-prefixed JSON message into v.
func ReadMsg(r io.Reader, v any) error {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return fmt.Errorf("wire: read header: %w", err)
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 {
		return fmt.Errorf("wire: empty message")
	}
	if uint64(size) > uint64(maxMsgSize) {
		return fmt.Errorf("wire: message too large (%d bytes)", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return fmt.Errorf("wire: read payload: %w", err)
	}
	if err := json.Unmarshal(buf, v); err != nil {
		return fmt.Errorf("wire: unmarshal: %w", err)
	}
	return nil
}
