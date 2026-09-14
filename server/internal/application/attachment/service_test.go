package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	port.AttachmentStore
	created   *domain.Attachment
	metas     map[uuid.UUID]domain.AttachmentMeta
	taskLinks map[uuid.UUID][]uuid.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		metas:     map[uuid.UUID]domain.AttachmentMeta{},
		taskLinks: map[uuid.UUID][]uuid.UUID{},
	}
}

func (f *fakeStore) Create(_ context.Context, att domain.Attachment) (domain.AttachmentMeta, error) {
	att.ID = uuid.New()
	f.created = &att
	return att.Meta(), nil
}

func (f *fakeStore) GetMeta(_ context.Context, id uuid.UUID) (domain.AttachmentMeta, error) {
	meta, ok := f.metas[id]
	if !ok {
		return domain.AttachmentMeta{}, errors.New("attachment not found")
	}
	return meta, nil
}

func (f *fakeStore) ListMetaByTask(_ context.Context, taskID uuid.UUID) ([]domain.AttachmentMeta, error) {
	var out []domain.AttachmentMeta
	for _, id := range f.taskLinks[taskID] {
		out = append(out, f.metas[id])
	}
	return out, nil
}

func (f *fakeStore) LinkTask(_ context.Context, taskID, attachmentID uuid.UUID, _ int) error {
	f.taskLinks[taskID] = append(f.taskLinks[taskID], attachmentID)
	return nil
}

type fakeTasks struct {
	port.BoardTaskStore
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *fakeTasks) Get(_ context.Context, _ uuid.UUID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, errors.New("task not found")
	}
	return task, nil
}

var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

func TestUpload_RejectsOverSizeCap(t *testing.T) {
	svc := NewService(newFakeStore(), nil)

	_, err := svc.Upload(context.Background(), "big.png", "image/png", make([]byte, domain.MaxAttachmentBytes+1), nil, "user", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAttachmentTooLarge)
}

func TestUpload_RejectsDisallowedContentType(t *testing.T) {
	svc := NewService(newFakeStore(), nil)

	_, err := svc.Upload(context.Background(), "evil.exe", "application/x-msdownload", []byte("MZ..."), nil, "user", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAttachmentTypeNotAllowed)
}

func TestUpload_SniffsWhenClientTypeMissing(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil)

	meta, err := svc.Upload(context.Background(), "shot.png", "", pngHeader, nil, "user", "")

	require.NoError(t, err)
	assert.Equal(t, "image/png", meta.ContentType)
}

func TestUpload_SniffsWhenClientTypeGeneric(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil)

	meta, err := svc.Upload(context.Background(), "shot.png", "application/octet-stream", pngHeader, nil, "user", "")

	require.NoError(t, err)
	assert.Equal(t, "image/png", meta.ContentType)
}

func TestUpload_KeepsDeclaredAllowlistedType(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil)

	meta, err := svc.Upload(context.Background(), "notes.md", "text/markdown; charset=utf-8", []byte("# hi"), nil, "user", "")

	require.NoError(t, err)
	assert.Equal(t, "text/markdown", meta.ContentType)
}

func TestUpload_RejectsUnsniffableGenericBytes(t *testing.T) {
	svc := NewService(newFakeStore(), nil)

	_, err := svc.Upload(context.Background(), "blob.bin", "application/octet-stream", []byte{0x00, 0x01, 0x02, 0x03}, nil, "user", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrAttachmentTypeNotAllowed)
}

func TestUpload_RejectsEmptyBody(t *testing.T) {
	svc := NewService(newFakeStore(), nil)

	_, err := svc.Upload(context.Background(), "empty.png", "image/png", nil, nil, "user", "")

	require.Error(t, err)
}

func TestUpload_ComputesSHA256AndDefaults(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, nil)
	sum := sha256.Sum256(pngHeader)

	meta, err := svc.Upload(context.Background(), "  ", "image/png", pngHeader, nil, "", "")

	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), meta.SHA256)
	assert.Equal(t, int64(len(pngHeader)), meta.SizeBytes)
	assert.Equal(t, "user", store.created.CreatedByType)
	assert.Equal(t, "attachment", store.created.Filename)
}

func TestLinkTask_VerifiesTaskExists(t *testing.T) {
	store := newFakeStore()
	attachmentID := uuid.New()
	store.metas[attachmentID] = domain.AttachmentMeta{ID: attachmentID, Filename: "shot.png"}
	svc := NewService(store, &fakeTasks{tasks: map[uuid.UUID]domain.BoardTask{}})

	_, err := svc.LinkTask(context.Background(), uuid.New(), uuid.New(), attachmentID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestLinkTask_AppendsAfterExistingAttachments(t *testing.T) {
	store := newFakeStore()
	taskID := uuid.New()
	repositoryID := uuid.New()
	first, second := uuid.New(), uuid.New()
	store.metas[first] = domain.AttachmentMeta{ID: first}
	store.metas[second] = domain.AttachmentMeta{ID: second}
	svc := NewService(store, &fakeTasks{tasks: map[uuid.UUID]domain.BoardTask{taskID: {ID: taskID}}})

	_, err := svc.LinkTask(context.Background(), repositoryID, taskID, first)
	require.NoError(t, err)
	meta, err := svc.LinkTask(context.Background(), repositoryID, taskID, second)
	require.NoError(t, err)

	assert.Equal(t, second, meta.ID)
	assert.Equal(t, []uuid.UUID{first, second}, store.taskLinks[taskID])
}

func TestLinkTask_UnknownAttachmentFails(t *testing.T) {
	store := newFakeStore()
	taskID := uuid.New()
	svc := NewService(store, &fakeTasks{tasks: map[uuid.UUID]domain.BoardTask{taskID: {ID: taskID}}})

	_, err := svc.LinkTask(context.Background(), uuid.New(), taskID, uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "attachment not found")
}
