package rag

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates every golden fixture in this package from the
// CURRENT implementation: `go test ./internal/application/rag/... -run Golden -update`.
var updateGolden = flag.Bool("update", false, "update golden fixtures")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file %s (run with -update)", path)
	assert.Equal(t, string(want), got)
}

type goldenFileStore struct {
	chunks []domain.FileChunk
}

func (f *goldenFileStore) Create(context.Context, string, string, int64) (domain.FileRecord, error) {
	return domain.FileRecord{}, nil
}
func (f *goldenFileStore) Get(context.Context, uuid.UUID) (domain.FileRecord, error) {
	return domain.FileRecord{}, nil
}
func (f *goldenFileStore) List(context.Context) ([]domain.FileRecord, error) { return nil, nil }
func (f *goldenFileStore) Delete(context.Context, uuid.UUID) error           { return nil }
func (f *goldenFileStore) SaveChunks(context.Context, uuid.UUID, []domain.FileChunk) error {
	return nil
}
func (f *goldenFileStore) SearchChunks(context.Context, []uuid.UUID, []float32, int) ([]domain.FileChunk, error) {
	return f.chunks, nil
}

type goldenLLM struct{}

func (goldenLLM) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (goldenLLM) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}
func (goldenLLM) Models(context.Context) ([]string, error) { return nil, nil }
func (goldenLLM) Embed(context.Context, string, string) ([]float32, error) {
	return []float32{0, 1}, nil
}

func TestGoldenInjectContextWrapper(t *testing.T) {
	fileID := uuid.New()

	single := &goldenFileStore{chunks: []domain.FileChunk{{FileID: fileID, Content: "The invoice total is $42."}}}
	svc := NewService(single, goldenLLM{}, domain.RAGConfig{TopK: 1})
	out, err := svc.InjectContext(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "what is the total?"}}, []string{fileID.String()})
	require.NoError(t, err)
	assertGolden(t, "inject_context_single_chunk", out[0].Content)

	multi := &goldenFileStore{chunks: []domain.FileChunk{
		{FileID: fileID, Content: "Chunk one content."},
		{FileID: fileID, Content: "Chunk two content."},
	}}
	svc2 := NewService(multi, goldenLLM{}, domain.RAGConfig{TopK: 2})
	out2, err := svc2.InjectContext(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "summarize"}}, []string{fileID.String()})
	require.NoError(t, err)
	assertGolden(t, "inject_context_multi_chunk", out2[0].Content)
}
