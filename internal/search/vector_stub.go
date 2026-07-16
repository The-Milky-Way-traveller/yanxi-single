//go:build !vector

package search

// SearchVector stub — returns BM25 results when vector tag is not enabled.
func (idx *Index) SearchVector(query string, topK int, mode string) []VectorResult {
	results := idx.Search(query, topK)
	var vr []VectorResult
	for _, r := range results {
		vr = append(vr, VectorResult{Doc: r.Doc, Score: r.Score, BM25: r.Score, Module: r.Module})
	}
	return vr
}

// LoadVector is a no-op without the vector build tag.
func LoadVector() error { return nil }
