package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickar/cal/v2"
	"github.com/rickar/cal/v2/ar"
)

// to embed the time in a custom format to parse the dates that come from the json
type Fecha time.Time

const DateFormat = "2006-01-02"

var EasternsDay2 = &cal.Holiday{
	Name:   "Viernes Santo",
	Type:   cal.ObservancePublic,
	Offset: -2,
	Func:   cal.CalcEasterOffset,
}

var ChristmasDayEve = &cal.Holiday{
	Name:      "24 de diciembre",
	Type:      cal.ObservancePublic,
	StartYear: 2021,
	EndYear:   2022,
	Month:     time.December,
	Day:       24,
	Func:      cal.CalcDayOfMonth,
}

var LastDayYearEve = &cal.Holiday{
	Name:      "30 de diciembre",
	Type:      cal.ObservancePublic,
	StartYear: 2021,
	EndYear:   2022,
	Month:     time.December,
	Day:       30,
	Func:      cal.CalcDayOfMonth,
}

var BelgranoDay = &cal.Holiday{
	Name:      "Aniversario paso a la inmortalidad del General Juan Manuel Belgrano",
	Type:      cal.ObservancePublic,
	StartYear: 2022,
	EndYear:   2022,
	Month:     time.June,
	Day:       20,
	Func:      cal.CalcDayOfMonth,
}

var calendar = cal.NewBusinessCalendar()

var Bonds []Bond
var Coef []CER

// define data structure to hold the json data
type Flujo struct {
	Date     Fecha
	Rate     float64
	Amort    float64
	Residual float64
	Amount   float64
}

type Bond struct {
	ID        string
	Ticker    string
	IssueDate Fecha
	Maturity  Fecha
	Coupon    float64
	Cashflow  []Flujo
	Index     string
	Offset    int // Indexed bonds uses offset as date lookback period for the Index. In CER adjusted bonds this is set to 10 working days.
}

// embed methods in the custom struct to be able to use them
func (d Fecha) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(d).Format(DateFormat) + `"`), nil
}

func (d *Fecha) UnmarshalJSON(p []byte) error {
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
}

func (d Fecha) String() string {
	x, _ := d.MarshalJSON()
	return string(x)
}

func (d Fecha) After(t time.Time) bool {
	return time.Time(d).After(t)
}

func (d Fecha) Format(s string) string {
	return time.Time(d).Format(s)
}

func main() {
	// SetUpCalendar creates the calendar and set ups the holidays for Argentina.
	SetUpCalendar()
	// load json with all the bond's data and handle any errors
	data, err := ioutil.ReadFile("./bonds2.json")
	if err != nil {
		fmt.Print(err)
	}
	// json data
	// unmarshall the loaded JSON
	err = json.Unmarshal([]byte(data), &Bonds)
	if err != nil {
		fmt.Println("error:", err)
	}
	// Load the CER data into Coef
	Coef, err = getCER()
	if err != nil {
		fmt.Println("Error getting CER: ", err)
		return
	}
	fmt.Println("Total Records in file: ", len(Coef))

	// start of the router and endpoints
	router := gin.Default()
	router.GET("/yield", yieldWrapper)
	router.GET("/apr", aprWrapper)
	router.GET("/price", priceWrapper)
	router.GET("/schedule", scheduleWrapper)
	router.POST("/upload", uploadWrapper)
	router.GET("/bonds", getBondsWrapper)
	// run the router
	router.Run("localhost:8080")
}

