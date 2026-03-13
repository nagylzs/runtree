package runner

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/vmihailenco/msgpack/v5"
	"io"
)

func ReadInt(ctx context.Context, stream io.Reader) (uint32, error) {
	const size = 4 // uint32
	buf := make([]byte, size)
	n, err := stream.Read(buf)
	if err != nil {
		return 0, err
	}
	if n != size {
		return 0, fmt.Errorf("expected %s bytes, got %d", size, n)
	}
	return binary.LittleEndian.Uint32(buf), nil
}

func WriteInt(ctx context.Context, stream io.Writer, i uint32) error {
	const size = 4 // uint32
	b := make([]byte, size)
	binary.LittleEndian.PutUint32(b, i)
	n, err := stream.Write(b)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("expected %d bytes, got %d", size, n)
	}
	return nil
}

func RecvBytes(ctx context.Context, stream io.Reader) ([]byte, error) {
	size, err := ReadInt(ctx, stream)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	totalRead := 0
	for totalRead < int(size) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			n, err := stream.Read(buf[totalRead:])
			if err != nil {
				return nil, err
			}
			totalRead += n
		}
	}
	return buf, nil
}

func SendBytes(ctx context.Context, stream io.Writer, data []byte) error {
	err := WriteInt(ctx, stream, uint32(len(data)))
	if err != nil {
		return err
	}
	totalWritten := 0
	for totalWritten < len(data) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			n, err := stream.Write(data[totalWritten:])
			if err != nil {
				return err
			}
			totalWritten += n
		}
	}
	return nil
}

func RecvString(ctx context.Context, stream io.Reader) (string, error) {
	buf, err := RecvBytes(ctx, stream)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func SendString(ctx context.Context, stream io.Writer, s string) error {
	return SendBytes(ctx, stream, []byte(s))
}

func RecvAny(ctx context.Context, stream io.Reader, v any) error {
	buf, err := RecvBytes(ctx, stream)
	if err != nil {
		return err
	}
	return msgpack.Unmarshal(buf, v)
}

func SendAny[T any](ctx context.Context, stream io.Writer, v T) error {
	buf, err := msgpack.Marshal(v)
	if err != nil {
		return err
	}
	return SendBytes(ctx, stream, buf)
}

func SendCmdCode(ctx context.Context, stream io.Writer, cmd CmdCode) error {
	buf, err := msgpack.Marshal(&cmd)
	if err != nil {
		return err
	}
	return SendBytes(ctx, stream, buf)
}

func RecvCmdCode(ctx context.Context, stream io.Reader) (CmdCode, error) {
	buf, err := RecvBytes(ctx, stream)
	if err != nil {
		return 0, err
	}
	var result CmdCode
	err = msgpack.Unmarshal(buf, &result)
	return result, err
}
