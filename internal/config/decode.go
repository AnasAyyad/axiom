package config

import (
	"bytes"
	"encoding/json"
	"io"
)

// DecodeJSON decodes one strict, versioned configuration document.
func DecodeJSON(data []byte) (Configuration, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var configuration Configuration
	if err := decoder.Decode(&configuration); err != nil {
		return Configuration{}, configError("invalid_configuration", "json")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return Configuration{}, err
	}
	if err := Validate(configuration); err != nil {
		return Configuration{}, err
	}
	return cloneConfiguration(configuration), nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	return configError("invalid_configuration", "json")
}
