package postgres

import (
	"strconv"
	"strings"
	"time"

	"axiom/internal/api/console"
)

const a11CursorSeparator = "\x1f"

func decodeA11TimeCursor(codec console.CursorCodec, scope, cursor string) (time.Time, string, int, error) {
	position, err := codec.Decode(scope, cursor)
	if err != nil || position == "" {
		return time.Time{}, "", -1, err
	}
	parts := strings.Split(position, a11CursorSeparator)
	if len(parts) < 2 || len(parts) > 3 {
		return time.Time{}, "", -1, console.ErrInvalidRequest
	}
	occurred, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil || occurred.Location() != time.UTC || parts[1] == "" {
		return time.Time{}, "", -1, console.ErrInvalidRequest
	}
	line := -1
	if len(parts) == 3 {
		line, err = strconv.Atoi(parts[2])
		if err != nil || line < 0 {
			return time.Time{}, "", -1, console.ErrInvalidRequest
		}
	}
	return occurred, parts[1], line, nil
}

func encodeA11TimeCursor(codec console.CursorCodec, scope string, occurred time.Time, id string, line ...int) string {
	position := occurred.UTC().Format(time.RFC3339Nano) + a11CursorSeparator + id
	if len(line) == 1 {
		position += a11CursorSeparator + strconv.Itoa(line[0])
	}
	return codec.Encode(scope, position)
}

func decodeA11PairCursor(codec console.CursorCodec, scope, cursor string) (string, string, error) {
	position, err := codec.Decode(scope, cursor)
	if err != nil || position == "" {
		return "", "", err
	}
	parts := strings.Split(position, a11CursorSeparator)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", console.ErrInvalidRequest
	}
	return parts[0], parts[1], nil
}
