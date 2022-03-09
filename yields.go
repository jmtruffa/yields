package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
	"math"
	"time"
	"errors"
)

// define data structure to hold the json data
type Flujo struct {
	Date time.Time
	Rate float64
	Amort float64
	Residual float64
	Amount float64
}

type Bond struct {
	ID string
	Ticker string
	IssueDate time.Time
	Maturity time.Time
	Coupon float64
	Cashflow []Flujo
}



func main() {
    
    // load json with all the bond's data and handle any errors
    data, err := ioutil.ReadFile("./bonds.json")
    if err != nil {
      fmt.Print(err)
    }
		
	  // json data
	  var bonds []Bond

	 // unmarshall the loaded JSON
	 err = json.Unmarshal([]byte(data), &bonds)
	 if err != nil {
		 fmt.Println("error:", err)
	 }


	for i := 1; i <2500; i++ {
		i:=4 //settlement offset

		fmt.Println("Ejercicio GD30: ", "\n")
		fmt.Println("Precio: ", 32.54)
		fmt.Println("Settlement: ", time.Now().AddDate(0,0,i).Format("2006-01-02"), "\n")
		yield, err := Yield(bonds[0].Cashflow, 32.54, time.Now().AddDate(0,0,i))
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Yield: %.4f%%\n", yield * 100)

		fmt.Println("==================================\n")
		fmt.Println("Ejercicio GD30: ", "\n")
		fmt.Printf("Yield: %.4f%%\n", yield * 100)
		fmt.Println("Settlement: ", time.Now().AddDate(0,0,i).Format("2006-01-02"), "\n")
		price, err := Price(bonds[0].Cashflow, 0.2633, time.Now().AddDate(0,0,i))
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Price: %.2f\n", price)
		
	}
}

func Yield (flow []Flujo, price float64, settlementDate time.Time) (float64, error) {
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
//ScheduledNetPresentValue(rate float64, values []float64, dates []time.Time)
func Price (flow []Flujo, rate float64, settlementDate time.Time) (float64, error) {
	// TODO: settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
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

	return price,nil
}

// ScheduledNetPresentValue returns the Net Present Value of a scheduled cash flow series given a discount rate
//
// Excel equivalent: XNPV
func ScheduledNetPresentValue(rate float64, values []float64, dates []time.Time) (float64, error) {
	// this function calculates de price on the date of the first element.
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
//
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
	Precision = 1E-6
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
