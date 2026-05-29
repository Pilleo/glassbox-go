package demo

import (
	"context"

	"gopkg.in/yaml.v3"
)

//gobox:sandbox
type YAMLParser interface {
	Parse(ctx context.Context, data []byte) (map[string]interface{}, error)
}

// YAMLParserImpl handles parsing of YAML documents.
type YAMLParserImpl struct{}

func (y *YAMLParserImpl) Parse(ctx context.Context, data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := yaml.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
