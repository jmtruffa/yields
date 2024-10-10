package main

import (
	"database/sql"
	"fmt"
	"math"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var Coef []CER

// struct to hold the index (CER) to adjust the face value of indexed bonds.
type CER struct {
	Date time.Time
	//Country string
	CER float64
}

func getCoefficient(date time.Time, extendIndex float64, coef *[]CER) (float64, error) {
	//func getCoefficient(date Fecha, coef *[]CER) (float64, error) {
	for i := len(*coef) - 1; i >= 0; i-- {
		if (*coef)[i].Date == date {
			return (*coef)[i].CER, nil
		}
	}
	// The Index was not found. Return the last index value found
	// Date is already checked for correct format on the calling function

	// Calculate the difference in days between date variable and the last date in the index.
	diffDays := date.Sub((*coef)[len(*coef)-1].Date).Hours() / 24
	newCoef := (*coef)[len(*coef)-1].CER * (math.Pow(1+extendIndex/365, diffDays/365))

	return newCoef, nil
	//fmt.Errorf("CER not found for date %v and it was impossible to calculate it from the extended Index", date)
}

func getCER() error {

	// run the python script to get the CER data
	// CER is updated automatically every day at 5:00 PM by a cron job in desktoplinux
	//cmd := exec.Command("python3", "CERDownloader.py")

	//fmt.Println("Running CERDownloader.py...")
	//fmt.Println()

	// err := cmd.Run()
	// if err != nil {
	// 	fmt.Println("Error running CERDownloader.py:", err)
	// 	return err
	// }

	//fmt.Println("CERDownloader.py ran successfully")
	//fmt.Println()

	db, err := sql.Open("sqlite3", "/Users/juan/data/economicData.sqlite3")
	if err != nil {
		fmt.Println("Error opening database:", err)
		return err
	}
	defer db.Close()

	// Query the "CER" table
	rows, err := db.Query("SELECT date, CER FROM CER2")
	if err != nil {
		fmt.Println("Error querying CER table:", err)
		return err
	}
	defer rows.Close()

	// Iterate through the query results and populate the Coef global variable
	// if Coef is created, empty it. this will empty Coef if the function is called from the cron job
	if len(Coef) > 0 {
		Coef = nil
	}

	for rows.Next() {
		var dateTimestamp int64
		var cerValue float64

		// Scan the values from the row
		if err := rows.Scan(&dateTimestamp, &cerValue); err != nil {
			fmt.Println("Error scanning row:", err)
			return err
		}

		// Convert the timestamp to time.Time
		dateTime := time.Unix(dateTimestamp, 0).UTC()

		// Create a CER struct and populate its fields
		var coef CER
		coef.Date = time.Time(dateTime)

		coef.CER = cerValue

		// Append to the Coef slice
		Coef = append(Coef, coef)
	}

	fmt.Println("Total Records in file: ", len(Coef))
	fmt.Println()
	fmt.Println("Last Record in file: ")
	fmt.Println("Fecha: ", Coef[len(Coef)-1].Date.Format(DateFormat))
	fmt.Println("CER: ", Coef[len(Coef)-1].CER)
	fmt.Println()

	return nil
}
