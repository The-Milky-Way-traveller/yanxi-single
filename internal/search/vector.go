//go:build vector

package search

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

const (
	vectorDims = 384
	modelURL   = "https://huggingface.co/minishlab/potion-code-16M/resolve/main/model.safetensors"
	modelFile  = ".yanxi/potion-code-16M.safetensors"
)

var modelCached bool

func ensureModel() bool {
	if modelCached { return true }
	if _, err := os.Stat(filepath.Join(".", modelFile)); err == nil { modelCached = true; return true }
	return false
}

func downloadModel() error {
	dir := filepath.Dir(modelFile)
	os.MkdirAll(dir, 0755)
	resp, err := http.Get(modelURL)
	if err != nil { return fmt.Errorf("download: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return fmt.Errorf("HTTP %d", resp.StatusCode) }
	f, _ := os.Create(modelFile)
	defer f.Close()
	_, err = f.ReadFrom(resp.Body)
	modelCached = true
	return err
}

func (idx *Index) hybridSearch(query string, topK int) []VectorResult {
	if !ensureModel() {
		results := idx.Search(query, topK)
		var vr []VectorResult
		for _, r := range results { vr = append(vr, VectorResult{Doc: r.Doc, Score: r.Score, BM25: r.Score, Module: r.Module}) }
		return vr
	}
	bm25Results := idx.Search(query, 20)
	type scored struct { result VectorResult; rrf float64 }
	var candidates []scored
	for i, r := range bm25Results {
		rrf := 1.0 / float64(60+i+1)
		candidates = append(candidates, scored{result: VectorResult{Doc: r.Doc, Score: rrf, BM25: r.Score, Module: r.Module}, rrf: rrf})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].rrf > candidates[j].rrf })
	if topK > len(candidates) { topK = len(candidates) }
	var results []VectorResult
	for i := 0; i < topK; i++ { candidates[i].result.Score = math.Round(candidates[i].rrf*1000) / 1000; results = append(results, candidates[i].result) }
	return results
}

func (idx *Index) SearchVector(query string, topK int, mode string) []VectorResult {
	switch mode {
	case "bm25":
		r := idx.Search(query, topK)
		var vr []VectorResult
		for _, rr := range r { vr = append(vr, VectorResult{Doc: rr.Doc, Score: rr.Score, BM25: rr.Score, Module: rr.Module}) }
		return vr
	case "vector", "hybrid":
		return idx.hybridSearch(query, topK)
	default:
		return idx.hybridSearch(query, topK)
	}
}

func LoadVector() error { return downloadModel() }

func init() { _ = json.Marshal; _ = sort.Ints }
