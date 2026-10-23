// Command multichat-backfill-room logs the whole history Synapse holds for
// the rooms it is given, so every message in them has a message log row and a
// ref (message_000123) a timeline or a note can cite. The live log starts at
// message_log_started_at; this reaches past it. Running it again adds nothing.
//
//	set -a; . ~/.config/multichat/multichat.env; set +a
//	SYNAPSE_URL=http://localhost:8008 multichat-backfill-room -db ~/repos/multichat/multichat.db -room '!abc:chat.kayushkin.com' -room '!def:chat.kayushkin.com'
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/config"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/inbound"
	"github.com/kayushkin/multichat/internal/matrix"
)

// roomList collects every -room flag.
type roomList []string

func (rooms *roomList) String() string { return strings.Join(*rooms, ",") }
func (rooms *roomList) Set(room string) error {
	if !strings.HasPrefix(room, "!") {
		return fmt.Errorf("%q is not a Matrix room id (they start with !)", room)
	}
	*rooms = append(*rooms, room)
	return nil
}

func main() {
	databasePath := flag.String("db", "", "multichat's SQLite database (the unit's MULTICHAT_DB)")
	var rooms roomList
	flag.Var(&rooms, "room", "a Matrix room id to backfill; repeat for more")
	flag.Parse()
	if *databasePath == "" || len(rooms) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	// SQLite creates a missing file; a backfill into a new empty database
	// would look like success.
	if _, err := os.Stat(*databasePath); err != nil {
		log.Fatal(err)
	}
	settings, err := config.NewRoomBackfillCommandSettingsRegistry(servicesettings.ProcessEnvironment())
	if err != nil {
		log.Fatal(err)
	}
	if err := settings.CheckRequired(); err != nil {
		log.Fatal(err)
	}
	database, err := db.Initialize(*databasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	worker := &inbound.SyncWorker{
		Matrix:   matrix.NewClient(settings.String(config.RoomBackfillSettingSynapseURL), settings.String(config.RoomBackfillSettingMatrixToken)),
		Database: database,
	}
	for _, room := range rooms {
		result, err := worker.BackfillRoom(room)
		if err != nil {
			log.Fatalf("%s: %v", room, err)
		}
		fmt.Printf("%s: %d events in Synapse, %d messages newly logged\n", room, result.EventsSeen, result.MessagesLogged)
	}
}
