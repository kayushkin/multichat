package api

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadGoogleMessagesPhoneNumbersReadsTheGhostTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gmessages.db")
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE ghost (id TEXT, metadata TEXT)`,
		`INSERT INTO ghost VALUES ('1.5', '{"phone":"+12025550101"}'), ('1.6', '{}'), ('1.7', '{"phone":""}')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	numbers, err := readGoogleMessagesPhoneNumbers(path, "example.org")
	if err != nil {
		t.Fatal(err)
	}
	if want := []GoogleMessagesPhoneNumber{{UserID: "@gmessages_1.5:example.org", PhoneNumber: "+12025550101"}}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("%v", numbers)
	}
}
