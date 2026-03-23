package output

type PluginOutput[T interface{}] struct {
	Parameters T `json:"parameters"`
}

type ResponsePayload[T interface{}] struct {
	Output struct {
		Parameters T `json:"parameters"`
	} `json:"output"`
}

func (o *PluginOutput[T]) BuildPluginOutput() ResponsePayload[T] {
	response := ResponsePayload[T]{}
	response.Output.Parameters = o.Parameters
	return response
}
