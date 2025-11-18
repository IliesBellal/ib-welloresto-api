package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := os.Getenv("postgresql://wello_resto_testing_user:WXxtMQG7aibJDzOAJEJ13ADI3kKLNKwv@dpg-d4e5fqumcj7s73cfpe8g-a.frankfurt-postgres.render.com/wello_resto_testing")
	if dbURL == "" {
		log.Fatal("Missing DATABASE_URL")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	log.Println("Server running on port " + os.Getenv("PORT"))
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}