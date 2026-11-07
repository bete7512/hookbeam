package sorting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bete7512/hookbeam/pkg/sorting"
)

var columns = sorting.Columns{"id": "id", "email": "email", "createdAt": "created_at"}

func TestOrderBy(t *testing.T) {
	tests := []struct {
		name    string
		sortBy  string
		sortDir string
		want    string
	}{
		{name: "defaults to id asc", want: "id ASC"},
		{name: "id desc", sortBy: "id", sortDir: "desc", want: "id DESC"},
		{name: "column with id tie-breaker", sortBy: "email", sortDir: "desc", want: "email DESC, id DESC"},
		{name: "maps api key to column", sortBy: "createdAt", sortDir: "asc", want: "created_at ASC, id ASC"},
		{name: "unknown direction is asc", sortBy: "email", sortDir: "sideways", want: "email ASC, id ASC"},
		{name: "uppercase direction", sortBy: "email", sortDir: "DESC", want: "email DESC, id DESC"},
		{name: "mixed case key", sortBy: "CreatedAt", sortDir: "Desc", want: "created_at DESC, id DESC"},
		{name: "injection in sortBy falls back to id", sortBy: "email; DROP TABLE users", sortDir: "desc", want: "id DESC"},
		{name: "injection in sortDir is ignored", sortBy: "email", sortDir: "desc; DROP TABLE users", want: "email ASC, id ASC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, columns.OrderBy(tt.sortBy, tt.sortDir))
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		sortBy     string
		sortDir    string
		wantFields []string
	}{
		{name: "empty is valid"},
		{name: "known key and direction", sortBy: "email", sortDir: "desc"},
		{name: "case-insensitive key and direction", sortBy: "EMAIL", sortDir: "ASC"},
		{name: "unknown key", sortBy: "password", wantFields: []string{"sort_by"}},
		{name: "unknown direction", sortDir: "up", wantFields: []string{"sort_dir"}},
		{name: "both invalid", sortBy: "password", sortDir: "up", wantFields: []string{"sort_by", "sort_dir"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := columns.Validate(tt.sortBy, tt.sortDir)
			assert.Len(t, fields, len(tt.wantFields))
			for _, f := range tt.wantFields {
				assert.Contains(t, fields, f)
			}
		})
	}
}
