package mcp

// inputObject decodes the map-shaped arguments passed across the MCP boundary.
// Handlers normally receive validated input from the SDK, but keeping this
// check here also makes direct handler use safe for tests and future adapters.
func inputObject(input any) (map[string]any, error) {
	object, ok := input.(map[string]any)
	if !ok {
		return nil, invalidArgumentsError()
	}
	return object, nil
}

func requiredString(input map[string]any, key string) (string, error) {
	value, ok := input[key].(string)
	if !ok {
		return "", invalidArgumentsError()
	}
	return value, nil
}
