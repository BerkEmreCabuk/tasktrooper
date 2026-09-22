package embedmap

import (
	"math"
	"runtime"
	"sort"
	"sync"
)

const (
	subspaceIterMax = 24

	subspaceIterMin = 5

	subspaceIterBudget = 6e9

	subspaceEpsilon = 1e-6

	zeroNorm = 1e-12

	collapseRatio = 1e-10
)

type PCAResult struct {
	Kept   []int
	Coords [][]float64

	Dims int
}

func PCA(vectors [][]float32, k int) PCAResult {
	if k < 1 {
		k = 1
	}
	m, kept := buildMatrix(vectors)
	if m == nil {
		return PCAResult{Dims: 0}
	}
	if k > m.cols {
		k = m.cols
	}
	center(m)

	comps := principalComponents(m, k)
	for _, comp := range comps {
		canonicalSign(comp)
	}

	flat := make([]float64, m.rows*k)
	m.mulRight(comps, flat)
	coords := orderByVariance(flat, m.rows, k)
	return PCAResult{Kept: kept, Coords: coords, Dims: k}
}

type matrix struct {
	rows, cols int
	data       []float32
}

func buildMatrix(vectors [][]float32) (*matrix, []int) {
	dim := modalLength(vectors)
	if dim == 0 {
		return nil, nil
	}
	kept := make([]int, 0, len(vectors))
	data := make([]float32, 0, len(vectors)*dim)
	for i, v := range vectors {
		if len(v) != dim {
			continue
		}
		var sumSq float64
		bad := false
		for _, x := range v {
			f := float64(x)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				bad = true
				break
			}
			sumSq += f * f
		}
		if bad || sumSq <= zeroNorm*zeroNorm {
			continue
		}
		inv := 1 / math.Sqrt(sumSq)
		for _, x := range v {
			data = append(data, float32(float64(x)*inv))
		}
		kept = append(kept, i)
	}
	if len(kept) == 0 {
		return nil, nil
	}
	return &matrix{rows: len(kept), cols: dim, data: data}, kept
}

func modalLength(vectors [][]float32) int {
	counts := map[int]int{}
	for _, v := range vectors {
		if len(v) > 0 {
			counts[len(v)]++
		}
	}
	best, bestCount := 0, 0
	for length, count := range counts {
		if count > bestCount || (count == bestCount && length < best) {
			best, bestCount = length, count
		}
	}
	return best
}

func center(m *matrix) {
	means := make([]float64, m.cols)
	for r := 0; r < m.rows; r++ {
		row := m.data[r*m.cols : (r+1)*m.cols]
		for j, x := range row {
			means[j] += float64(x)
		}
	}
	inv := 1 / float64(m.rows)
	for j := range means {
		means[j] *= inv
	}
	for r := 0; r < m.rows; r++ {
		row := m.data[r*m.cols : (r+1)*m.cols]
		for j := range row {
			row[j] = float32(float64(row[j]) - means[j])
		}
	}
}

func principalComponents(m *matrix, k int) [][]float64 {
	comps := startBasis(m.cols, k)
	y := make([]float64, m.rows*k)
	next := make([][]float64, k)
	for c := range next {
		next[c] = make([]float64, m.cols)
	}

	var prev []float64
	maxIter := iterationBudget(m.rows, m.cols, k)
	for it := 0; it < maxIter; it++ {
		m.mulRight(comps, y)
		m.mulLeft(y, k, next)

		quotients := make([]float64, k)
		for c := range next {
			quotients[c] = norm(next[c])
		}
		orthonormalizeColumns(next)
		comps, next = next, comps

		if settled(prev, quotients) {
			break
		}
		prev = quotients
	}
	return comps
}

func iterationBudget(rows, cols, k int) int {
	perPass := 2 * float64(rows) * float64(cols) * float64(k)
	if perPass <= 0 {
		return subspaceIterMax
	}
	iters := int(subspaceIterBudget / perPass)
	if iters < subspaceIterMin {
		return subspaceIterMin
	}
	if iters > subspaceIterMax {
		return subspaceIterMax
	}
	return iters
}

func startBasis(dim, k int) [][]float64 {
	cols := make([][]float64, 0, k)
	base := 1 / math.Sqrt(float64(dim))
	for c := 0; c < k; c++ {
		v := make([]float64, dim)
		for i := range v {
			v[i] = base * (1 + 0.5*math.Cos(float64((i+1)*(c+1))))
		}
		orthogonalize(v, cols)
		for j := 0; j < dim && norm(v) <= zeroNorm; j++ {
			for i := range v {
				v[i] = 0
			}
			v[(c+j)%dim] = 1
			orthogonalize(v, cols)
		}
		normalize(v)
		cols = append(cols, v)
	}
	return cols
}

