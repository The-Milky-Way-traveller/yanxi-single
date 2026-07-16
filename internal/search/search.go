package search

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// DocKind distinguishes AIexplain cards from source code.
type DocKind string

const (
	KindAIExplain DocKind = "aiexplain"
	KindSource    DocKind = "source"
)

// Doc represents a single indexed document.
type Doc struct {
	ID     string  `json:"id"`
	Path   string  `json:"path"`
	Text   string  `json:"-"`
	Length int     `json:"length"`
	Kind   DocKind `json:"kind"`
	Module string  `json:"module"`
}

// TermEntry records how many times a term appears in a doc.
type TermEntry struct {
	Doc  string `json:"d"`
	Freq int    `json:"f"`
}

// Index is the complete BM25 search index.
type Index struct {
	Docs   map[string]*Doc          `json:"docs"`
	Terms  map[string][]TermEntry   `json:"terms"`
	AvgDL  float64                  `json:"avgdl"`
	N      int                      `json:"doc_count"`
}

// Result is a single search hit.
type Result struct {
	Doc    Doc     `json:"doc"`
	Score  float64 `json:"score"`
	Module string  `json:"module"`
}

// VectorResult adds vector scores to search results.
type VectorResult struct {
	Doc    Doc     `json:"doc"`
	Score  float64 `json:"score"`
	BM25   float64 `json:"bm25,omitempty"`
	Vector float64 `json:"vector,omitempty"`
	Module string  `json:"module"`
}

// ── Tokenizer ──────────────────────────────────────────────────

func tokenize(text string) []string {
	var tokens []string
	var current strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(unicode.ToLower(r))
		} else {
			if current.Len() > 1 {
				tokens = append(tokens, current.String())
			}
			current.Reset()
		}
	}
	if current.Len() > 1 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// ── BM25 ───────────────────────────────────────────────────────

const (
	k1 = 1.5
	b  = 0.75
)

func (idx *Index) score(queryTokens []string, doc *Doc) float64 {
	if doc.Length == 0 {
		return 0
	}
	var score float64
	for _, qt := range queryTokens {
		entries, ok := idx.Terms[qt]
		if !ok {
			continue
		}
		var tf int
		var df int
		for _, e := range entries {
			if e.Doc == doc.ID {
				tf = e.Freq
			}
			df++
		}
		if tf == 0 {
			continue
		}
		idf := math.Log(1 + (float64(idx.N)-float64(df)+0.5)/(float64(df)+0.5))
		tfNorm := (float64(tf) * (k1 + 1)) / (float64(tf) + k1*(1-b+b*float64(doc.Length)/idx.AvgDL))
		score += idf * tfNorm
	}
	return score
}

