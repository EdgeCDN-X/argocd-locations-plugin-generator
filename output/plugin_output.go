package output

type PluginOutput[T any] struct {
	Parameters T `json:"parameters"`
}
