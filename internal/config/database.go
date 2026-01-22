package config

import "os"

type Database struct {
	MySQLURL string
}

func loadDatabase() Database {
	return Database{
		MySQLURL: os.Getenv("MYSQL_URL"),
	}
}