func aprWrapper(c *gin.Context) {
	//Params: ticker, settlementDate, price, InitialFee, endingFee
	ticker := strings.ToUpper(c.Query("ticker"))

	settle, _ := c.GetQuery("settlementDate")
	settlementDate, error := time.Parse("2006-01-02", settle)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Settlement Date. ": "Invalid date format"})
		return
	}
	priceTemp, _ := c.GetQuery("price")
	price, error := strconv.ParseFloat(priceTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Price. ": error.Error()})
		return
	}
	initialFeeTemp, _ := c.GetQuery("initialFee")
	initialFee, error := strconv.ParseFloat(initialFeeTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Initial Fee. ": error.Error()})
		return
	}
	endingFeeTemp, _ := c.GetQuery("endingFee")
	endingFee, error := strconv.ParseFloat(endingFeeTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Ending Fee. ": error.Error()})
		return
	}

	// Get the cashflow only if the ticker is a valid zero coupon bond

	cashFlow, index, error := getCashFlow(ticker)
	if error != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"Error: ": "Ticker not found"})
		return
	} else if Bonds[index].Coupon != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Coupon. ": "The coupon of this bond is not zero. Try with endopoint /yield"})
		return
	}

	// adjust price, if the bond is indexed, by using the ratio calculated by dividing the index of settlementDate by the index of IssueDate.
	// There's an offset variable to adjust the lookback period for the index.

	ratio := 1.0
	if Bonds[index].Index != "" { // assuming for now that only one type of index is used: CER

		offset := Bonds[index].Offset
		//offset := -10

		fmt.Println("Fechas a buscar: ", Fecha(calendar.WorkdaysFrom(time.Time(Fecha(settlementDate)), offset)), "\n", Fecha(calendar.WorkdaysFrom(time.Time(Bonds[index].IssueDate), offset)))

		coef1, err := getCoefficient(Fecha(calendar.WorkdaysFrom(time.Time(Fecha(settlementDate)), offset)), Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		coef2, err := getCoefficient(Fecha(calendar.WorkdaysFrom(time.Time(Bonds[index].IssueDate), offset)), Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}

		ratio = coef1 / coef2

	}

	days := time.Time(cashFlow[0].Date).Sub(settlementDate).Hours() / 24
	fmt.Println("Días: ", days)
	r := ((100*(1-endingFee))/((price*(1+initialFee))/ratio) - 1) * (365 / days)
	mduration := (days / 365) / (1 + r)

	c.JSON(http.StatusOK, gin.H{
		"Yield":     r,
		"MDuration": mduration,
	})

}

func getBondsWrapper(c *gin.Context) {
	var bondsOut []string
	for _, bond := range Bonds {
		bondsOut = append(bondsOut, bond.Ticker)
	}
	c.JSON(http.StatusOK, gin.H{
		"bonds": bondsOut,
	})

}

func uploadWrapper(c *gin.Context) {
	var upload Bond
	jsonData, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	err = json.Unmarshal([]byte(jsonData), &upload)
	if err != nil {
		fmt.Println("error:", err)
	}
	upload.ID = strings.ToUpper(strconv.Itoa(len(Bonds) + 1))
	upload.Ticker = strings.ToUpper(upload.Ticker)
	Bonds = append(Bonds, upload)

	c.JSON(http.StatusOK, gin.H{
		"Result":      "Bond uploaded",
		"Assigned ID": upload.ID,
	})

	jsonOut, err := json.Marshal(Bonds)
	if err != nil {
		fmt.Println("Error when marshalling:", err)
	}
	// backup the file containing the data first
	dest := "./bonds_" + time.Now().Format("2006-01-02") + ".json"
	orig := "./bonds.json"
	cpFile, err := ioutil.ReadFile(orig)
	if err != nil {
		fmt.Print(err)
	}
	err = ioutil.WriteFile(dest, cpFile, 0644)
	if err != nil {
		fmt.Println("Error when copying:", err)
	}
	err = ioutil.WriteFile("./bonds.json", jsonOut, 0644)
	if err != nil {
		fmt.Println("Error when writing:", err)
	}
}

func scheduleWrapper(c *gin.Context) {
	ticker := strings.ToUpper(c.Query("ticker"))
	settlementDate := c.Query("settlementDate")
	if ticker == "" || settlementDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ticker and settlementDate are required",
		})
		return
	}
	t, err := time.Parse(DateFormat, settlementDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid settlementDate",
		})
		return
	}
	cashFlow, _, err := getCashFlow(ticker)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "ticker not found",
		})
		return
	}
	scheduleOut := getScheduleOfPayments(&cashFlow, &t)
	c.JSON(http.StatusOK, gin.H{
		"schedule": scheduleOut,
	})

}

func getScheduleOfPayments(cashFlow *[]Flujo, settlementDate *time.Time) []Flujo {
	var schedule []Flujo
	for _, cash := range *cashFlow {
		if cash.Date.After(settlementDate.Add(-24 * time.Hour)) {
			schedule = append(schedule, Flujo{
				Date:     cash.Date,
				Rate:     cash.Rate,
				Amort:    cash.Amort,
				Residual: cash.Residual,
				Amount:   cash.Amount,
			})
		}

	}
	return schedule
}

