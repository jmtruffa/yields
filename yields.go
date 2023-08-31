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

	"bytes"
	"encoding/csv"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jasonlvhit/gocron"
)

// to embed the time in a custom format to parse the dates that come from the json
type Fecha time.Time

const DateFormat = "2006-01-02"

var Bonds []Bond

// struct to use with extendedInfo func
type extInfo struct {
	accDays    int
	currCoupon float64
	residual   float64
	accInt     float64
	techValue  float64
	parity     float64
	lastCoupon Fecha
	lastAmort  float64
}

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

func (d Fecha) Sub(t Fecha) time.Duration {
	return time.Time(d).Sub(time.Time(t))
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

func executeCronJob() {
	gocron.Every(24).Hours().Do(getCER)
	<-gocron.Start()
}

func main() {
	// SetUpCalendar creates the calendar and set ups the holidays for Argentina.
	SetUpCalendar()
	go executeCronJob() // this will make the cron run in the background.

	// load json with all the bond's data and handle any errors
	getBondsData()

	// Load the CER data into Coef
	getCER()

	// start of the router and endpoints
	router := gin.Default()
	// CORS for https://foo.com and https://github.com origins, allowing:
	// - PUT and PATCH methods
	// - Origin header
	// - Credentials share
	// - Preflight requests cached for 12 hours
	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"}, // or use https://foo.com, https://github.com, etc
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Origin"},
		//ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	//router.Run()
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
	//Params: ticker, settlementDate, price, InitialFee, endingFee, extendIndex
	// extendIndex: rate at which extend Index (CER). In yearly basis.
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

	extIndex, _ := c.GetQuery("extendIndex")
	if extIndex == "" { // if extendIndex is empty, set it to 0
		extIndex = "0"
	}

	extendIndex, error := strconv.ParseFloat(extIndex, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Extended Index. Value maybe misisng or not numeric	": error.Error()})
		return
	}

	// should check number should be >= 0
	if extendIndex < 0 {
		// s// var err error
		c.JSON(http.StatusBadRequest, gin.H{"Extended Index should be greater or equal to 0": "Error"})
		return
	}

	// Get the cashflow only if the ticker is a valid zero coupon bond

	cashFlow, index, error := getCashFlow(ticker)
	if error != nil {
		c.JSON(http.StatusNotFound, gin.H{"Error: ": "Ticker not found"})
		return
	} else if Bonds[index].Coupon != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Coupon. ": "The coupon of this bond is not zero. Try with endopoint /yield"})
		return
	}

	// adjust price, if the bond is indexed, by using the ratio calculated by dividing the index of settlementDate by the index of IssueDate.
	// There's an offset variable to adjust the lookback period for the index.

	ratio := 1.0
	var coef1 float64
	var coef2 float64
	var coefFecha time.Time

	if Bonds[index].Index != "" { // assuming for now that only one type of index is used: CER

		offset := Bonds[index].Offset

		type error interface {
			Error() string
		}
		var err error

		coefFecha = calendar.WorkdaysFrom(settlementDate, offset)
		coef1, err = getCoefficient(coefFecha, extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		issueDate, _ := time.Parse(DateFormat, (Bonds[index].IssueDate.Format(DateFormat)))
		coef2, err = getCoefficient(calendar.WorkdaysFrom(issueDate, offset), extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}

		ratio = coef1 / coef2

	}

	days := time.Time(cashFlow[0].Date).Sub(settlementDate).Hours() / 24
	r := ((100*(1-endingFee))/((price*(1+initialFee))/ratio) - 1) * (365 / days)
	mduration := (days / 365) / (1 + r)
	accDays := time.Time(settlementDate).Sub(time.Time(Bonds[index].IssueDate)).Hours() / 24
	coupon := Bonds[index].Coupon //I could have used 0 but this is more informative
	residual := cashFlow[0].Residual + cashFlow[0].Amort
	accInt := (accDays / 360 * coupon) * 100
	techValue := ratio * residual
	parity := price / techValue * 100

	c.JSON(http.StatusOK, gin.H{
		"Yield":                 r,
		"MDuration":             mduration,
		"AccrualDays":           accDays,
		"CurrentCoupon: ":       coupon,
		"Residual":              residual,
		"AccruedInterest":       accInt,
		"TechnicalValue":        techValue,
		"Parity":                parity,
		"LastCoupon":            "N/A",
		"Coef Used":             coef1,
		"Coef Issue":            coef2,
		"Coef Fecha de Cálculo": Fecha(coefFecha),
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

	csvString := convertToCSV(scheduleOut)
	c.Writer.Header().Set("Content-Type", "text/csv")
	c.Writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment;filename=%s.csv", ticker))
	c.Writer.WriteString(string(csvString))

	c.JSON(http.StatusOK, gin.H{
		"schedule": scheduleOut,
	})
	c.Writer.Write(csvString)
}

func convertToCSV(schedule []Flujo) []byte {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	defer writer.Flush()

	for _, cash := range schedule {
		record := []string{
			cash.Date.String(),
			strconv.FormatFloat(cash.Rate, 'f', -1, 64),
			strconv.FormatFloat(cash.Amort, 'f', -1, 64),
			strconv.FormatFloat(cash.Residual, 'f', -1, 64),
			strconv.FormatFloat(cash.Amount, 'f', -1, 64),
		}
		writer.Write(record)
	}

	return buffer.Bytes()
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
	return nil, -1, errors.New("ticker not found")
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

	extIndex, _ := c.GetQuery("extendIndex")
	if extIndex == "" { // if extendIndex is empty, set it to 0
		extIndex = "0"
	}

	extendIndex, error := strconv.ParseFloat(extIndex, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Extended Index. ": error.Error()})
		return
	}
	// should check number should be >= 0
	if extendIndex < 0 {
		// s// var err error
		c.JSON(http.StatusBadRequest, gin.H{"Extended Index should be greater or equal to 0": "Error"})
		return
	}

	cashFlow, index, error := getCashFlow(ticker)
	if error != nil {
		c.JSON(http.StatusNotFound, gin.H{"Error: ": "Ticker not found"})
		return
	}

	// adjust price, if the bond is indexed, by using the ratio calculated by dividing the index of settlementDate by the index of IssueDate.
	// There's an offset variable to adjust the lookback period for the index.

	ratio := 1.0
	var coef1 float64
	var coef2 float64
	var coefFecha time.Time
	if Bonds[index].Index != "" { // assuming for now that only one type of index is used: CER

		offset := Bonds[index].Offset

		type error interface {
			Error() string
		}
		var err error

		coefFecha = calendar.WorkdaysFrom(settlementDate, offset)
		coef1, err = getCoefficient(coefFecha, extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}
		issueDate, _ := time.Parse(DateFormat, (Bonds[index].IssueDate.Format(DateFormat)))
		coef2, err = getCoefficient(calendar.WorkdaysFrom(issueDate, offset), extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}

		ratio = coef1 / coef2

	}

	price = price / ratio

	r, error, cfIndex := Yield(cashFlow, price, settlementDate, initialFee, endingFee)
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Yield calculation."})
		return
	}

	mduration, error := Mduration(cashFlow, r, settlementDate, initialFee, endingFee, price)
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Mduration calculation"})
		return
	}

	// Use index to calculate accDays, Parity
	origPrice := price * ratio // back to price to calculate parity correctly

	// need to adapt to the new way of calling extendedInfo with a struct.

	info := extendedInfo(&settlementDate, &cashFlow, &origPrice, cfIndex, ratio)
	//accDays, coupon, residual, accInt, techValue, parity, lastCoupon, _ := extendedInfo(&settlementDate, &cashFlow, &origPrice, cfIndex)

	c.JSON(http.StatusOK, gin.H{
		"Yield":                 r,
		"MDuration":             mduration,
		"AccrualDays":           info.accDays,
		"CurrentCoupon: ":       info.currCoupon,
		"Residual":              info.residual,
		"AccruedInterest":       info.accInt,
		"TechnicalValue":        info.techValue,
		"Parity":                info.parity,
		"LastCoupon":            info.lastCoupon,
		"LastAmort":             info.lastAmort,
		"Coef Used":             coef1,
		"Coef Issue":            coef2,
		"Coef Fecha de Cálculo": Fecha(coefFecha),
	})

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
	extIndex, _ := c.GetQuery("extendIndex")
	if extIndex == "" { // if extendIndex is empty, set it to 0
		extIndex = "0"
	}

	extendIndex, error := strconv.ParseFloat(extIndex, 64)
	if error != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error in Extended Index. Value maybe misisng or not numeric": error.Error()})
		return
	}
	// should check number should be >= 0
	if extendIndex < 0 {
		// s// var err error
		c.JSON(http.StatusBadRequest, gin.H{"Extended Index should be greater or equal to 0": "Error"})
		return
	}

	cashFlow, index, error := getCashFlow(ticker)
	if error != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "ticker not found"})
		return
	}

	ratio := 1.0
	var coef1 float64
	var coef2 float64
	var coefFecha time.Time
	if Bonds[index].Index != "" { // assuming for now that only one type of index is used: CER

		offset := Bonds[index].Offset

		type error interface {
			Error() string
		}
		var err error

		coefFecha = calendar.WorkdaysFrom(settlementDate, offset)
		coef1, err = getCoefficient(coefFecha, extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}

		issueDate, _ := time.Parse(DateFormat, (Bonds[index].IssueDate.Format(DateFormat)))
		coef2, err = getCoefficient(calendar.WorkdaysFrom(issueDate, offset), extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}

		ratio = coef1 / coef2

	}
	p, error, cfIndex := Price(cashFlow, rate, settlementDate, initialFee, endingFee)
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Price calculation"})
		return
	}

	mduration, error := Mduration(cashFlow, rate, settlementDate, initialFee, endingFee, p)
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "sth went wrong with the Mduration calculation"})
		return
	}

	p = p * ratio

	// Use index to calculate accDays, Parity

	origPrice := p / ratio
	info := extendedInfo(&settlementDate, &cashFlow, &origPrice, cfIndex, ratio)
	//accDays, coupon, residual, accInt, techValue, parity, lastCoupon, _ := extendedInfo(&settlementDate, &cashFlow, &p, cfIndex)

	c.JSON(http.StatusOK, gin.H{
		"Price":                 p,
		"MDuration":             mduration,
		"AccrualDays":           info.accDays,
		"CurrentCoupon: ":       info.currCoupon,
		"Residual":              info.residual,
		"AccruedInterest":       info.accInt,
		"TechnicalValue":        info.techValue,
		"Parity":                info.parity,
		"LastCoupon":            info.lastCoupon,
		"LastAmort":             info.lastAmort,
		"Coef Used":             coef1,
		"Coef Issue":            coef2,
		"Coef Fecha de Cálculo": Fecha(coefFecha),
	})

}

