package domain

import "strings"

// The engines a vector can come from. One model name is not enough to say two
// vectors are comparable: the desktop's int8 ONNX build of
// nomic-embed-text-v1.5 and a full-precision server (TEI) answer under the same
// name with different numbers, and ranking one against the other is wrong
// without being an error.
const (
	EmbeddingSourceONNXInt8 = "onnx-int8"
	EmbeddingSourceTEI      = "tei"
)

// EmbeddingProvenance is the single string an index records as what it was
// embedded with when the engine matters as much as the model, so the
// existing name-and-dimension staleness check (EmbeddingProvenanceStale) also
// catches an engine change without a second column.
func EmbeddingProvenance(model, source string) string {
	model, source = strings.TrimSpace(model), strings.TrimSpace(source)
	if model == "" || source == "" {
		return model
	}
	return model + "@" + source
}
