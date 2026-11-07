---
name: add-list-endpoint
description: Use when adding a list/collection GET endpoint (paginated, searchable, filterable or sortable) to the api, or when adding a filter or sort field to an existing ListXOpts. Offset plus total. Load add-endpoint too when the resource itself is new.
---

# add-list-endpoint

Convention: offset pagination plus a total count. `repos.ListXOpts` flows handler → service → repo. The repo has `ListX` + `CountX` sharing one WHERE filter, and the response is `{items, total}`. Sorting goes through a `pkg/sorting.Columns` whitelist. Canonical example: `listUsers` (`paths/users.yaml`, `repos.ListUsersOpts`, `repos.UserSortColumns`, `models.UserForList`, `TestListUsers`/`TestCountUsers`). `Project`/`projects` is a placeholder.

## Steps

1. **Index** (large tables only): the default order `id ASC` uses the primary key. For a sort column that big tables are sorted by, run the `add-migration` skill to add `CREATE INDEX CONCURRENTLY IF NOT EXISTS projects_name_id_idx ON projects (name, id)` in a `goose.AddMigrationNoTxContext` migration. Use the concurrent-index template in the `add-migration` skill.
2. **Spec**: add a `get` (`operationId: listProjects`) to the collection path item with inline `limit`, `offset`, `search`, `sort_by` (enum) and `sort_dir` parameters copied from `listUsers` in `paths/users.yaml`. Add a `ProjectList` schema under `components: schemas:` in `paths/projects.yaml` (not `components/`) by copying `UserList`: `required: [items, total]`, `items` an array of `$ref: '#/components/schemas/Project'`, `total` an integer. Run `make lint-openapi && make gen-openapi`.
3. **Model + repo**: add `ProjectForList` to `models/project.go` with the same `db:` tags, holding only the columns the DTO shows (no secrets or hashes). Add `ListProjectsOpts`, `ProjectSortColumns`, `ListProjects` and `CountProjects` to the repo, and the methods to the `ProjectRepo` interface.
4. **Service + handler**: `ListProjects(ctx, opts) ([]models.ProjectForList, int, error)` and `ListProjects(w, r, params openapi.ListProjectsParams)`.
5. **Mocks + tests**: `go generate ./api/...`, then table-driven tests at all three layers (repo suites use `testutil.Postgres(s.T(), migrations.Up)`).
6. **Done**: `make gen-openapi` → `go generate ./api/...` → `make fmt` → `make lint` → `make test` → `make test-integration` → `make gen-check` → `verify-change` skill (`curl '.../v1/projects?limit=2&search=x&sort_by=name&sort_dir=desc'`).

## Spec

```yaml
projects:
  get:
    operationId: listProjects
    tags: [projects]
    summary: List projects
    description: Paginated with limit and offset, sorted by `sort_by` (default id) and `sort_dir` (default asc). `search` matches name.
    security:
      - bearerAuth: []
      - cookieAuth: []
    parameters:
      - name: limit
        in: query
        required: false
        schema:
          type: integer
          minimum: 1
          maximum: 100
          default: 20
      - name: offset
        in: query
        required: false
        schema:
          type: integer
          minimum: 0
          default: 0
      - name: search
        in: query
        required: false
        description: Case-insensitive match on name.
        schema:
          type: string
      - name: sort_by
        in: query
        required: false
        schema:
          type: string
          enum: [id, name, createdAt]
          default: id
      - name: sort_dir
        in: query
        required: false
        schema:
          type: string
          enum: [asc, desc]
          default: asc
    responses:
      '200':
        description: A page of projects.
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ProjectList'   # defined in this file's components:
      '400':
        $ref: '../components/responses.yaml#/components/responses/BadRequest'
      '401':
        $ref: '../components/responses.yaml#/components/responses/Unauthorized'
      '422':
        $ref: '../components/responses.yaml#/components/responses/ValidationFailed'
      '500':
        $ref: '../components/responses.yaml#/components/responses/InternalError'
```

- Every list parameter is inline on its operation; nothing is shared. Describe the matched search columns in the `search` parameter's `description`.
- `sort_by` is inline because its enum is per resource; the enum must equal the `ProjectSortColumns` keys.
- Each extra filter is a query parameter and generates a pointer field in `ListProjectsParams`.

## Repo

```go
// ListProjectsOpts filters, sorts and pages ListProjects and CountProjects.
type ListProjectsOpts struct {
	Limit   int
	Offset  int
	Search  string
	SortBy  string
	SortDir string
}

// ProjectSortColumns are the sort keys clients may use, mapped to columns.
var ProjectSortColumns = sorting.Columns{
	"id":        "id",
	"name":      "name",
	"createdAt": "created_at",
}

const (
	projectListColumns = `id, name, created_at, updated_at`
	projectListFilter  = `@search::text = '' OR name ILIKE '%' || @search::text || '%'`
)

func (r *projectRepo) ListProjects(ctx context.Context, opts ListProjectsOpts) ([]models.ProjectForList, error) {
	query := `
	SELECT ` + projectListColumns + ` FROM projects
	WHERE ` + projectListFilter + `
	ORDER BY ` + ProjectSortColumns.OrderBy(opts.SortBy, opts.SortDir) + `
	LIMIT @limit OFFSET @offset
	`
	args := pgx.NamedArgs{"search": opts.Search, "limit": opts.Limit, "offset": opts.Offset}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	projects, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.ProjectForList])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return projects, nil
}

func (r *projectRepo) CountProjects(ctx context.Context, opts ListProjectsOpts) (int, error) {
	query := `SELECT count(*) FROM projects WHERE ` + projectListFilter
	var count int
	if err := r.db.QueryRow(ctx, query, pgx.NamedArgs{"search": opts.Search}).Scan(&count); err != nil {
		return 0, db.HandleError(err)
	}
	return count, nil
}
```