func extendedInfo(settlementDate *time.Time, cashflow *[]Flujo, p *float64, cfIndex int, ratio float64) extInfo {
	var info extInfo

	//teng que dejar cfIndex = 0 siempre y cuando el bono sea zerocoupon

	if cfIndex == 0 {
		info.accDays = 0
		info.currCoupon = (*cashflow)[cfIndex+0].Rate //because is the coupon on the next cashflow that will be paid.
		info.residual = 100
		info.lastCoupon = Fecha(*settlementDate)
		info.lastAmort = 0
	} else {
		info.accDays = int(time.Time(*settlementDate).Sub(time.Time((*cashflow)[cfIndex].Date)).Hours() / 24)
		info.currCoupon = (*cashflow)[cfIndex+1].Rate //because is the coupon on the next cashflow that will be paid.
		info.residual = (*cashflow)[cfIndex].Residual
		info.lastCoupon = (*cashflow)[cfIndex].Date
		info.lastAmort = (*cashflow)[cfIndex].Amort
	}

	info.accInt = (float64(info.accDays) / 360 * info.currCoupon) * info.residual * ratio
	info.techValue = float64(info.accInt) + info.residual*ratio
	info.parity = *p / info.techValue * 100

	return info

}

func Yield(flow []Flujo, price float64, settlementDate time.Time, initialFee float64, endingFee float64) (float64, error, int) {
	// settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
	// Discard all cashflows before the settlementDate

	values, dates, index := GenerateArrays(flow, settlementDate, initialFee, endingFee, price)

	rate, error := ScheduledInternalRateOfReturn(values, dates, 0.0001)
	if error != nil {
		return 0, error, 0
	}

	return rate, nil, index
}