// Search returns the top-K matching documents.
func (idx *Index) Search(query string, topK int) []Result {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	type scored struct {
		doc   *Doc
		score float64
	}
	var candidates []scored
	for _, doc := range idx.Docs {
		s := idx.score(queryTokens, doc)
		if s > 0 {
			candidates = append(candidates, scored{doc, s})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}

	var results []Result
	for i := 0; i < topK; i++ {
		results = append(results, Result{
			Doc:    *candidates[i].doc,
			Score:  math.Round(candidates[i].score*1000) / 1000,
			Module: candidates[i].doc.Module,
		})
	}
	return results
}

// ── Index builder ──────────────────────────────────────────────

// BuildIndex scans a project and creates a BM25 index.
func BuildIndex(root string) (*Index, error) {
	idx := &Index{
		Docs:  make(map[string]*Doc),
		Terms: make(map[string][]TermEntry),
	}

	var totalLen int64

	// Index AIexplain files
	aiexplainDir := filepath.Join(root, "AIexplain", "modules")
	if entries, err := os.ReadDir(aiexplainDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			modName := entry.Name()
			cardPath := filepath.Join(aiexplainDir, modName, modName+".md")
			if data, err := os.ReadFile(cardPath); err == nil {
				addDoc(idx, modName, modName, cardPath, string(data), KindAIExplain, &totalLen)
			}
			ifacePath := filepath.Join(aiexplainDir, modName, "interface.md")
			if data, err := os.ReadFile(ifacePath); err == nil {
				addDoc(idx, modName+"_iface", modName, ifacePath, string(data), KindAIExplain, &totalLen)
			}
		}
	}

	// Index source files
	modulesDir := filepath.Join(root, "source", "modules")
	if entries, err := os.ReadDir(modulesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			modName := entry.Name()
			for _, ext := range []string{".py", ".ts", ".js", ".go"} {
				srcPath := filepath.Join(modulesDir, modName, modName+ext)
				if data, err := os.ReadFile(srcPath); err == nil {
					addDoc(idx, modName+"_src", modName, srcPath, string(data), KindSource, &totalLen)
					break
				}
			}
			modJSONPath := filepath.Join(modulesDir, modName, "module.json")
			if data, err := os.ReadFile(modJSONPath); err == nil {
				addDoc(idx, modName+"_contract", modName, modJSONPath, string(data), KindSource, &totalLen)
			}
		}
	}

	// Index project memory
	memoryDir := filepath.Join(root, "project-memory")
	if entries, err := os.ReadDir(memoryDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(memoryDir, entry.Name())
			if data, err := os.ReadFile(path); err == nil {
				addDoc(idx, "memory_"+entry.Name(), "project-memory", path, string(data), KindSource, &totalLen)
			}
		}
	}

	if idx.N > 0 {
		idx.AvgDL = float64(totalLen) / float64(idx.N)
	}

	return idx, nil
}

// BuildLooseIndex scans any directory for source files without
// requiring a micro-architecture structure. Use for legacy projects.
func BuildLooseIndex(root string) (*Index, error) {
	idx := &Index{Docs: make(map[string]*Doc), Terms: make(map[string][]TermEntry)}
	var totalLen int64

	// Resolve to absolute to avoid SkipDir on "." root
	absRoot, err := filepath.Abs(root)
	if err != nil { absRoot = root }

	codeExts := map[string]bool{".py": true, ".ts": true, ".js": true, ".go": true, ".rs": true, ".java": true, ".c": true, ".cpp": true, ".h": true, ".rb": true, ".php": true}
	docExts := map[string]bool{".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true}

		filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				// Skip hidden dirs and node_modules
				if info != nil && info.IsDir() {
					name := info.Name()
					if strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" {
						return filepath.SkipDir
					}
				}
				return nil
			}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		kind := KindSource
		if docExts[ext] { kind = KindAIExplain }
		if !codeExts[ext] && !docExts[ext] { return nil }

		data, err := os.ReadFile(path)
		if err != nil { return nil }
		rel, _ := filepath.Rel(absRoot, path)
		mod := filepath.Dir(rel)
		addDoc(idx, rel, mod, rel, string(data), kind, &totalLen)
		return nil
	})

	if idx.N > 0 { idx.AvgDL = float64(totalLen) / float64(idx.N) }
	return idx, nil
}

func addDoc(idx *Index, id, mod, path, text string, kind DocKind, totalLen *int64) {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return
	}
	idx.Docs[id] = &Doc{
		ID: id, Path: path, Text: "", Length: len(tokens), Kind: kind, Module: mod,
	}
	*totalLen += int64(len(tokens))
	idx.N++

	freq := make(map[string]int)
	for _, t := range tokens {
		freq[t]++
	}
	for term, count := range freq {
		idx.Terms[term] = append(idx.Terms[term], TermEntry{Doc: id, Freq: count})
	}
}

// ── Persistence ────────────────────────────────────────────────

const indexFileName = ".yanxi/search_index.json"

// Save serializes the index to disk.
func (idx *Index) Save(root string) error {
	// Don't persist the raw text (already stored in files)
	for _, d := range idx.Docs {
		d.Text = ""
	}

	idxDir := filepath.Join(root, ".yanxi")
	os.MkdirAll(idxDir, 0755)

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, indexFileName), data, 0644)
}

// Load deserializes the index from disk.
func Load(root string) (*Index, error) {
	data, err := os.ReadFile(filepath.Join(root, indexFileName))
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}
