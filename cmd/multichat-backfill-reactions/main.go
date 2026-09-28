// Command multichat-backfill-reactions logs the reactions made since the
// message log began, in every room @admin has joined, through the sync
// worker's own code. It exists for the stretch between message_log_started_at
// and the deploy that taught the worker to ask for m.reaction (2026-09-28,
// 17:15 to 17:56 UTC). Running it again changes nothing.
//
//	set -a; . ~/.config/multichat/multichat.env; set +a
//	SYNAPSE_URL=http://localhost:8008 multichat-backfill-reactions -db ~/repos/multichat/multichat.db
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/kayushkin/llm-bridge/servicesettings"
	"github.com/kayushkin/multichat/internal/config"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/inbound"
	"github.com/kayushkin/multichat/internal/matrix"
)

func main() {
	databasePath := flag.String("db", "", "multichat's SQLite database (the unit's MULTICHAT_DB)")
	flag.Parse()
	if *databasePath == "" {
		flag.Usage()
		os.Exit(2)
	}
	// SQLite creates a missing file; a backfill into a new empty database
	// would look like success.
	if _, err := os.Stat(*databasePath); err != nil {
		log.Fatal(err)
	}
	settings, err := config.NewReactionBackfillCommandSettingsRegistry(servicesettings.ProcessEnvironment())
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
	since, err := db.RecordedMessageLogStartedAt(database)
	if err != nil {
		log.Fatal(err)
	}
	worker := &inbound.SyncWorker{
		Matrix:   matrix.NewClient(settings.String(config.ReactionBackfillSettingSynapseURL), settings.String(config.ReactionBackfillSettingMatrixToken)),
		Database: database,
	}
	result, err := worker.BackfillReactions(since)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("since %s: read %d rooms, saw %d reactions and %d redactions; logged %d new reactions, marked %d removed\n",
		since.Format("2006-01-02 15:04:05Z07:00"), result.RoomsRead, result.ReactionsSeen, result.RedactionsSeen,
		result.ReactionsLogged, result.ReactionsMarkedOff)
}
