package flagstack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func DecodeConfiguration(data []byte) (Configuration, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var configuration Configuration
	if err := decoder.Decode(&configuration); err != nil {
		return Configuration{}, fmt.Errorf("decode configuration: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Configuration{}, fmt.Errorf("configuration contains multiple JSON values")
		}
		return Configuration{}, fmt.Errorf("decode trailing configuration data: %w", err)
	}
	if err := ValidateConfiguration(configuration); err != nil {
		return Configuration{}, fmt.Errorf("validate configuration: %w", err)
	}
	return configuration, nil
}

func cloneConfiguration(configuration Configuration) Configuration {
	data, err := json.Marshal(configuration)
	if err != nil {
		return configuration
	}
	cloned, err := DecodeConfiguration(data)
	if err != nil {
		return configuration
	}
	return cloned
}