func Mduration(flow []Flujo, rate float64, settlementDate time.Time, initialFee float64, endingFee float64, price float64) (float64, error) {
	values, dates, _ := GenerateArrays(flow, settlementDate, initialFee, endingFee, 0)

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

	// calculate the number of payments per year as the maximum number of payments in a year that appears in the dates vector
	datesPerYear := DatesPerYear(dates)

	return (-1 * (dur / (1 + rate/float64(datesPerYear)))), nil
}

func DatesPerYear(dateVector []time.Time) int {
	counts := make(map[int]int)

	for _, date := range dateVector[1:] {
		year := date.Year()
		counts[year]++
	}

	maxCount := 0
	for _, count := range counts {
		if count > maxCount {
			maxCount = count
		}
	}

	return maxCount
}

// Pass the casflow and get the slices separated to use with calculating functions.
// To get the cashflow to use with price, pass 0 as price
// To get the casfhflow to use with yield, pass the price obtained from the endpoint
// It returns index of the immediate cashflow before the settlementDate in order to obtain the number of days, coupon to calculate parity.
func GenerateArrays(flow []Flujo, settlementDate time.Time, initialFee float64, endingFee float64, price float64) ([]float64, []time.Time, int) {
	var index int
	for i, cf := range flow {
		fmt.Println(settlementDate.Add(-24 * time.Hour))
		if cf.Date.After(settlementDate.Add(-24 * time.Hour)) { // returns true if cf.Date is after date to settlementDate - 1
			index = int(math.Max(float64(i-1), 0))
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

	return values, dates, index

}

func Price(flow []Flujo, rate float64, settlementDate time.Time, initialFee float64, endingFee float64) (float64, error, int) {
	// settlementDate acts as cut-off date for the yield calculation. On every function call, all previous cashflows are discarded.
	// Discard all cashflows before the settlementDate
	values, dates, index := GenerateArrays(flow, settlementDate, initialFee, endingFee, 0)

	price, error := ScheduledNetPresentValue(rate, values, dates)
	if error != nil {
		return 0, error, 0
	}

	return price * (1 + initialFee), nil, index
}

func getBondsData() {
	fmt.Println("Leyendo data de bonos...")
	fmt.Println()

	data, err := ioutil.ReadFile("./bonds.json")
	if err != nil {
		fmt.Print(err)
	}
	// json data
	// unmarshall the loaded JSON
	err = json.Unmarshal([]byte(data), &Bonds)
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println()
	fmt.Println("Llenado de data de bonos exitosa")
	fmt.Println("Cantidad de bonos cargados: ", len(Bonds))
	fmt.Println()

}