func getCashFlow(ticker string) ([]Flujo, int, error) {
	for i, bond := range Bonds {
		if bond.Ticker == ticker {
			return bond.Cashflow, i, nil
		}
	}
	return nil, -1, errors.New("Ticker Not Found")
}

func yieldWrapper(c *gin.Context) {
	/* Params: ticker, settlementDate, price, initialFee, endingFee */

	ticker := strings.ToUpper(c.Query("ticker"))
	settle, _ := c.GetQuery("settlementDate")
	settlementDate, error := time.Parse("2006-01-02", settle)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Settlement Date. ": "Invalid date format"})
		//c.JSON(http.StatusBadRequest, gin.H{"error": error.Error()})
		return
	}
	priceTemp, _ := c.GetQuery("price")
	price, error := strconv.ParseFloat(priceTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Price. ": error.Error()})
		return
	}
	initialFeeTemp, _ := c.GetQuery("initialFee")
	initialFee, error := strconv.ParseFloat(initialFeeTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Initial Fee. ": error.Error()})
		return
	}
	endingFeeTemp, _ := c.GetQuery("endingFee")
	endingFee, error := strconv.ParseFloat(endingFeeTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Ending Fee. ": error.Error()})
		return
	}

	cashFlow, index, error := getCashFlow(ticker)
	if error != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"Error: ": "Ticker not found"})
		return
	}

	// adjust price, if the bond is indexed, by using the ratio calculated by dividing the index of settlementDate by the index of IssueDate.
	// There's an offset variable to adjust the lookback period for the index.

	ratio := 1.0
	if Bonds[index].Index != "" { // assuming for now that only one type of index is used: CER

		offset := Bonds[index].Offset
		//offset := -10

		fmt.Println("Fechas a buscar: ", Fecha(calendar.WorkdaysFrom(time.Time(Fecha(settlementDate)), offset)), "\n", Fecha(calendar.WorkdaysFrom(time.Time(Bonds[index].IssueDate), offset)))

		coef1, err := getCoefficient(Fecha(calendar.WorkdaysFrom(time.Time(Fecha(settlementDate)), offset)), Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		coef2, err := getCoefficient(Fecha(calendar.WorkdaysFrom(time.Time(Bonds[index].IssueDate), offset)), Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		//fmt.Println("coef1: ", coef1, "coef2: ", coef2)

		ratio = coef1 / coef2

	}

	price = price / ratio

	r, error := Yield(cashFlow, price, settlementDate, initialFee, endingFee)
	if error != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Yield calculation."})
		return
	}

	mduration, error := Mduration(cashFlow, r, settlementDate, initialFee, endingFee, price)
	if error != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Mduration calculation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"Yield":     r,
		"MDuration": mduration,
	})

	//c.IndentedJSON(http.StatusOK, r)
}

func priceWrapper(c *gin.Context) {
	ticker := strings.ToUpper(c.Query("ticker"))
	settle, _ := c.GetQuery("settlementDate")
	settlementDate, error := time.Parse("2006-01-02", settle)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		//c.JSON(http.StatusBadRequest, gin.H{"error": error.Error()})
		return
	}
	rateTemp, _ := c.GetQuery("rate")
	rate, error := strconv.ParseFloat(rateTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": error.Error()})
		return
	}

	initialFeeTemp, _ := c.GetQuery("initialFee")
	initialFee, error := strconv.ParseFloat(initialFeeTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Initial Fee. ": error.Error()})
		return
	}
	endingFeeTemp, _ := c.GetQuery("endingFee")
	endingFee, error := strconv.ParseFloat(endingFeeTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Ending Fee. ": error.Error()})
		return
	}

	cashFlow, index, error := getCashFlow(ticker)
	if error != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "ticker not found"})
		return
	}
	// debería poner la obtención de los coefs
	ratio := 1.0
	if Bonds[index].Index != "" { // assuming for now that only one type of index is used: CER

		offset := Bonds[index].Offset
		//offset := -10

		fmt.Println("Fechas a buscar: ", Fecha(calendar.WorkdaysFrom(time.Time(Fecha(settlementDate)), offset)), "\n", Fecha(calendar.WorkdaysFrom(time.Time(Bonds[index].IssueDate), offset)))

		coef1, err := getCoefficient(Fecha(calendar.WorkdaysFrom(time.Time(Fecha(settlementDate)), offset)), Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		coef2, err := getCoefficient(Fecha(calendar.WorkdaysFrom(time.Time(Bonds[index].IssueDate), offset)), Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		//fmt.Println("coef1: ", coef1, "coef2: ", coef2)

		ratio = coef1 / coef2

	}
	p, error := Price(cashFlow, rate, settlementDate, initialFee, endingFee)
	if error != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Price calculation"})
		return
	}

	mduration, error := Mduration(cashFlow, rate, settlementDate, initialFee, endingFee, p)
	if error != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Mduration calculation"})
		return
	}

	p = p * ratio

	c.JSON(http.StatusOK, gin.H{
		"Price":     p,
		"MDuration": mduration,
	})

	//c.IndentedJSON(http.StatusOK, p)
}

