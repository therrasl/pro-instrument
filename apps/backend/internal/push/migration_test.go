package push

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationDeduplicatesRepeatedStatusWebhooks(t *testing.T) {
	contents, err := os.ReadFile("../../migrations/000012_push_notifications.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	if !strings.Contains(sql, "UNIQUE (rental_request_id, status)") ||
		!strings.Contains(sql, "ON CONFLICT (rental_request_id, status) DO NOTHING") {
		t.Fatal("push outbox migration must deduplicate repeated rental status events")
	}
}
