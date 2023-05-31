package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"
)

var Coef []CER

// struct to hold the index (CER) to adjust the face value of indexed bonds.
type CER struct {
	Date Fecha
	//Country string
	CER float64
}

func getCoefficient(date Fecha, extendIndex float64, coef *[]CER) (float64, error) {
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
	newCoef := (*coef)[len(*coef)-1].CER * (math.Pow(1+extendIndex, diffDays/365))

	return newCoef, nil
	//fmt.Errorf("CER not found for date %v and it was impossible to calculate it from the extended Index", date)
}

func getCER() error {
	var saveFile bool
	var downloadFile bool
	var reader *csv.Reader
	file := "/Users/juan/Google Drive/Mi unidad/analisis financieros/functions/data/CER.csv"
	fileInfo, _ := os.Stat(file)

	if fileInfo == nil {
		fmt.Println("No previous file found. Downloading...")
		downloadFile = true
		saveFile = true
	} else {
		modTime := fileInfo.ModTime()
		// calculate the time difference
		diff := time.Since(modTime)

		if diff < 120*time.Hour { // 5 days -> 24 * 5
			// grab the file from disk
			fmt.Println("The file is newer than 5 days old. Grabbing from disk...")
			res, error := os.Open(file)
			if error != nil {
				fmt.Println("Error opening CSV file: ", error)
				return error
			}
			defer res.Close()
			reader = csv.NewReader(res)
			saveFile = false
			downloadFile = false

		} else {
			// download the file again
			fmt.Println("The file is older than 5 days. Downloading...")
			downloadFile = true
			saveFile = true
		}
	}
	if downloadFile {
		// download the file again

		//apiKey := os.Getenv("ALPHACAST_API_KEY")
		apiKey := "ak_PrhTBPgElKK60m8iWqqI"

		url := "https://api.alphacast.io/datasets/8277/data?apiKey=" + apiKey + "&%24select=3290015&$format=csv"
		dataset := http.Client{
			Timeout: time.Second * 10,
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			fmt.Println("Error descargando CER.")
			return err
		}

		res, getErr := dataset.Do(req)
		if getErr != nil {
			fmt.Println("Error descargando CER.")
			return getErr
		}

		if res.Body != nil {
			defer res.Body.Close()
		}
		reader = csv.NewReader(res.Body)
		saveFile = true

	}

	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		fmt.Println("Falla en el ReadAll. ", err)
	}

	//var coefs []CER

	if saveFile {
		f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Println("Error creating file: ", err)
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		w.WriteAll(rows)
		w.Flush()
	}

	// Populate the Coef global variable with the data grabbed from disk.
	for i := 1; i < len(rows); i++ {
		var coef CER
		date, _ := time.Parse(DateFormat, rows[i][0])
		coef.Date = Fecha(date)
		coef.CER, _ = strconv.ParseFloat(rows[i][1], 64)
		Coef = append(Coef, coef)
	}
	fmt.Println("Proceso de llenado de index CER exitoso.")
	return nil

}