func settled(prev, cur []float64) bool {
	if prev == nil {
		return false
	}
	for c := range cur {
		scale := math.Max(math.Abs(cur[c]), math.Abs(prev[c]))
		if scale <= zeroNorm {
			continue
		}
		if math.Abs(cur[c]-prev[c])/scale > subspaceEpsilon {
			return false
		}
	}
	return true
}

func (m *matrix) mulRight(comps [][]float64, out []float64) {
	k := len(comps)
	runParallel(m.rows, func(start, end int) {
		for r := start; r < end; r++ {
			row := m.data[r*m.cols : (r+1)*m.cols]
			base := r * k
			for c := 0; c < k; c++ {
				out[base+c] = dot(row, comps[c])
			}
		}
	})
}

func (m *matrix) mulLeft(y []float64, k int, out [][]float64) {
	runParallel(k, func(start, end int) {
		for c := start; c < end; c++ {
			w := out[c]
			for j := range w {
				w[j] = 0
			}
		}
		for r := 0; r < m.rows; r++ {
			row := m.data[r*m.cols : (r+1)*m.cols]
			base := r * k
			for c := start; c < end; c++ {
				if d := y[base+c]; d != 0 {
					axpy(d, row, out[c])
				}
			}
		}
	})
}

func orderByVariance(flat []float64, rows, k int) [][]float64 {
	variance := make([]float64, k)
	for r := 0; r < rows; r++ {
		base := r * k
		for c := 0; c < k; c++ {
			v := flat[base+c]
			variance[c] += v * v
		}
	}
	order := make([]int, k)
	for c := range order {
		order[c] = c
	}
	sort.SliceStable(order, func(a, b int) bool { return variance[order[a]] > variance[order[b]] })

	coords := make([][]float64, rows)
	for r := 0; r < rows; r++ {
		base := r * k
		out := make([]float64, k)
		for c, src := range order {
			out[c] = flat[base+src]
		}
		coords[r] = out
	}
	return coords
}

func orthonormalizeColumns(cols [][]float64) {
	for c, v := range cols {
		before := norm(v)
		orthogonalize(v, cols[:c])
		after := norm(v)

		if after <= zeroNorm || after <= collapseRatio*before {
			for i := range v {
				v[i] = 0
			}
			continue
		}
		normalize(v)
	}
}

func orthogonalize(v []float64, comps [][]float64) {
	for _, comp := range comps {
		var d float64
		for j, c := range comp {
			d += c * v[j]
		}
		if d == 0 {
			continue
		}
		for j, c := range comp {
			v[j] -= d * c
		}
	}
}

func canonicalSign(v []float64) {
	idx, best := -1, 0.0
	for i, x := range v {
		if a := math.Abs(x); a > best {
			idx, best = i, a
		}
	}
	if idx >= 0 && v[idx] < 0 {
		for i := range v {
			v[i] = -v[i]
		}
	}
}

func runParallel(n int, fn func(start, end int)) {
	workers := runtime.GOMAXPROCS(0)
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		fn(0, n)
		return
	}
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for start := 0; start < n; start += chunk {
		end := start + chunk
		if end > n {
			end = n
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			fn(s, e)
		}(start, end)
	}
	wg.Wait()
}

func dot(row []float32, v []float64) float64 {
	v = v[:len(row)]
	var s0, s1, s2, s3 float64
	j := 0
	for ; j+4 <= len(row); j += 4 {
		s0 += float64(row[j]) * v[j]
		s1 += float64(row[j+1]) * v[j+1]
		s2 += float64(row[j+2]) * v[j+2]
		s3 += float64(row[j+3]) * v[j+3]
	}
	for ; j < len(row); j++ {
		s0 += float64(row[j]) * v[j]
	}
	return (s0 + s1) + (s2 + s3)
}

func axpy(d float64, row []float32, out []float64) {
	out = out[:len(row)]
	j := 0
	for ; j+4 <= len(row); j += 4 {
		out[j] += d * float64(row[j])
		out[j+1] += d * float64(row[j+1])
		out[j+2] += d * float64(row[j+2])
		out[j+3] += d * float64(row[j+3])
	}
	for ; j < len(row); j++ {
		out[j] += d * float64(row[j])
	}
}

func norm(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	return math.Sqrt(sum)
}

func normalize(v []float64) {
	n := norm(v)
	if n <= zeroNorm {
		return
	}
	inv := 1 / n
	for i := range v {
		v[i] *= inv
	}
}
