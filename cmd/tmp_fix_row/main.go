package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dsn := os.Getenv("RENDER_STAGING_DATABASE_URL")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	var token string
	err = db.QueryRow(`SELECT token FROM users_rights WHERE user_id = $1 AND merchant_id = '2'`, os.Args[1]).Scan(&token)
	if err != nil {
		log.Fatal("query: ", err)
	}
	fmt.Println(token)
}
