package observability

import (
	"context"
	"log/slog"
	"strings"
)

const redactedValue = "[REDACTED]"

var sensitiveAttributeNames = map[string]struct{}{
	"authorization": {}, "cookie": {}, "credential": {}, "csrf": {},
	"header": {}, "headers": {}, "password": {}, "payload": {},
	"private_key": {}, "secret": {}, "session": {}, "set_cookie": {},
	"signature": {}, "token": {},
}

type redactingHandler struct {
	next    slog.Handler
	secrets []string
}

func newRedactingHandler(next slog.Handler, secrets []string) slog.Handler {
	return &redactingHandler{next: next, secrets: filterSecrets(secrets)}
}

func filterSecrets(secrets []string) []string {
	filtered := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if len(secret) >= 8 && !strings.ContainsAny(secret, "\r\n\x00") {
			filtered = append(filtered, secret)
		}
	}
	return filtered
}

// Enabled delegates level selection to the wrapped handler.
func (handler *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

// Handle redacts one complete record before delegation.
func (handler *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, handler.redactText(record.Message), record.PC)
	record.Attrs(func(attribute slog.Attr) bool {
		clean.AddAttrs(handler.redactAttribute(attribute))
		return true
	})
	return handler.next.Handle(ctx, clean)
}

// WithAttrs returns a wrapped handler with already-redacted attributes.
func (handler *redactingHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attributes))
	for index, attribute := range attributes {
		clean[index] = handler.redactAttribute(attribute)
	}
	return &redactingHandler{next: handler.next.WithAttrs(clean), secrets: handler.secrets}
}

// WithGroup returns a wrapped handler with a redacted group name.
func (handler *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: handler.next.WithGroup(handler.redactText(name)), secrets: handler.secrets}
}

func (handler *redactingHandler) redactAttribute(attribute slog.Attr) slog.Attr {
	attribute.Key = handler.redactText(attribute.Key)
	attribute.Value = attribute.Value.Resolve()
	if sensitiveAttribute(attribute.Key) {
		return slog.String(attribute.Key, redactedValue)
	}
	switch attribute.Value.Kind() {
	case slog.KindString:
		return slog.String(attribute.Key, handler.redactText(attribute.Value.String()))
	case slog.KindGroup:
		members := attribute.Value.Group()
		for index := range members {
			members[index] = handler.redactAttribute(members[index])
		}
		return slog.Group(attribute.Key, attrsToAny(members)...)
	case slog.KindAny:
		if err, ok := attribute.Value.Any().(error); ok {
			return slog.String(attribute.Key, handler.redactText(err.Error()))
		}
		return slog.String(attribute.Key, redactedValue)
	default:
		return attribute
	}
}

func (handler *redactingHandler) redactText(value string) string {
	for _, secret := range handler.secrets {
		value = strings.ReplaceAll(value, secret, redactedValue)
	}
	return value
}

func sensitiveAttribute(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	if _, sensitive := sensitiveAttributeNames[normalized]; sensitive {
		return true
	}
	for _, suffix := range []string{"_password", "_secret", "_token", "_signature", "_credential", "_cookie"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func attrsToAny(attributes []slog.Attr) []any {
	result := make([]any, len(attributes))
	for index := range attributes {
		result[index] = attributes[index]
	}
	return result
}
