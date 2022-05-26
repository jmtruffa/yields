package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

/* type Fecha time.Time

const DateFormat = "2006-01-02" */

/* func (d Fecha) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(d).Format(DateFormat) + `"`), nil
} */

/*func (d *Fecha) UnmarshalJSON(p []byte) error {
	var s string
	if err := json.Unmarshal(p, &s); err != nil {
		return err
	}
	t, err := time.Parse(DateFormat, s)
	if err != nil {
		return err
	}
	*d = Fecha(t)
	return nil
}*/

/* func (d Fecha) String() string {
	x, _ := d.MarshalJSON()
	return string(x)
} */

// struct to hold the index (CER) to adjust the face value of indexed bonds.
type CER struct {
	Date Fecha
	//Country string
	CER float64
}

/* func main() {
Coef, err := getCER()
if err != nil {
	fmt.Println("Error getting CER: ", err)
	return
}
fmt.Println("Total Records in file: ", len(Coef))
/*for i := len(Coef) - 10; i < len(Coef); i++ {
	fmt.Println(Coef[i].Date.String(), Coef[i].Country, Coef[i].CER)
}*/
/*for id, line := range Coef {
		fmt.Println(line.Date.String(), line.Country, line.CER)
		if id > 10 {
			break
		}
	}

	date1, _ := time.Parse(DateFormat, "2018-01-01")
	date2, _ := time.Parse(DateFormat, "2022-04-19")

	date1Parsed := Fecha(date1)
	date2Parsed := Fecha(date2)

	fmt.Println("Date1: ", date1Parsed)
	fmt.Println("Date2: ", date2Parsed)

	coef1, err := getCoefficient(date1Parsed, Coef)
	if err != nil {
		fmt.Println("Error getting CER with coefficient 1: ", err)
	}
	coef2, err := getCoefficient(date2Parsed, Coef)
	if err != nil {
		fmt.Println("Error getting CER with coefficient 2: ", err)
	}

	if coef1 == 0 || coef2 == 0 {
		fmt.Println("Cant obtain quotient since one (or both) coefs is (are) zero: ", err)
	}

	fmt.Println("Coef date1: ", coef1)
	fmt.Println("Coef date2: ", coef2)
	fmt.Println("Quotient: ", coef2/coef1)

} */

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
		diff := time.Now().Sub(modTime)

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
		//Coef.Country = rows[i][1]
		coef.CER, _ = strconv.ParseFloat(rows[i][2], 64)
		/*if err != nil {
			fmt.Println("Falla en el ParseFloat. ", err, "registro: ", i)
		}*/
		coefs = append(coefs, coef)
	}

	if saveFile == true {
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