- **Filter field**: add it to the opts struct and write it as a `NamedArgs` clause. Parenthesise the existing filter first: `(@search::text = '' OR ...) AND (@status::text = '' OR status = @status::text)`. Pass it in both the List and Count args.
- **Sort field**: add `"apiKey": "sql_column"` to `ProjectSortColumns` and the key to the spec enum. Nothing else changes.
- `OrderBy` never errors: an empty or unknown key sorts by `id`, any direction other than `desc` is `ASC`, and `, id <DIR>` is appended as a tie-breaker. Count ignores sort.

## Service

```go
// ListProjects returns a page of projects and the total number matching opts.
func (s *projectService) ListProjects(ctx context.Context, opts repos.ListProjectsOpts) ([]models.ProjectForList, int, error) {
	fields := map[string]string{}
	if opts.Limit < 1 || opts.Limit > 100 {
		fields["limit"] = "must be between 1 and 100"
	}
	if opts.Offset < 0 {
		fields["offset"] = "must not be negative"
	}
	maps.Copy(fields, repos.ProjectSortColumns.Validate(opts.SortBy, opts.SortDir))
	if len(fields) > 0 {
		return nil, 0, apperr.NewValidation(fields)
	}
	projects, err := s.deps.Projects.ListProjects(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	total, err := s.deps.Projects.CountProjects(ctx, opts)
	if err != nil {
		return nil, 0, apperr.NewInternal(err)
	}
	return projects, total, nil
}
```

`Validate` returns `sort_by` (lists allowed keys) and `sort_dir` ("must be asc or desc") field errors; empty values are valid. The generated binder does not check enums, so this is the 422 path.

## Handler

```go
// ListProjects returns a page of projects.
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request, params openapi.ListProjectsParams) {
	opts := repos.ListProjectsOpts{Limit: defaultPageSize}
	if params.Limit != nil {
		opts.Limit = *params.Limit
	}
	if params.Offset != nil {
		opts.Offset = *params.Offset
	}
	if params.Search != nil {
		opts.Search = *params.Search
	}
	if params.SortBy != nil {
		opts.SortBy = string(*params.SortBy)
	}
	if params.SortDir != nil {
		opts.SortDir = string(*params.SortDir)
	}
	projects, total, err := h.deps.Projects.ListProjects(r.Context(), opts)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.ProjectList{Items: make([]openapi.Project, 0, len(projects)), Total: total}
	for _, p := range projects {
		resp.Items = append(resp.Items, toProjectListDTO(p))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}
```

`defaultPageSize` already exists in `handlers/users.go`, so don't redeclare it. `make([]..., 0, n)` makes an empty page serialise as `[]`, not `null`.

## Tests (mirror users)

**Repo**: reuse the suite's `SetupSubTest` truncate and `seed`, and seed the same 3 names in every case (`alpha`, `beta`, `gamma`, in id order).

- **`TestListProjects`**: a table of `{name, opts, wantNames []string}`. Cases:
  - first page by id (`Limit: 2` → `{"alpha", "beta"}`), second page (`Offset: 2` → `{"gamma"}`), offset past end (`[]string{}`);
  - sort by column desc (`SortBy: "name", SortDir: "desc"` → `{"gamma", "beta", "alpha"}`);
  - injection-looking key falls back to id (`SortBy: "name; DROP TABLE projects", SortDir: "desc"` → id desc);
  - case-insensitive search, search with no match (`[]string{}`).
  - Collect names with `make([]string, 0, len(got))` so an empty page equals `[]string{}`.
- **`TestCountProjects`**: a separate table of `{name, opts, want int}` covering all rows (3), a search (1), and no match (0).

**Service**: a table of `{name, opts, setup, wantStatus, wantFields}`. Valid opts expect `ListProjects` then `CountProjects` with the same `opts`. 422 cases expect no repo call: `Limit: 0` / `Offset: -1` → `limit`, `offset`; `SortBy: "secret", SortDir: "sideways"` → `sort_by`, `sort_dir`. Add List and Count failure cases → 500.

**Handler**: `projects_test.go` `endpointCase` rows `list` (expects `repos.ListProjectsOpts{Limit: 20}`), `list with params` (`?limit=5&offset=10&search=x`), `list sorted` (`?sort_by=name&sort_dir=desc` → `{Limit: 20, SortBy: "name", SortDir: "desc"}`), and `list bad limit` (`?limit=abc` → 400). Add a `TestListProjectsResponse` that decodes `openapi.ProjectList`.

## Common mistakes

- Different WHERE clauses in List and Count, which makes `total` lie. Share the `projectListFilter` const.
- `fmt.Sprintf("ORDER BY %s", params.SortBy)` or any client string in SQL: SQL injection. Only `ProjectSortColumns.OrderBy` output goes after `ORDER BY`.
- A hand-written sort map or `switch` per repo. Use `sorting.Columns`; its `OrderBy` adds the `id` tie-breaker so pages don't repeat or skip rows.
- Spec `sort_by` enum and `ProjectSortColumns` keys drifting apart. Keep them identical.
- Naming a helper package or import `query`: repos use `query :=` locals, which shadow it (gocritic `importShadow`).
- Returning `[]models.Project` from a list: secrets end up one DTO mapping away from the wire. Use `ProjectForList`.
- Validating limit/offset/sort in the handler, or clamping silently. The service returns a single `apperr.NewValidation`.
- `@search` without `::text`: pgx can't infer the parameter type.
- One list test method per scenario, or a list test that also asserts counts. Use separate `TestListX` / `TestCountX` tables.
- Keyset cursors: not this repo's convention, even if an older doc says otherwise.
