package embedmap_test

import (
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
)

func fixedVectors(rows, cols int) [][]float32 {
	out := make([][]float32, rows)
	for i := range out {
		v := make([]float32, cols)
		for j := range v {
			v[j] = float32(math.Sin(float64((i+1)*(j+3))) + 0.25*math.Cos(float64(i*j+7)))
		}
		out[i] = v
	}
	return out
}

func axisSpread(coords [][]float64, axis int) float64 {
	if len(coords) == 0 {
		return 0
	}
	min, max := coords[0][axis], coords[0][axis]
	for _, c := range coords {
		if c[axis] < min {
			min = c[axis]
		}
		if c[axis] > max {
			max = c[axis]
		}
	}
	return max - min
}

func TestPCAFirstComponentIsTheXAxis(t *testing.T) {
	angles := []float64{-0.2, -0.1, 0, 0.1, 0.2}
	vectors := make([][]float32, len(angles))
	for i, a := range angles {
		vectors[i] = []float32{float32(math.Sin(a)), float32(math.Cos(a))}
	}

	res := embedmap.PCA(vectors, 2)

	require.Equal(t, 2, res.Dims)
	require.Len(t, res.Kept, len(angles))
	for i := range res.Coords {
		require.Len(t, res.Coords[i], 2)
	}

	for i := 1; i < len(res.Coords); i++ {
		require.Greater(t, res.Coords[i][0], res.Coords[i-1][0])
	}
	require.InDelta(t, 0, res.Coords[len(res.Coords)/2][0], 1e-6)

	require.Less(t, axisSpread(res.Coords, 1), 0.1*axisSpread(res.Coords, 0))
}

func TestPCARankExhaustedComponentIsZero(t *testing.T) {
	res := embedmap.PCA([][]float32{{1, 0, 0}, {-1, 0, 0}}, 2)

	require.Equal(t, 2, res.Dims)
	require.Len(t, res.Coords, 2)
	require.InDelta(t, 1, res.Coords[0][0], 1e-6)
	require.InDelta(t, -1, res.Coords[1][0], 1e-6)
	require.InDelta(t, 0, res.Coords[0][1], 1e-9)
	require.InDelta(t, 0, res.Coords[1][1], 1e-9)
}

func TestPCAIsDeterministic(t *testing.T) {
	vectors := fixedVectors(64, 24)

	first := embedmap.PCA(vectors, 8)
	second := embedmap.PCA(vectors, 8)

	require.Equal(t, first.Dims, second.Dims)
	require.Equal(t, first.Kept, second.Kept)
	require.Equal(t, first.Coords, second.Coords, "same input must project to byte-identical coordinates")
}

func TestPCAIsIndependentOfParallelism(t *testing.T) {
	vectors := fixedVectors(600, 96)
	restore := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(restore)

	single := embedmap.PCA(vectors, 12)
	runtime.GOMAXPROCS(8)
	parallel := embedmap.PCA(vectors, 12)

	require.Equal(t, single.Coords, parallel.Coords)
}

func TestPCADoesNotMutateInput(t *testing.T) {
	vectors := fixedVectors(8, 5)
	before := make([][]float32, len(vectors))
	for i, v := range vectors {
		before[i] = append([]float32(nil), v...)
	}

	embedmap.PCA(vectors, 3)

	require.Equal(t, before, vectors)
}

func TestPCAOutputLength(t *testing.T) {
	for _, k := range []int{2, 3, 16, 50} {
		res := embedmap.PCA(fixedVectors(40, 32), k)
		want := k
		if want > 32 {
			want = 32
		}
		require.Equal(t, want, res.Dims)
		for _, c := range res.Coords {
			require.Len(t, c, want, "every output vector has exactly Dims entries")
		}
	}
}

func TestPCADegenerateInputs(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	tests := []struct {
		name      string
		vectors   [][]float32
		k         int
		wantDims  int
		wantKept  []int
		allZero   bool
		wantEmpty bool
	}{
		{
			name:      "nil input",
			vectors:   nil,
			k:         50,
			wantEmpty: true,
		},
		{
			name:      "no points",
			vectors:   [][]float32{},
			k:         4,
			wantEmpty: true,
		},
		{
			name:      "only empty vectors",
			vectors:   [][]float32{{}, {}},
			k:         4,
			wantEmpty: true,
		},
		{
			name:      "only zero vectors",
			vectors:   [][]float32{{0, 0, 0}, {0, 0, 0}},
			k:         2,
			wantEmpty: true,
		},
		{
			name:     "single point",
			vectors:  [][]float32{{1, 2, 3}},
			k:        2,
			wantDims: 2,
			wantKept: []int{0},
			allZero:  true,
		},
		{
			name:     "all identical points",
			vectors:  [][]float32{{1, 2, 3}, {1, 2, 3}, {1, 2, 3}},
			k:        3,
			wantDims: 3,
			wantKept: []int{0, 1, 2},
			allZero:  true,
		},
		{
			name:     "identical up to scale collapses after normalization",
			vectors:  [][]float32{{1, 0}, {2, 0}, {4, 0}},
			k:        2,
			wantDims: 2,
			wantKept: []int{0, 1, 2},
			allZero:  true,
		},
		{
			name:     "dimension below k",
			vectors:  [][]float32{{1, 0}, {0, 1}, {1, 1}},
			k:        50,
			wantDims: 2,
			wantKept: []int{0, 1, 2},
		},
		{
			name:     "k below one is raised to one",
			vectors:  [][]float32{{1, 0}, {0, 1}},
			k:        0,
			wantDims: 1,
			wantKept: []int{0, 1},
		},
		{
			name:     "NaN and Inf rows are dropped",
			vectors:  [][]float32{{1, 0}, {nan, 1}, {0, 1}, {inf, 0}, {1, 1}},
			k:        2,
			wantDims: 2,
			wantKept: []int{0, 2, 4},
		},
		{
			name:     "zero rows are dropped",
			vectors:  [][]float32{{1, 0}, {0, 0}, {0, 1}},
			k:        2,
			wantDims: 2,
			wantKept: []int{0, 2},
		},
		{
			name:     "ragged rows are dropped down to the modal length",
			vectors:  [][]float32{{1, 0, 0}, {0, 1}, {0, 1, 0}, {1, 1, 1, 1}, {0, 0, 1}},
			k:        3,
			wantDims: 3,
			wantKept: []int{0, 2, 4},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := embedmap.PCA(tc.vectors, tc.k)

			if tc.wantEmpty {
				require.Equal(t, 0, res.Dims)
				require.Empty(t, res.Kept)
				require.Empty(t, res.Coords)
				return
			}

			require.Equal(t, tc.wantDims, res.Dims)
			require.Equal(t, tc.wantKept, res.Kept)
			require.Len(t, res.Coords, len(tc.wantKept))
			for _, c := range res.Coords {
				require.Len(t, c, tc.wantDims)
				for _, v := range c {
					require.False(t, math.IsNaN(v) || math.IsInf(v, 0), "projection must stay finite")
					if tc.allZero {
						require.InDelta(t, 0, v, 1e-9)
					}
				}
			}
		})
	}
}
