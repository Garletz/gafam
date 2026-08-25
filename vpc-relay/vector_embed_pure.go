package main

// Pure-Go embeddings — no sidecar, no cloud, no C extension, no download
// required. Two tiers:
//   1. (optional) static word vectors (FastText/word2vec ".vec" text format),
//      averaged per word → real semantics, still 100 % Go.
//   2. (default, always available) feature hashing over words + character
//      n-grams → fixed-size deterministic vector, zero dependency.

import (
	"bufio"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

const hashEmbedDims = 512

// ─── Feature hashing (default, always on) ───

func hashEmbed(text string) []float32 {
	vec := make([]float32, hashEmbedDims)
	for _, f := range extractFeatures(text) {
		h := fnv.New32a()
		h.Write([]byte(f))
		idx := h.Sum32() % hashEmbedDims
		h2 := fnv.New32a()
		h2.Write([]byte("s:" + f))
		sign := float32(1.0)
		if h2.Sum32()&1 == 1 {
			sign = -1.0
		}
		vec[idx] += sign
	}
	normalizeVec(vec)
	return vec
}

// extractFeatures yields words plus their padded char n-grams (3..5), the
// FastText-style subword recipe that survives typos and French morphology.
func extractFeatures(text string) []string {
	seen := map[string]bool{}
	feats := make([]string, 0, 64)
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			feats = append(feats, s)
		}
	}
	for _, w := range tokenize(text) {
		add(w)
		pw := "<" + w + ">"
		for n := 3; n <= 5; n++ {
			for i := 0; i+n <= len(pw); i++ {
				add(pw[i : i+n])
			}
		}
	}
	return feats
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func normalizeVec(v []float32) {
	var n float32
	for _, x := range v {
		n += x * x
	}
	if n == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(float64(n)))
	for i := range v {
		v[i] *= inv
	}
}

// ─── Static word vectors (optional, pure Go) ───

var (
	wordVecMu   sync.RWMutex
	wordVecs    map[string][]float32
	wordVecDims int
)

// loadWordVectors parses a FastText/word2vec ".vec" text file (first line
// "vocab dims", then "word v1 v2 … vd"). Call once; safe to call again to
// replace the model.
func loadWordVectors(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)

	first := true
	m := make(map[string][]float32)
	dims := 0
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if first {
			if len(fields) < 2 {
				return fmt.Errorf("bad .vec header")
			}
			dims, _ = strconv.Atoi(fields[1])
			first = false
			continue
		}
		if dims <= 0 || len(fields) < dims+1 {
			continue
		}
		v := make([]float32, dims)
		ok := true
		for i := 0; i < dims; i++ {
			f64, err := strconv.ParseFloat(fields[i+1], 32)
			if err != nil {
				ok = false
				break
			}
			v[i] = float32(f64)
		}
		if ok {
			m[fields[0]] = v
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(m) == 0 || dims == 0 {
		return fmt.Errorf("no vectors loaded from %s", path)
	}
	wordVecMu.Lock()
	wordVecs = m
	wordVecDims = dims
	wordVecMu.Unlock()
	return nil
}

// wordVecEmbed averages the vectors of known words. Returns (vec, true) on
// success, (nil, false) if no model is loaded or no word matched.
func wordVecEmbed(text string) ([]float32, bool) {
	wordVecMu.RLock()
	vecs := wordVecs
	dims := wordVecDims
	wordVecMu.RUnlock()
	if len(vecs) == 0 || dims <= 0 {
		return nil, false
	}
	var sum []float32
	count := 0
	for _, w := range tokenize(text) {
		if v, ok := vecs[w]; ok {
			if sum == nil {
				sum = make([]float32, dims)
			}
			for i := range v {
				sum[i] += v[i]
			}
			count++
		}
	}
	if count == 0 {
		return nil, false
	}
	for i := range sum {
		sum[i] /= float32(count)
	}
	normalizeVec(sum)
	return sum, true
}