func Yield(flow []Flujo, price float64, settlementDate time.Time, initialFee float64, endingFee float64) (float64, error) {
	// settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
	// Discard all cashflows before the settlementDate

	values, dates := GenerateArrays(flow, settlementDate, initialFee, endingFee, price)

	rate, error := ScheduledInternalRateOfReturn(values, dates, 0.001)
	if error != nil {
		return 0, error
	}

	return rate, nil
}

func Mduration(flow []Flujo, rate float64, settlementDate time.Time, initialFee float64, endingFee float64, price float64) (float64, error) {
	values, dates := GenerateArrays(flow, settlementDate, initialFee, endingFee, 0)

	if len(values) != len(dates) {
		return 0, errors.New("values and dates must have the same length")
	}

	xnpv := 0.0
	dur := 0.0
	nper := len(values)
	for i := 1; i <= nper; i++ {
		exp := dates[i-1].Sub(dates[0]).Hours() / 24.0 / 365.0
		xnpv = values[i-1] / math.Pow(1+rate, exp)
		dur += xnpv * exp / -price
	}
	return (-1 * (dur / (1 + rate))), nil
}

// Pass the casflow and get the slices separated to use with calculating funcions.
// To get the cashflow to use with price, pass 0 as price
// To get the casfhflow to use with yield, pass the price obtained from the endpoint
func GenerateArrays(flow []Flujo, settlementDate time.Time, initialFee float64, endingFee float64, price float64) ([]float64, []time.Time) {
	for i, cf := range flow {
		if cf.Date.After(settlementDate.Add(-24 * time.Hour)) {
			flow = flow[i:]
			break
		}
	}
	values := make([]float64, len(flow)+1)
	dates := make([]time.Time, len(flow)+1)

	values[0] = -price * (1 + initialFee)
	dates[0] = settlementDate

	for i := 1; i <= len(flow); i++ {
		values[i] = flow[i-1].Amount
		dates[i] = time.Time(flow[i-1].Date)
	}
	values[len(flow)] = values[len(flow)] * (1 - endingFee)

	return values, dates

}

func Price(flow []Flujo, rate float64, settlementDate time.Time, initialFee float64, endingFee float64) (float64, error) {
	// settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
	// Discard all cashflows before the settlementDate
	values, dates := GenerateArrays(flow, settlementDate, initialFee, endingFee, 0)

	price, error := ScheduledNetPresentValue(rate, values, dates)
	if error != nil {
		return 0, error
	}

	return price * (1 + initialFee), nil
}

// ScheduledNetPresentValue returns the Net Present Value of a scheduled cash flow series given a discount rate
// Excel equivalent: XNPV
func ScheduledNetPresentValue(rate float64, values []float64, dates []time.Time) (float64, error) {
	// this function calculates the price on the date of the first element.
	// by providing a settlementDate, we can calculate the price on any date.
	// we just need to add a first element consisting of the settlementDate and 0 Amount prior to passing the values and dates arrays to the function

	if len(values) != len(dates) {
		return 0, errors.New("values and dates must have the same length")
	}

	xnpv := 0.0
	nper := len(values)
	for i := 1; i <= nper; i++ {
		exp := dates[i-1].Sub(dates[0]).Hours() / 24.0 / 365.0
		xnpv += values[i-1] / math.Pow(1+rate, exp)
	}
	return xnpv, nil
}

