package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	// Parse basic env or hardcode from .env
	connStr := "host=127.0.0.1 port=2345 user=wchsaas password=wchsaas123 dbname=wchsaasbot_db sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Cannot ping db: %v", err)
	}

	files := []string{
		"database/migrations/000005_add_exchange_to_trading_pairs.up.sql",
		"database/migrations/000006_add_report_interval.up.sql",
	}

	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			log.Fatalf("Cannot read %s: %v", file, err)
		}
		
		queries := strings.Split(string(content), ";")
		for _, q := range queries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			_, err = db.Exec(q)
			if err != nil {
				log.Printf("Warning executing query in %s: %v\nQuery: %s", file, err, q)
			} else {
				fmt.Printf("Executed query from %s successfully.\n", file)
			}
		}
	}
	fmt.Println("Done")
}
