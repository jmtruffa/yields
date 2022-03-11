package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// define data structure to hold the json data
type Flujo struct {
	Date     time.Time
	Rate     float64
	Amort    float64
	Residual float64
	Amount   float64
}

type Bond struct {
	ID        string
	Ticker    string
	IssueDate time.Time
	Maturity  time.Time
	Coupon    float64
	Cashflow  []Flujo
}

const dateFormat = "2006-01-02"

var Bonds []Bond

func (c *Flujo) UnmarshalJSON(p []byte) error {
	var aux struct {
		Date     string  `json:"date"`
		Rate     float64 `json:"rate"`
		Amort    float64 `json:"amortization"`
		Residual float64 `json:"residual"`
		Amount   float64 `json:"amount"`
	}
	if err := json.Unmarshal(p, &aux); err != nil {
		return err
	}
	t, err := time.Parse(dateFormat, aux.Date)
	if err != nil {
		return err
	}
	(*c).Date = t
	fmt.Println("Date: ", c.Date)
	c.Rate = aux.Rate
	fmt.Println("Rate: ", c.Rate)
	c.Amort = aux.Amort
	fmt.Println("Amort: ", c.Amort)
	c.Residual = aux.Residual
	fmt.Println("Residual: ", c.Residual)
	c.Amount = aux.Amount
	fmt.Println("Amount: ", c.Amount)
	return nil
}

func (u *Bond) UnmarshalJSON(p []byte) error {
	var aux struct {
		ID        string  `json:"id"`
		Ticker    string  `json:"ticker"`
		IssueDate string  `json:"issueDate"`
		Maturity  string  `json:"maturity"`
		Coupon    float64 `json:"coupon"`
		Cashflow  []Flujo `json:"cashflow"`
	}

	err := json.Unmarshal(p, &aux)
	if err != nil {
		return err
	}

	t, err := time.Parse(dateFormat, aux.IssueDate)
	if err != nil {
		return err
	}
	y, err := time.Parse(dateFormat, aux.Maturity)
	if err != nil {
		return err
	}
	u.ID = aux.ID
	u.Ticker = aux.Ticker
	(*u).IssueDate = t
	(*u).Maturity = y
	u.Coupon = aux.Coupon
	return nil
}

func main() {
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
	fmt.Println("Bonds: ", Bonds)
	fmt.Println("Cashflow: ", Bonds[0].Cashflow)
	fmt.Println("Cashflow: ", Bonds[1].Cashflow)
	// with the json loaded
	router := gin.Default()
	router.GET("/yield", yieldWrapper)
	router.GET("/price", priceWrapper)

	router.Run("localhost:8080")

}

func yieldWrapper(c *gin.Context) {
	ticker, _ := c.GetQuery("ticker")
	settle, _ := c.GetQuery("settlementDate")
	settlementDate, error := time.Parse("2006-01-02", settle)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date format"})
		//c.JSON(http.StatusBadRequest, gin.H{"error": error.Error()})
		return
	}
	priceTemp, _ := c.GetQuery("price")
	price, error := strconv.ParseFloat(priceTemp, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": error.Error()})
		return
	}
	cashFlow, error := getCashFlow(ticker)
	if error != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "ticker not found"})
		return
	}

	//cashFlow := Bonds[0].Cashflow used when getCashFlow was not written yet
	r, error := Yield(cashFlow, price, settlementDate)
	if error != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Yield calculation"})
		return
	}
	c.IndentedJSON(http.StatusOK, r)
}

func getCashFlow(ticker string) ([]Flujo, error) {
	fmt.Println("Ticker solicitado: ", ticker)
	fmt.Println("Bonds available: ", Bonds)
	fmt.Println("Flujo del bono: ", Bonds[0].Cashflow)
	fmt.Println("Flujo del bono: ", Bonds[1].Cashflow)
	for _, bond := range Bonds {
		if bond.Ticker == ticker {
			return bond.Cashflow, nil
		}
	}
	return nil, errors.New("Ticker Not Found")
}

func priceWrapper(c *gin.Context) {
	ticker, _ := c.GetQuery("ticker")
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
	cashFlow, error := getCashFlow(ticker)
	if error != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "ticker not found"})
		return
	}

	p, error := Price(cashFlow, rate, settlementDate)
	if error != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Price calculation"})
		return
	}
	c.IndentedJSON(http.StatusOK, p)
}

func Yield(flow []Flujo, price float64, settlementDate time.Time) (float64, error) {
	// settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
	// Discard all cashflows before the settlementDate

	for i, cf := range flow {
		if cf.Date.After(settlementDate) {
			flow = flow[i:]
			break
		}
	}
	values := make([]float64, len(flow)+1)
	dates := make([]time.Time, len(flow)+1)

	// Add the first cashflow which is the price argument
	values[0] = -price
	dates[0] = settlementDate

	// need to generate the arrays to pass as arguments to the function
	for i := 1; i <= len(flow); i++ {
		values[i] = flow[i-1].Amount
		dates[i] = flow[i-1].Date
	}
	rate, error := ScheduledInternalRateOfReturn(values, dates, 0.001)
	if error != nil {
		return 0, error
	}

	return rate, nil
}

func Price(flow []Flujo, rate float64, settlementDate time.Time) (float64, error) {
	// settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
	// Discard all cashflows before the settlementDate

	for i, cf := range flow {
		if cf.Date.After(settlementDate) {
			flow = flow[i:]
			break
		}
	}
	values := make([]float64, len(flow)+1)
	dates := make([]time.Time, len(flow)+1)

	values[0] = 0
	dates[0] = settlementDate

	// need to generate the arrays to pass as arguments to the function
	for i := 1; i <= len(flow); i++ {
		values[i] = flow[i-1].Amount
		dates[i] = flow[i-1].Date
	}

	price, error := ScheduledNetPresentValue(rate, values, dates)
	if error != nil {
		return 0, error
	}

	return price, nil
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