// ScheduledInternalRateOfReturn returns the internal rate of return of a scheduled cash flow series.
// Guess is a guess for the rate, used as a starting point for the iterative algorithm.
// Excel equivalent: XIRR
func ScheduledInternalRateOfReturn(values []float64, dates []time.Time, guess float64) (float64, error) {
	min, max := minMaxSlice(values)
	if min*max >= 0 {
		return 0, errors.New("the cash flow must contain at least one positive value and one negative value")
	}
	if len(values) != len(dates) {
		return 0, errors.New("values and dates must have the same length")
	}

	function := func(rate float64) float64 {
		r, _ := ScheduledNetPresentValue(rate, values, dates)
		return r
	}
	derivative := func(rate float64) float64 {
		r, _ := dScheduledNetPresentValue(rate, values, dates)
		return r
	}
	return newton(guess, function, derivative, 0)
}

func dScheduledNetPresentValue(rate float64, values []float64, dates []time.Time) (float64, error) {
	if len(values) != len(dates) {
		return 0, errors.New("values and dates must have the same length")
	}

	dxnpv := 0.0
	nper := len(values)
	for i := 1; i <= nper; i++ {
		exp := dates[i-1].Sub(dates[0]).Hours() / 24.0 / 365.0
		dxnpv -= values[i-1] * exp / math.Pow(1+rate, exp+1)
	}
	return dxnpv, nil
}

func minMaxSlice(values []float64) (float64, float64) {
	min := math.MaxFloat64
	max := -min
	for _, value := range values {
		if value > max {
			max = value
		}
		if value < min {
			min = value
		}
	}
	return min, max
}

const (
	// MaxIterations determines the maximum number of iterations performed by the Newton-Raphson algorithm.
	MaxIterations = 30
	// Precision determines how close to the solution the Newton-Raphson algorithm should arrive before stopping.
	Precision = 1e-6
)

func newton(guess float64, function func(float64) float64, derivative func(float64) float64, numIt int) (float64, error) {
	x := guess - function(guess)/derivative(guess)
	if math.Abs(x-guess) < Precision {
		return x, nil
	} else if numIt >= MaxIterations {
		return 0, errors.New("solution didn't converge")
	} else {
		return newton(x, function, derivative, numIt+1)
	}
}

/*
func getCER() ([]CER, error) {
	var saveFile bool
	var downloadFile bool
	var reader *csv.Reader
	file := "/Users/juan/Google Drive/Mi unidad/analisis financieros/functions/data/CER.csv"
	fileInfo, err := os.Stat(file)

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

	var Coefs []CER

	for i := 1; i < len(rows); i++ {
		var Coef CER
		date, _ := time.Parse(DateFormat, rows[i][0])
		Coef.Date = Fecha(date)
		//Coef.Country = rows[i][1]
		Coef.CER, _ = strconv.ParseFloat(rows[i][2], 64)

		Coefs = append(Coefs, Coef)
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

	return Coefs, nil
}

/* func getCoefficient(date Fecha, coef []CER) (float64, error) {
	for i := len(coef) - 1; i >= 0; i-- {
		if coef[i].Date == date {
			return coef[i].CER, nil
		}
	}
	return 0, fmt.Errorf("CER not found for date %v", date)
} */

func SetUpCalendar() {
	calendar.AddHoliday(
		ar.NewYear,
		ar.IndependenceDay,
		ar.LaborDay,
		ar.ChristmasDay,
		ar.CarnivalDay1,
		ar.CarnivalDay2,
		ar.TruethDay,
		ar.MalvinasVeterans,
		ar.EasternsDay,
		EasternsDay2,
		ar.RevolutionDay,
		ar.GuemesDay,
		ar.SanMartinDay,
		ar.DiversityDay,
		ar.SovereigntyDay,
		ar.VirgenDay,
		ar.CensoNacional2022,
		ChristmasDayEve,
		LastDayYearEve,
		BelgranoDay,
		ar.BelgranoDay,
	)
}
