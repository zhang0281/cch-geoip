// Creates synthetic MMDBs for reproducible tests, never production data.
package main

import (
	"log"
	"os"
	"time"

	"github.com/zhang0281/cch-geoip/internal/testdb"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: fixtures DIRECTORY [CITY]")
	}
	city := "Fixture City"
	if len(os.Args) > 2 {
		city = os.Args[2]
	}
	if _, _, err := testdb.Write(os.Args[1], city, time.Now()); err != nil {
		log.Fatal(err)
	}
}
