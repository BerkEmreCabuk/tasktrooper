package http

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/newrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeNewRepoCreator struct {
	err  error
	repo domain.Repository
}

func (f *fakeNewRepoCreator) CreateWithoutScan(_ context.Context, req domain.CreateRepositoryRequest) (domain.Repository, error) {
	if f.err != nil {
		return domain.Repository{}, f.err
	}
	name, err := domain.NewRepoDirName(req.Name)
	if err != nil {
		return domain.Repository{}, err
	}
	f.repo = domain.Repository{ID: uuid.New(), Name: name, Description: req.Description, Kind: req.Kind}
	return f.repo, nil
}

func (f *fakeNewRepoCreator) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, nil
}

type fakeNewRepoComponents struct {
	id  uuid.UUID
	err error
}

func (f *fakeNewRepoComponents) AddComponent(_ context.Context, repoID uuid.UUID, req domain.NewComponentRequest) (domain.Component, error) {
	if f.err != nil {
		return domain.Component{}, f.err
	}
	f.id = uuid.New()
	return domain.Component{ID: f.id, RepositoryID: repoID, Path: req.Path}, nil
}

func (f *fakeNewRepoComponents) UpdateComponent(_ context.Context, id uuid.UUID, _ domain.ComponentPatch) (domain.Component, error) {
	return domain.Component{ID: id}, nil
}

func newNewRepoTestApp(creator *fakeNewRepoCreator, components *fakeNewRepoComponents) (*fiber.App, *fakeRepoDocsTasks) {
	tasks := &fakeRepoDocsTasks{}
	h := &Handler{newRepoSvc: newrepo.NewService(creator, components, tasks)}
	app := fiber.New()
	h.registerNewRepositoryRoute(app)
	return app, tasks
}

type newRepoResponse struct {
	Repository  domain.Repository `json:"repository"`
	ComponentID string            `json:"component_id"`
	Task        *struct {
		ID    string `json:"id"`
		Key   string `json:"key"`
		Title string `json:"title"`
	} `json:"task"`
}

func TestCreateNewRepositoryReturnsTheRepositoryComponentAndTask(t *testing.T) {
	components := &fakeNewRepoComponents{}
	app, tasks := newNewRepoTestApp(&fakeNewRepoCreator{}, components)

	rec := postJSON(t, app, "/v1/repositories/new", `{"name":"My App","owner":"acme","description":"shop","role":"frontend",
		"stack":"Next.js 15, TypeScript","notes":"pnpm","scaffold":true,"docs":["coding_standards","local_run"]}`)
	require.Equal(t, fiber.StatusCreated, rec.Code, rec.Body.String())

	var body newRepoResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "my-app", body.Repository.Name)
	require.Equal(t, components.id.String(), body.ComponentID)
	require.NotNil(t, body.Task)
	require.Equal(t, tasks.task.ID.String(), body.Task.ID)
	require.Equal(t, "T-1", body.Task.Key)
	require.Equal(t, "Set up my-app", body.Task.Title)
}

func TestCreateNewRepositoryReturnsANullTaskWhenNothingWasAskedFor(t *testing.T) {
	app, _ := newNewRepoTestApp(&fakeNewRepoCreator{}, &fakeNewRepoComponents{})

	rec := postJSON(t, app, "/v1/repositories/new", `{"name":"lib","role":"library","scaffold":false,"docs":[]}`)
	require.Equal(t, fiber.StatusCreated, rec.Code, rec.Body.String())

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Equal(t, "null", string(raw["task"]))
}

func TestCreateNewRepositoryStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		creatorErr error
		compErr    error
		want       int
		message    string
	}{
		{"unknown role", `{"name":"app","role":"wizard"}`, nil, nil, fiber.StatusBadRequest, `unknown role "wizard"`},
		{"unknown doc", `{"name":"app","role":"backend","docs":["vibes"]}`, nil, nil, fiber.StatusBadRequest, `unknown doc kind "vibes"`},
		{"git failure", `{"name":"app","role":"backend"}`, errors.New("git/GitHub setup failed: boom"), nil, fiber.StatusBadRequest, "git/GitHub setup failed: boom"},
		{"after creation", `{"name":"app","role":"backend"}`, nil, errors.New("db down"), fiber.StatusInternalServerError, `repository "app" was created`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := newNewRepoTestApp(&fakeNewRepoCreator{err: tt.creatorErr}, &fakeNewRepoComponents{err: tt.compErr})
			rec := postJSON(t, app, "/v1/repositories/new", tt.body)
			require.Equal(t, tt.want, rec.Code)
			var body errorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Contains(t, body.Error.Message, tt.message)
		})
	}
}
