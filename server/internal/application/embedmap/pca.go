// Package embedmap reduces stored chunk embeddings to a handful of dimensions
// so the browser can run UMAP over them. The full vectors (768–3072 floats per
// chunk) are never sent to the client: the server L2-normalizes them,
// mean-centers them and projects them onto their top principal components.
package embedmap

import (
	"math"
	"runtime"
	"sort"
	"sync"
)

const (
	// subspaceIterMax caps the block power iterations. All k directions are
	// iterated together, so this is a cap on passes over the matrix, not on
	// passes per component: extracting 50 components one at a time, each with
	// its own 100-iteration budget, costs 5000 passes and takes seconds on a
	// 2000×768 matrix.
	subspaceIterMax = 24
	// subspaceIterMin is the floor the work budget may not cut below.
	subspaceIterMin = 5
	// subspaceIterBudget is the multiply-add budget the iteration is allowed to
	// spend. One pass costs 2·n·d·k, so the budget only bites on the largest
	// request the API accepts (5000 points × 3072 dimensions × 128 components);
	// the 2000×768 k=50 case gets the full subspaceIterMax and usually settles
	// well before it. The map is a viewer aid, not a numerical result — it must
	// not spend ten seconds refining axes nobody can see.
	subspaceIterBudget = 6e9
	// subspaceEpsilon stops the iteration once every direction's Rayleigh
	// quotient has settled to this relative precision. A 2D map does not need
	// eigenvectors to more digits than this.
	subspaceEpsilon = 1e-6
	// zeroNorm is the norm at or below which a vector counts as degenerate:
	// a zero embedding, or a direction with no variance left after deflation.
	zeroNorm = 1e-12
	// collapseRatio is the share of its own norm a block column must keep after
	// deflation to still count as a direction of its own.
	collapseRatio = 1e-10
)

// PCAResult is one projection. Coords[i] belongs to the input vector at
// Kept[i] — rows dropped as unusable (zero, NaN/Inf, or of a length other than
// the modal one) have no entry, so the caller must carry its per-point metadata
// through Kept rather than assume index alignment.
type PCAResult struct {
	Kept   []int
	Coords [][]float64
	// Dims is the length of every Coords entry. It is min(k, source dimension),
	// or 0 when nothing was usable.
	Dims int
}

// PCA projects vectors onto their top-k principal components.
//
// It is deterministic: no math/rand, no time, no map iteration, and no
// dependence on how many cores ran it — the parallel decomposition below splits
// work so that every output element is produced by exactly one worker,
// accumulating in a fixed order. The same input always yields byte-identical
// output.
//
// The components come from block power iteration: all k directions are iterated
// at once against the covariance action Xᵀ(Xv) and re-orthogonalized against
// each other after every pass, which is deflation applied to the whole block
// rather than one component at a time. The d×d covariance matrix is never
// materialized — for a 3072-dimensional embedding it would be 9.4M entries.
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

// matrix is a row-major n×d block of L2-normalized (and later mean-centered)
// vectors. float32 storage halves the memory traffic of every pass; all
// arithmetic accumulates in float64.
type matrix struct {
	rows, cols int
	data       []float32
}

// buildMatrix drops every unusable row and L2-normalizes the rest.
//
// Unusable means: empty, of a length other than the modal length across the
// input (a provider switch leaves rows of two different dimensions in one
// table), holding a NaN or an Inf, or having a zero norm (nothing to normalize
// and no direction to contribute).
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

// modalLength returns the most common non-zero vector length, ties broken by
// the shorter length so the choice does not depend on iteration order.
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

// center subtracts the column means in place.
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

// principalComponents returns k orthonormal directions spanning the dominant
// eigenspace of XᵀX, ordered by decreasing eigenvalue.
//
// A direction with no variance left (rank exhausted: fewer points than
// dimensions, or all-identical points) comes back as a zero vector, which
// projects every point to 0 on that axis. The result therefore always has
// exactly k entries.
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
		m.mulRight(comps, y)  // Y = X · V
		m.mulLeft(y, k, next) // W = Xᵀ · Y, i.e. the covariance action on every column at once

		// The pre-normalization column norms are the Rayleigh quotients; once
		// they stop moving the subspace has settled.
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

// iterationBudget caps the passes so the largest accepted request stays
// interactive. Fewer passes leave the axes inside the dominant subspace less
// separated from each other, which a UMAP embedding does not notice: it reads
// pairwise distances, and any orthonormal basis of the same subspace preserves
// them.
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

// startBasis is the fixed, seed-free starting block. A plain all-ones vector in
// every column would give k identical directions that collapse to one on the
// first orthogonalization, so each column gets its own deterministic pattern on
// top of the uniform 1/sqrt(d) base; a column that still collapses falls back
// to a standard basis direction.
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

// settled reports whether every Rayleigh quotient moved less than
// subspaceEpsilon relative to the previous pass.
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

// mulRight computes out[r*k+c] = row_r · comps[c] — the projection of every row
// onto every component. Rows are split across workers; each output element is
// written by exactly one of them.
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

// mulLeft computes out[c] = Xᵀ·y[:,c] for every component c. The split is by
// component, not by row: each column is accumulated over rows 0..n-1 by a
// single worker, so no partial sums are merged and the floating-point result
// does not depend on the number of workers.
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

// orderByVariance turns the flat projection into per-row slices with the axes
// sorted by decreasing variance. Block iteration already converges its columns
// in eigenvalue order; sorting makes that a guarantee rather than a property of
// how far the iteration got, so "component 0" is always the widest axis.
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

// orthonormalizeColumns runs modified Gram-Schmidt over the block. This is the
// deflation step: every column is stripped of the directions already claimed by
// the columns before it, so the block cannot collapse onto the leading
// eigenvector. A column with nothing left is zeroed rather than normalized into
// noise.
func orthonormalizeColumns(cols [][]float64) {
	for c, v := range cols {
		before := norm(v)
		orthogonalize(v, cols[:c])
		after := norm(v)
		// The exhaustion test is relative, not absolute: a tail component whose
		// eigenvalue is merely small still carries a direction and must be kept,
		// whereas a column that genuinely lies in the span of its predecessors
		// loses essentially all of its norm right here.
		if after <= zeroNorm || after <= collapseRatio*before {
			for i := range v {
				v[i] = 0
			}
			continue
		}
		normalize(v)
	}
}

// orthogonalize removes the projection of v onto every earlier component.
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

// canonicalSign fixes the arbitrary ± of an eigenvector so the same data always
// projects to the same coordinates: the largest-magnitude entry is made
// positive, ties resolved by the lowest index.
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

// runParallel splits [0,n) into contiguous chunks and runs fn on each. Callers
// must keep chunks independent — the split is a scheduling decision and must
// never change the numbers produced.
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

// dot accumulates into four independent sums: a single accumulator would
// serialize the loop behind the latency of one FP add, which on a 2000×768
// matrix is the difference between seconds and milliseconds. Go does not
// auto-vectorize, so this is unrolled by hand.
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

// axpy adds d·row to out. Reslicing out to the row's length hoists the bounds
// check out of the loop.
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
