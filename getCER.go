package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// struct to hold the index (CER) to adjust the face value of indexed bonds.
type CER struct {
	Date Fecha
	//Country string
	CER float64
}

func getCoefficient(date Fecha, coef []CER) (float64, error) {
	for i := len(coef) - 1; i >= 0; i-- {
		if coef[i].Date == date {
			return coef[i].CER, nil
		}
	}
	return 0, fmt.Errorf("CER not found for date %v", date)
}

func getCER() ([]CER, error) {
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

		if diff < 24*time.Hour {
			// grab the file from disk
			fmt.Println("The file is newer than 24 hours old. Grabbing from disk...")
			res, error := os.Open(file)
			if error != nil {
				fmt.Println("Error opening CSV file: ", error)
				return nil, error
			}
			defer res.Close()
			reader = csv.NewReader(res)
			saveFile = false
			downloadFile = false

		} else {
			// download the file again
			fmt.Println("The file is older than 24 hours. Downloading...")
			downloadFile = true
			saveFile = true
		}
	}
	if downloadFile {
		// download the file again

		apiKey := os.Getenv("ALPHACAST_API_KEY")
		url := "https://api.alphacast.io/datasets/8277/data?apiKey=" + apiKey + "&%24select=3290015&$format=csv"
		dataset := http.Client{
			Timeout: time.Second * 10,
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		res, getErr := dataset.Do(req)
		if getErr != nil {
			return nil, getErr
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

	var coefs []CER

	for i := 1; i < len(rows); i++ {
		var coef CER
		date, _ := time.Parse(DateFormat, rows[i][0])
		coef.Date = Fecha(date)
		coef.CER, _ = strconv.ParseFloat(rows[i][2], 64)
		coefs = append(coefs, coef)
	}

	if saveFile {
		f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			fmt.Println("Error creating file: ", err)
			return nil, err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		w.WriteAll(rows)
		w.Flush()
	}

	return coefs, nil
}
