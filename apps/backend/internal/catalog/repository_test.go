package catalog

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildListToolsQueryUsesParameters(t *testing.T) {
	filter := ListToolsFilter{
		CategoryID:    "10000000-0000-4000-8000-000000000001",
		Search:        "bosch",
		AvailableOnly: true,
		Limit:         10,
		Offset:        20,
	}

	query, arguments := buildListToolsQuery(filter)

	for _, value := range []string{filter.CategoryID, filter.Search} {
		if strings.Contains(query, value) {
			t.Fatalf("query contains unparameterized value %q", value)
		}
	}

	expectedArguments := []any{filter.CategoryID, filter.Search, filter.Limit, filter.Offset}
	if !reflect.DeepEqual(arguments, expectedArguments) {
		t.Fatalf("unexpected arguments: %#v", arguments)
	}

	for _, fragment := range []string{
		"t.category_id = $1::uuid",
		"t.name ILIKE '%' || $2 || '%'",
		"FROM tool_images AS ti",
		"tu.status = 'available'",
		"HAVING COUNT(tu.id)",
		"LIMIT $3 OFFSET $4",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query does not contain %q", fragment)
		}
	}
}
