package main

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// Global variable to hold CER data
var Coef []CER

// Struct to hold the index (CER) to adjust the face value of indexed bonds.
type CER struct {
	Date time.Time
	CER  float64
}

func getCoefficient(date time.Time, extendIndex float64, coef *[]CER) (float64, error) {
	for i := len(*coef) - 1; i >= 0; i-- {
		if (*coef)[i].Date == date {
			return (*coef)[i].CER, nil
		}
	}
	// Calculate the difference in days between date variable and the last date in the index.
	diffDays := date.Sub((*coef)[len(*coef)-1].Date).Hours() / 24
	newCoef := (*coef)[len(*coef)-1].CER * (math.Pow(1+extendIndex/365, diffDays/365))

	return newCoef, nil
}

func getCER() error {
	// Connect to PostgreSQL database using environment variables
	dbUser := os.Getenv("POSTGRES_USER")
	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	dbHost := os.Getenv("POSTGRES_HOST")
	dbPort := os.Getenv("POSTGRES_PORT")
	dbName := os.Getenv("POSTGRES_DB")
	fmt.Println("Host: ", dbHost)
	fmt.Println("Port: ", dbPort)
	fmt.Println("DB: ", dbName)

	// Connection string for PostgreSQL
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	// Open a connection to the PostgreSQL database
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		fmt.Println("Error opening database:", err)
		return err
	}
	defer db.Close()

	// Query the "CER" table
	rows, err := db.Query(`SELECT date, "CER" FROM "CER"`) // Using escaped quotes for table name
	if err != nil {
		fmt.Println("Error querying CER table:", err)
		return err
	}
	defer rows.Close()

	// Clear previous CER data
	if len(Coef) > 0 {
		Coef = nil
	}

	// Iterate through the query results and populate the Coef global variable
	for rows.Next() {
		var dateTimestamp time.Time // Changed to time.Time directly for PostgreSQL
		var cerValue float64

		// Scan the values from the row
		if err := rows.Scan(&dateTimestamp, &cerValue); err != nil {
			fmt.Println("Error scanning row:", err)
			return err
		}

		// Convert the date to UTC to strip timezone info
		dateTimestamp = dateTimestamp.UTC() // Ensure the date is in UTC

		// Create a CER struct and populate its fields
		var coef CER
		coef.Date = dateTimestamp // Use date directly from PostgreSQL
		coef.CER = cerValue

		// Append to the Coef slice
		Coef = append(Coef, coef)
	}

	fmt.Println("Total Records in table: ", len(Coef))
	fmt.Println()
	fmt.Println("Last Record in table: ")
	fmt.Println("Fecha: ", Coef[len(Coef)-1].Date.Format(DateFormat))
	fmt.Println("CER: ", Coef[len(Coef)-1].CER)
	fmt.Println()

	return nil
}
