---
name: add-endpoint
description: Use when adding an HTTP endpoint to the api app (api/cmd/api + api/), either a new operation on an existing resource or a whole new resource (spec, model, repo, service, handler, wiring, tests). For list/paginated GET endpoints also load add-list-endpoint.
---

Canonical example: `users`. Copy its shape exactly: `api/openapi/paths/users.yaml`, `api/{models/user.go,repos/users.go,services/users.go,handlers/users.go}` and their tests. `Project`/`projects` is a placeholder.

A new business area with its own data is a new service folder at the repository root (copy the `api/` shape), not a new folder inside an existing service.

## Steps

1. **Table** (new resource only): run the `add-migration` skill. Columns `id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`, `created_at`/`updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`, plus trigger `update_projects_updated_at` on `update_updated_at_column()`.
2. **Spec**: add path items to `api/openapi/paths/projects.yaml`, reference each from `openapi.yaml` `paths:` (plus a `tags:` entry). `Project`, `CreateProjectRequest`, `UpdateProjectRequest` schemas and the `ProjectId` param go in the same file under `components:` with local `#/components/...` refs (camelCase, `required:` list; optional fields generate pointers; `ProjectId` copies `UserId`: `type: integer, format: int64, minimum: 1`); id properties are `type: integer, format: int64`. Every error response is a `$ref` into `../components/responses.yaml`; `components/` holds shared pieces only. Every op declares `security`: public → `security: []` **and** add its id (PascalCase, e.g. `ListProjects`) to `publicOperations` in `handlers/authn_test.go`, or `TestPublicOperationsArePinned` fails; protected → `bearerAuth` + `cookieAuth` (plus a `401` response). The auth middleware reads it; nothing else to wire. Run `make lint-openapi && make gen-openapi`; the compiler then lists the missing handler methods.
3. **Code**, in this order, under `api/`: `models/project.go` (struct with `db:` tags) → `repos/projects.go` → `services/projects.go` → `handlers/projects.go`.
4. **Wiring + mocks**: add a `Projects services.ProjectService` field to `handlers.Deps` and its nil check in `handlers.New`; in `api/cmd/api/main.go` `build`, construct `repos.NewProjectRepo(pool)` → `services.NewProjectService(...)` and pass it as `Projects:` in the returned `handlers.Deps` (the worker's `build` gets the same two lines only if a job needs the service). Run `go generate ./api/...`.
5. **Tests** (table-driven at all three layers, see Tests), then **Done**: `make gen-openapi` → `go generate ./api/...` → `make fmt` → `make lint` → `make test` → `make test-integration` → `make gen-check` → `verify-change` skill for a live curl.

## Spec (`paths/projects.yaml`)

```yaml
projects:
  post:
    operationId: createProject
    tags: [projects]
    summary: Create a project
    security:          # public op: `security: []`
      - bearerAuth: []
      - cookieAuth: []
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/CreateProjectRequest'
    responses:
      '201':
        description: Project created.
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Project'
      '400':
        $ref: '../components/responses.yaml#/components/responses/BadRequest'
      '401':
        $ref: '../components/responses.yaml#/components/responses/Unauthorized'
      '409':
        $ref: '../components/responses.yaml#/components/responses/Conflict'
      '422':
        $ref: '../components/responses.yaml#/components/responses/ValidationFailed'
      '500':
        $ref: '../components/responses.yaml#/components/responses/InternalError'

components:
  parameters:
    ProjectId:          # copy UserId
      name: id
      in: path
      required: true
      schema:
        type: integer
        format: int64
        minimum: 1
  schemas:
    Project: ...               # copy User
    CreateProjectRequest: ...  # copy CreateUserRequest
    UpdateProjectRequest: ...  # copy UpdateUserRequest
```

- **`project:` item**: copy `users.yaml#/user`, with `parameters: [$ref: '#/components/parameters/ProjectId']` at item level.
  - `get` 200 `Project` | 400 401 404 500; `patch` 200 | 400 401 404 422 500; `delete` 204 | 400 401 404 500.
- **`openapi.yaml`**: map `/v1/projects` → `'./paths/projects.yaml#/projects'` and `/v1/projects/{id}` → `'./paths/projects.yaml#/project'`.

## Repo

```go
//go:generate mockgen -source=projects.go -destination=repo_mocks/mock_projects.go -package=repo_mocks
const projectColumns = `id, name, description, created_at, updated_at`

func (r *projectRepo) CreateProject(ctx context.Context, project *models.Project) error {
	query := `INSERT INTO projects (name, description) VALUES (@name, @description) RETURNING id, created_at, updated_at`
	args := pgx.NamedArgs{"name": project.Name, "description": project.Description}
	err := r.db.QueryRow(ctx, query, args).Scan(&project.ID, &project.CreatedAt, &project.UpdatedAt)
	return db.HandleError(err)
}
```

- **Shape**: exported `ProjectRepo` interface (`CreateProject(ctx, *models.Project) error`, `GetProject(ctx, id int64) (*models.Project, error)`, `UpdateProject`, `DeleteProject`), unexported `projectRepo struct{ db db.DBTX }`, and `NewProjectRepo(conn db.DBTX) (ProjectRepo, error)` rejecting nil. Copy `UserRepo`.
- **Get**: `r.db.Query(ctx, "SELECT "+projectColumns+" FROM projects WHERE id = @id", pgx.NamedArgs{"id": id})`, then `pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Project])`. Both errors go through `db.HandleError` (no rows maps to `ErrNotFound`).
- **Update**: `UPDATE ... RETURNING ` + projectColumns via `Query` + `CollectOneRow`, then `*project = updated`. **Delete**: `Exec`, and `tag.RowsAffected() == 0` returns `apperr.ErrNotFound`.

## Service

```go
//go:generate mockgen -source=projects.go -destination=service_mocks/mock_projects.go -package=service_mocks
func (s *projectService) CreateProject(ctx context.Context, project *models.Project) error {
	project.Name = strings.TrimSpace(project.Name)
	if project.Name == "" {
		return apperr.NewValidation(map[string]string{"name": "is required"})
	}
	err := s.deps.Projects.CreateProject(ctx, project)
	if errors.Is(err, apperr.ErrConflict) {
		return apperr.NewConflict("project name already taken", err)
	}
	if err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

// projectError maps a repo error for a single project to an AppError.
func projectError(err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound("project", err)
	}
	return apperr.NewInternal(err)
}
```

- **Shape**: `ProjectService` interface mirroring the repo, `ProjectServiceDeps{Projects repos.ProjectRepo; Logger *slog.Logger}`, `projectService struct{ deps ProjectServiceDeps }`, and `NewProjectService` with one `if deps.Projects == nil || deps.Logger == nil` → `errors.New(...)`.
- **Validation and errors**: collect several invalid fields into one `fields` map and return a single `NewValidation`, keyed by JSON names (see `Register`). Get and Delete return `projectError(err)`; Update validates and maps `ErrConflict` like Create, otherwise `projectError`.

## Handler

```go
// CreateProject creates a project.
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req openapi.CreateProjectRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	project := &models.Project{Name: req.Name, Description: req.Description}
	if err := h.deps.Projects.CreateProject(r.Context(), project); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toProjectDTO(project))
}
```

Path-param methods take `id openapi.ProjectId` (an alias of `int64`); model `ID int64 \`db:"id"\``. Get and Update write 200 with `toProjectDTO(p *models.Project) openapi.Project`, which goes at the bottom of the file. Delete ends with `w.WriteHeader(http.StatusNoContent)`.

Protected routes needing the caller (copy `GetMe`):

```go
a, err := actor(r) // handlers/authn.go: a user session with an active workspace, else 403
if err != nil {
	httpx.WriteError(w, r, err)
	return
}
// pass a (services.Actor{UserID, WorkspaceID}) to the service; it scopes every query by WorkspaceID
```

A route that API keys may call declares `apiKeyAuth` in its `security` and reads `principal(r)` instead (`p.OrganizationID` is the workspace, `p.UserID` is empty for a key). Add its operation id to `apiKeyOperations` in `handlers/authn_test.go`.

- Never read `Authorization` or cookies in a handler; the middleware already enforced the spec's `security`.

## Tests

**Repo** (`api/repos/projects_test.go`, `//go:build integration`, `package repos_test`): copy the `OutboxRepoTestSuite` struct and `SetupSuite`. **No `TestMain`**: outbox_test.go already declares it in this package.

```go
import "github.com/bete7512/hookbeam/api/migrations"

func (s *ProjectRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewProjectRepo(s.db)
	s.Require().NoError(err)
}

func (s *ProjectRepoTestSuite) SetupSubTest() { testutil.Truncate(s.T(), s.db, "projects") }

func (s *ProjectRepoTestSuite) seed(names ...string) []*models.Project {
	projects := make([]*models.Project, 0, len(names))
	for _, name := range names {
		project := &models.Project{Name: name}
		s.Require().NoError(s.repo.CreateProject(context.Background(), project))
		projects = append(projects, project)
	}
	return projects
}

func (s *ProjectRepoTestSuite) TestGetProject() {
	tests := []struct {
		name    string
		seed    bool
		wantErr error
	}{
		{name: "found", seed: true},
		{name: "not found", wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			id := int64(-1)
			if tt.seed {
				id = s.seed("apollo")[0].ID
			}

			got, err := s.repo.GetProject(context.Background(), id)

			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal("apollo", got.Name)
		})
	}
}
```

- Write one table method per repo method with the same loop:
  - Create: a `seed` field, `s.Positive(project.ID)`, and duplicate → `ErrConflict`.
  - Update: asserts `UpdatedAt.After(CreatedAt)`.
  - Delete: a follow-up Get returns `ErrNotFound`.

**Service** (`api/services/projects_test.go`, `package services_test`): `ProjectServiceSuite` with `projects *repo_mocks.MockProjectRepo` (`github.com/bete7512/hookbeam/api/repos/repo_mocks`).
- Build the mock and `svc` in `SetupSubTest` so every case gets a fresh controller, and copy `requireErr`.
- One table per method: `{name, input, setup func(), wantStatus int, wantFields []string}`. `setup` sets only the repo `EXPECT`s that case needs; then `s.requireErr(err, tt.wantStatus, tt.wantFields)` (expects no error when `wantStatus` is 0). Example: `services/events_test.go`.
- Cover validation → 422 (no repo call), `ErrConflict` → 409, `ErrNotFound` → 404, `errors.New("boom")` → 500, and success.

**Handler**: in `api/handlers/handler_test.go` add `projects *service_mocks.MockProjectService` (`github.com/bete7512/hookbeam/api/services/service_mocks`) to `fixture`, create it in `newFixture`, and pass `Projects: f.projects`. Add `projects_test.go` with `runEndpoints(t, []endpointCase{...})` rows for: success per op, malformed JSON 400, non-numeric id (`/v1/projects/nope`) 400, 404, 422. Build paths with `strconv.FormatInt(id, 10)`.
- `f.do` already sends `Authorization: Bearer good`; the fixture's `fakeAuth` maps `"good"` to the `ada` principal (user `u_ada` in workspace `ws_1`, `adaActor` in `api_keys_test.go`). Clear `f.auth.principal.OrganizationID` for a "select a workspace" case; use `f.apiKeys.EXPECT().Authenticate(...)` with a `Bearer hb_...` request for an API-key case.
- Put the cases in a new `projects_test.go` as `func TestProjects(t *testing.T) { runEndpoints(t, []endpointCase{...}) }` (never in `handler_test.go`, which holds only the fixture and runner). Every `endpointCase` ends with an `auth bool`: `true` for a protected route (the table first sends it without a token and requires 401), `false` for public routes and for cases whose param binding fails before the middleware runs (bad id, bad limit).
- Use `f.send(req)` for requests without a token or with a cookie.
- Add a `TestAuthentication` row only when the new route's access differs (public, optional, or reads claims in a new way).

## Checklist

- [ ] Resource belongs to this service's business area; a new area with its own data goes in a new root service folder.
- [ ] Spec referenced from `openapi.yaml` (otherwise nothing is generated, silently); `api/gen/openapi` and mocks regenerated and committed.
- [ ] Errors: repo → sentinel via `db.HandleError`, service → `*apperr.AppError`, handler → `httpx.WriteError`. Tests table-driven per method; Done sequence green.

## Common mistakes

- Choosing a status in the handler (`w.WriteHeader(404)`), or logging in a service or handler. `httpx.WriteError` maps the status and the middleware logs.
- `fmt.Errorf("...: %v", err)` drops the sentinel, so a 404 becomes a 500. Use `%w`, and classify via `projectError`.
- Editing `api/gen/` by hand, or skipping `go generate` after an interface change (the stale mock breaks the build).
- Scenario-per-method repo tests (`TestNotFound`), truncating in `SetupTest` (rows leak between `s.Run` cases), redeclaring `TestMain` in a second `repos_test` file (compile error under `-tags integration`), or adding id helpers for found/not-found cases (use a `seed bool` field and `int64(-1)`).
- Assuming `format: email` validates (it generates a plain `string`, so validate in the service), setting `updated_at` in SQL (the trigger owns it), or forgetting the fixture's `Projects:` field or the `Core.Projects` → `handlers.Deps` pass in `api/cmd/api/main.go`.
- Omitting `security`: the op then requires login and `TestEveryOperationDeclaresSecurity` fails, so public ops must say `security: []`. Or checking tokens in a handler instead of `principal(r)`.
- Putting resource schemas or params in `components/*.yaml` (shared pieces only), or reusing a schema name already defined in another file (they collide in the bundle).
