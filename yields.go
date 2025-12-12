package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"archive/zip"
	"bytes"
	"encoding/csv"
	"mime/multipart"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/jasonlvhit/gocron"
)

// to embed the time in a custom format to parse the dates that come from the json
type Fecha time.Time

const DateFormat = "2006-01-02"

var (
	Bonds    []Bond
	bondRepo *BondRepository
)

// Flag para considerar /apr deprecado
const aprDeprecatedMsg = "Endpoint /apr deprecado. Use /yield (maneja cero cupón automáticamente)."

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
	ID           string
	Ticker       string
	IssueDate    Fecha
	Maturity     Fecha
	Coupon       float64
	Cashflow     []Flujo
	Index        string
	Offset       int // Indexed bonds uses offset as date lookback period for the Index. In CER adjusted bonds this is set to 10 working days.
	DayCountConv int // Day count convention: 1=30/360, 2=Actual/365, 3=Actual/Actual, 4=Actual/360
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
	// Refresca CER cada 12 horas
	gocron.Every(12).Hours().Do(func() {
		LoadCERWithRetry(context.Background(), time.Minute)
	})
	<-gocron.Start()
}

func main() {
	// Hacemos una salida a stdout para que quede log del arranque del servicio, incorporando la hora y día
	fmt.Println("==================================================")
	fmt.Println("Arrancando servicio yields...", time.Now().Format("2006-01-02 15:04:05"))

	// SetUpCalendar creates the calendar and set ups the holidays for Argentina.
	SetUpCalendar()
	go executeCronJob() // this will make the cron run in the background.

	// load json with all the bond's data and handle any errors
	ctx := context.Background()
	var err error
	bondRepo, err = NewBondRepository()
	if err != nil {
		panic(fmt.Sprintf("Error inicializando repo de bonos: %v", err))
	}

	if err := bondRepo.EnsureSchema(ctx); err != nil {
		panic(fmt.Sprintf("Error asegurando schema de bonos: %v", err))
	}

	if os.Getenv("BOND_SEED_FROM_JSON") == "1" {
		if err := bondRepo.SeedFromJSON(ctx, "./bonds.json"); err != nil {
			fmt.Println("Error al seedear bonos desde JSON:", err)
		} else {
			fmt.Println("Seed de bonos desde JSON completado")
		}
	}

	if err := getBondsData(ctx); err != nil {
		panic(fmt.Sprintf("Error cargando bonos: %v", err))
	}

	// Load the CER data into Coef
	// Load CER con reintentos cada 1 minuto hasta éxito
	LoadCERWithRetry(context.Background(), time.Minute)
	//getCER()

	// start of the router and endpoints
	// start the router in debug mode
	//gin.SetMode(gin.DebugMode)
	//gin.SetMode(gin.ReleaseMode)

	// Server mode via env (defaults to release)
	// Valores variable de entorno: GIN_MODE='debug' o GIN_MODE='release'
	mode := os.Getenv("GIN_MODE")
	if mode == "" {
		mode = gin.ReleaseMode
	}
	gin.SetMode(mode)

	//router := gin.New()
	fmt.Println("Server is running on port 8080")
	fmt.Println("==================================================")
	// Router: minimal en producción
	router := gin.New()
	router.Use(gin.Recovery())

	if gin.Mode() == gin.DebugMode {
		router.Use(gin.Logger())
	}
	//router := gin.Default() //En caso de querer ver los logs de las requests
	// CORS for https://foo.com and https://github.com origins, allowing:
	// - PUT and PATCH methods
	// - Origin header
	// - Credentials share
	// - Preflight requests cached for 12 hours
	// No voy a usar CORS por ahora porque todo va a correr en localhost
	// router.Use(cors.New(cors.Config{
	// 	AllowOrigins: []string{"*"}, // or use https://foo.com, https://github.com, etc
	// 	AllowMethods: []string{"GET", "POST"},
	// 	AllowHeaders: []string{"Origin"},
	// 	//ExposeHeaders:    []string{"Content-Length"},
	// 	AllowCredentials: true,
	// 	MaxAge:           12 * time.Hour,
	// }))
	//router.Run()
	router.GET("/yield", yieldWrapper)
	router.GET("/apr", aprDeprecatedWrapper)
	router.GET("/price", priceWrapper)
	router.GET("/schedule", scheduleWrapper)
	router.POST("/upload", uploadWrapper)
	router.GET("/bonds", getBondsWrapper)
	// run the router
	router.Run("localhost:8080")
}

func aprWrapper(c *gin.Context) {
	c.Header("Warning", aprDeprecatedMsg)
	c.JSON(http.StatusGone, gin.H{
		"error": aprDeprecatedMsg,
	})
}

// Wrapper de compatibilidad para /apr
func aprDeprecatedWrapper(c *gin.Context) {
	aprWrapper(c)
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
	// API key required
	if !validateAPIKey(c) {
		return
	}
	if bondRepo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Repositorio de bonos no inicializado",
		})
		return
	}

	// Refrescar cache en memoria por si hubo cambios externos (ej. deletes manuales)
	_ = getBondsData(c.Request.Context())

	// Esperamos multipart con dos archivos: bonds (CSV) y cashflows (CSV)
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "multipart parse error: " + err.Error()})
		return
	}

	bondsFile, err := getFileFromForm(c, "bonds")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bonds.csv requerido: " + err.Error()})
		return
	}
	cfFile, err := getFileFromForm(c, "cashflows")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cashflows.csv requerido: " + err.Error()})
		return
	}

	bondsCSV, err := bondsFile.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no se pudo abrir bonds.csv: " + err.Error()})
		return
	}
	defer bondsCSV.Close()

	cfCSV, err := cfFile.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no se pudo abrir cashflows.csv: " + err.Error()})
		return
	}
	defer cfCSV.Close()

	bondsParsed, err := parseBondsCSV(bondsCSV)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "error parseando bonds.csv: " + err.Error()})
		return
	}
	cfParsed, err := parseCashflowsCSV(cfCSV)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "error parseando cashflows.csv: " + err.Error()})
		return
	}

	summary := processUpload(c.Request.Context(), bondsParsed, cfParsed)

	// Refrescar cache en memoria si hubo inserciones/updates
	if summary.Inserted > 0 || summary.Updated > 0 {
		_ = getBondsData(c.Request.Context())
	}

	c.JSON(http.StatusOK, summary)
}

// ===== Helpers para /upload =====
type bondCSVRow struct {
	Ticker       string
	IssueDate    time.Time
	Maturity     time.Time
	Coupon       float64
	Index        string
	Offset       int
	DayCountConv int
	Active       bool
	Operation    string // "insert" (default) | "update"
}

type cfCSVRow struct {
	Ticker   string
	Seq      int
	Date     time.Time
	Rate     float64
	Amort    float64
	Residual float64
	Amount   float64
}

type uploadSummary struct {
	Inserted int      `json:"inserted"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
	Missing  []string `json:"missing_cashflows"`
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func getFileFromForm(c *gin.Context, field string) (*multipart.FileHeader, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		return nil, err
	}
	return fh, nil
}

func parseBondsCSV(r io.Reader) ([]bondCSVRow, error) {
	cr := csv.NewReader(r)
	headers, err := cr.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range headers {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	req := []string{"ticker", "issue_date", "maturity", "coupon"}
	for _, k := range req {
		if _, ok := col[k]; !ok {
			return nil, fmt.Errorf("columna requerida faltante: %s", k)
		}
	}
	var rows []bondCSVRow
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(name string) string {
			if idx, ok := col[name]; ok && idx < len(rec) {
				return strings.TrimSpace(rec[idx])
			}
			return ""
		}
		ticker := strings.ToUpper(get("ticker"))
		if ticker == "" {
			continue
		}
		issueDate, err := time.Parse(DateFormat, get("issue_date"))
		if err != nil {
			return nil, fmt.Errorf("ticker %s: issue_date invalido: %v", ticker, err)
		}
		maturity, err := time.Parse(DateFormat, get("maturity"))
		if err != nil {
			return nil, fmt.Errorf("ticker %s: maturity invalido: %v", ticker, err)
		}
		coupon, err := strconv.ParseFloat(get("coupon"), 64)
		if err != nil {
			return nil, fmt.Errorf("ticker %s: coupon invalido: %v", ticker, err)
		}
		offset := 0
		if v := get("offset"); v != "" {
			if off, err := strconv.Atoi(v); err == nil {
				offset = off
			}
		}
		dayConv := 1
		if v := get("day_count_conv"); v != "" {
			if dc, err := strconv.Atoi(v); err == nil {
				dayConv = dc
			}
		}
		active := true
		if v := strings.ToLower(get("active")); v == "false" || v == "0" {
			active = false
		}
		op := strings.ToLower(get("operation"))
		if op == "" {
			op = "insert"
		}
		rows = append(rows, bondCSVRow{
			Ticker:       ticker,
			IssueDate:    issueDate,
			Maturity:     maturity,
			Coupon:       coupon,
			Index:        get("index"),
			Offset:       offset,
			DayCountConv: dayConv,
			Active:       active,
			Operation:    op,
		})
	}
	return rows, nil
}

func parseCashflowsCSV(r io.Reader) (map[string][]cfCSVRow, error) {
	cr := csv.NewReader(r)
	headers, err := cr.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range headers {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	req := []string{"ticker", "date", "rate", "amort", "residual", "amount"}
	for _, k := range req {
		if _, ok := col[k]; !ok {
			return nil, fmt.Errorf("columna requerida faltante en cashflows: %s", k)
		}
	}
	res := make(map[string][]cfCSVRow)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(name string) string {
			if idx, ok := col[name]; ok && idx < len(rec) {
				return strings.TrimSpace(rec[idx])
			}
			return ""
		}
		ticker := strings.ToUpper(get("ticker"))
		if ticker == "" {
			continue
		}
		date, err := time.Parse(DateFormat, get("date"))
		if err != nil {
			return nil, fmt.Errorf("ticker %s: date invalida: %v", ticker, err)
		}
		rate, _ := strconv.ParseFloat(get("rate"), 64)
		amort, _ := strconv.ParseFloat(get("amort"), 64)
		residual, _ := strconv.ParseFloat(get("residual"), 64)
		amount, _ := strconv.ParseFloat(get("amount"), 64)
		res[ticker] = append(res[ticker], cfCSVRow{
			Ticker:   ticker,
			Date:     date,
			Rate:     rate,
			Amort:    amort,
			Residual: residual,
			Amount:   amount,
		})
	}
	return res, nil
}

func processUpload(ctx context.Context, bonds []bondCSVRow, cf map[string][]cfCSVRow) uploadSummary {
	summary := uploadSummary{}
	// mapa de existencia actual
	existing := make(map[string]bool)
	for _, b := range Bonds {
		existing[b.Ticker] = true
	}

	for _, b := range bonds {
		flows, ok := cf[b.Ticker]
		if !ok || len(flows) == 0 {
			summary.Skipped++
			summary.Missing = append(summary.Missing, b.Ticker)
			continue
		}
		op := strings.ToLower(b.Operation)
		if op == "" {
			op = "insert"
		}
		if existing[b.Ticker] && op != "update" {
			summary.Skipped++
			summary.Errors = append(summary.Errors, fmt.Sprintf("ticker %s existe y no trae operation=update", b.Ticker))
			continue
		}
		if b.DayCountConv < 1 || b.DayCountConv > 4 {
			summary.Errors = append(summary.Errors, fmt.Sprintf("ticker %s: day_count_conv invalido", b.Ticker))
			summary.Skipped++
			continue
		}
		if b.Maturity.Before(b.IssueDate) {
			summary.Errors = append(summary.Errors, fmt.Sprintf("ticker %s: maturity antes de issue_date", b.Ticker))
			summary.Skipped++
			continue
		}

		// ordenar cashflows por fecha
		sort.Slice(flows, func(i, j int) bool {
			return flows[i].Date.Before(flows[j].Date)
		})
		// validar residual no creciente
		valid := true
		for i := 1; i < len(flows); i++ {
			if flows[i].Residual > flows[i-1].Residual+1e-9 {
				summary.Errors = append(summary.Errors, fmt.Sprintf("ticker %s: residual creciente en flujo %d", b.Ticker, i+1))
				summary.Skipped++
				valid = false
				break
			}
		}
		if !valid {
			continue
		}

		// reasignar seq
		for i := range flows {
			flows[i].Seq = i + 1
		}

		// construir Bond
		var bond Bond
		bond.Ticker = b.Ticker
		bond.IssueDate = Fecha(b.IssueDate)
		bond.Maturity = Fecha(b.Maturity)
		bond.Coupon = b.Coupon
		bond.Index = b.Index
		bond.Offset = b.Offset
		bond.DayCountConv = b.DayCountConv
		for _, f := range flows {
			bond.Cashflow = append(bond.Cashflow, Flujo{
				Date:     Fecha(f.Date),
				Rate:     f.Rate,
				Amort:    f.Amort,
				Residual: f.Residual,
				Amount:   f.Amount,
			})
		}

		_, err := bondRepo.InsertBondWithCashflows(ctx, &bond)
		if err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("ticker %s: %v", b.Ticker, err))
			summary.Skipped++
			continue
		}
		if existing[b.Ticker] {
			summary.Updated++
		} else {
			summary.Inserted++
		}
	}
	return summary
}

func scheduleWrapper(c *gin.Context) {
	tickersParam := c.QueryArray("ticker")
	if len(tickersParam) == 0 {
		if single := c.Query("ticker"); single != "" {
			tickersParam = []string{single}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ticker is required"})
			return
		}
	}
	cutoffParam := c.Query("settlementDate")
	var cutoff *time.Time
	if cutoffParam != "" {
		t, err := time.Parse(DateFormat, cutoffParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid settlementDate"})
			return
		}
		cutoff = &t
	}

	type bondRow struct {
		Ticker       string
		IssueDate    string
		Maturity     string
		Coupon       float64
		Index        string
		Offset       int
		DayCountConv int
	}

	missing := []string{}
	var bondsOut []bondRow
	var cashflowsOut [][]string

	for _, tkr := range tickersParam {
		ticker := strings.ToUpper(tkr)
		cf, idx, err := getCashFlow(ticker)
		if err != nil {
			missing = append(missing, ticker)
			continue
		}
		b := Bonds[idx]
		bondsOut = append(bondsOut, bondRow{
			Ticker:       b.Ticker,
			IssueDate:    time.Time(b.IssueDate).Format(DateFormat),
			Maturity:     time.Time(b.Maturity).Format(DateFormat),
			Coupon:       b.Coupon,
			Index:        b.Index,
			Offset:       b.Offset,
			DayCountConv: b.DayCountConv,
		})

		// filtrar por cutoff si aplica
		filtered := cf
		if cutoff != nil {
			filtered = nil
			for _, row := range cf {
				if time.Time(row.Date).After(cutoff.Add(-24*time.Hour)) || time.Time(row.Date).Equal(*cutoff) {
					filtered = append(filtered, row)
				}
			}
		}

		for i, row := range filtered {
			cashflowsOut = append(cashflowsOut, []string{
				ticker,
				strconv.Itoa(i + 1),
				time.Time(row.Date).Format(DateFormat),
				formatFloat(row.Rate),
				formatFloat(row.Amort),
				formatFloat(row.Residual),
				formatFloat(row.Amount),
			})
		}
	}

	// Si no hay bonos encontrados
	if len(bondsOut) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no tickers found", "missing": missing})
		return
	}

	var buf bytes.Buffer
	zipw := zip.NewWriter(&buf)

	// bonds.csv
	bondsFile, _ := zipw.Create("bonds.csv")
	bw := csv.NewWriter(bondsFile)
	_ = bw.Write([]string{"ticker", "issue_date", "maturity", "coupon", "index", "offset", "day_count_conv"})
	for _, b := range bondsOut {
		_ = bw.Write([]string{
			b.Ticker,
			b.IssueDate,
			b.Maturity,
			formatFloat(b.Coupon),
			b.Index,
			strconv.Itoa(b.Offset),
			strconv.Itoa(b.DayCountConv),
		})
	}
	bw.Flush()

	// cashflows.csv
	cfFile, _ := zipw.Create("cashflows.csv")
	cfw := csv.NewWriter(cfFile)
	_ = cfw.Write([]string{"ticker", "seq", "date", "rate", "amort", "residual", "amount"})
	for _, row := range cashflowsOut {
		_ = cfw.Write(row)
	}
	cfw.Flush()

	zipw.Close()

	c.Writer.Header().Set("Content-Type", "application/zip")
	c.Writer.Header().Set("Content-Disposition", "attachment;filename=schedule.zip")
	if len(missing) > 0 {
		c.Writer.Header().Set("X-Missing-Tickers", strings.Join(missing, ","))
	}
	c.Writer.Write(buf.Bytes())
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
	// Normalizar ticker a mayúsculas para evitar problemas de case sensitivity
	ticker = strings.ToUpper(ticker)
	for i, bond := range Bonds {
		if bond.Ticker == ticker {
			return bond.Cashflow, i, nil
		}
	}
	// En modo debug, mostrar información útil
	if gin.Mode() == gin.DebugMode {
		fmt.Printf("DEBUG: Ticker '%s' no encontrado. Total de bonos cargados: %d\n", ticker, len(Bonds))
		if len(Bonds) > 0 && len(Bonds) <= 10 {
			fmt.Printf("DEBUG: Tickers disponibles: %v\n", func() []string {
				var tickers []string
				for _, b := range Bonds {
					tickers = append(tickers, b.Ticker)
				}
				return tickers
			}())
		}
	}
	return nil, -1, fmt.Errorf("ticker '%s' not found", ticker)
}

// ======= API Key validation =======
func validateAPIKey(c *gin.Context) bool {
	apiKey := c.GetHeader("X-API-Key")
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "API key missing"})
		return false
	}
	if bondRepo == nil || bondRepo.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "repository not initialized"})
		return false
	}
	var active bool
	err := bondRepo.db.QueryRowContext(c.Request.Context(),
		`SELECT active FROM yields_api_keys WHERE api_key = $1`, apiKey).Scan(&active)
	if err != nil || !active {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
		return false
	}
	_, _ = bondRepo.db.ExecContext(c.Request.Context(),
		`UPDATE yields_api_keys SET last_used_at = now() WHERE api_key = $1`, apiKey)
	return true
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

	// Ajuste por índice (CER) previo, para usar en ambos caminos (cero cupón y general)
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
		tmpFecha := calendar.WorkdaysFrom(issueDate, offset)
		coef2, err = getCoefficient(tmpFecha, extendIndex, &Coef)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Error in CER. ": err.Error()})
			return
		}

		ratio = coef1 / coef2

	}

	// Unificar: si es bono cero cupón (un solo flujo futuro) usamos fórmula cerrada como /apr
	if len(cashFlow) == 1 {
		dayCountConv := Bonds[index].DayCountConv
		if dayCountConv == 0 {
			dayCountConv = DayCount30_360 // Default a 30/360
		}

		// Calcular fracción de año hasta vencimiento según convención
		maturityDate := time.Time(cashFlow[0].Date)
		yearFraction := calculateDays(dayCountConv, settlementDate, maturityDate)

		// Calcular yield usando la fracción de año correcta
		// r = ((100*(1-endingFee))/(price*(1+initialFee)/ratio) - 1) / yearFraction
		r := ((100*(1-endingFee))/((price*(1+initialFee))/ratio) - 1) / yearFraction
		mduration := yearFraction / (1 + r)

		// Calcular días devengados desde emisión
		accDaysFloat := calculateDays(dayCountConv, time.Time(Bonds[index].IssueDate), settlementDate)
		accDays := int(settlementDate.Sub(time.Time(Bonds[index].IssueDate)).Hours() / 24) // Para display

		coupon := Bonds[index].Coupon
		residual := cashFlow[0].Residual + cashFlow[0].Amort

		// Calcular interés devengado según convención
		var accInt float64
		if dayCountConv == DayCountActualActual {
			daysInYear := calculateDaysInYear(settlementDate)
			actualDays := settlementDate.Sub(time.Time(Bonds[index].IssueDate)).Hours() / 24
			accInt = (actualDays / daysInYear * coupon) * residual * ratio
		} else {
			accInt = accDaysFloat * coupon * residual * ratio
		}

		techValue := ratio*residual + accInt
		parity := price / techValue * 100
		conv, _ := Convexity(cashFlow, r, settlementDate, initialFee, endingFee, price, dayCountConv)
		// Para TNA usamos el precio ajustado por ratio (similar a cómo se ajusta para yield)
		adjustedPrice := price / ratio
		tna, tnaErr := CalculateTNA(r, adjustedPrice, cashFlow, Bonds[index].IssueDate, Bonds[index].Maturity, settlementDate)
		if tnaErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("Error calculating TNA: %v", tnaErr)})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"Yield":                 r,
			"MDuration":             mduration,
			"Convexity":             conv,
			"TNA":                   tna,
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
			"Maturity":              Bonds[index].Maturity,
			"Note":                  "Cero cupón (fórmula cerrada). /apr está deprecado.",
		})
		return
	}

	// adjust price, if the bond is indexed, by using the ratio calculated by dividing the index of settlementDate by the index of IssueDate.
	// There's an offset variable to adjust the lookback period for the index.

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
	dayCountConv := Bonds[index].DayCountConv
	if dayCountConv == 0 {
		dayCountConv = DayCount30_360 // Default a 30/360
	}
	info := extendedInfo(&settlementDate, &cashFlow, &origPrice, cfIndex, ratio, dayCountConv)
	conv, _ := Convexity(cashFlow, r, settlementDate, initialFee, endingFee, price, dayCountConv)
	// Para TNA usamos el precio ajustado por ratio (price ya está ajustado en línea 921)
	tna, tnaErr := CalculateTNA(r, price, cashFlow, Bonds[index].IssueDate, Bonds[index].Maturity, settlementDate)
	if tnaErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("Error calculating TNA: %v", tnaErr)})
		return
	}
	//accDays, coupon, residual, accInt, techValue, parity, lastCoupon, _ := extendedInfo(&settlementDate, &cashFlow, &origPrice, cfIndex)

	c.JSON(http.StatusOK, gin.H{
		"Yield":                 r,
		"MDuration":             mduration,
		"Convexity":             conv,
		"TNA":                   tna,
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
		"Maturity":              Bonds[index].Maturity,
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
	dayCountConv := Bonds[index].DayCountConv
	if dayCountConv == 0 {
		dayCountConv = DayCount30_360 // Default a 30/360
	}
	origPrice := p / ratio
	info := extendedInfo(&settlementDate, &cashFlow, &origPrice, cfIndex, ratio, dayCountConv)
	conv, _ := Convexity(cashFlow, rate, settlementDate, initialFee, endingFee, p, dayCountConv)
	tna, tnaErr := CalculateTNA(rate, p, cashFlow, Bonds[index].IssueDate, Bonds[index].Maturity, settlementDate)
	if tnaErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("Error calculating TNA: %v", tnaErr)})
		return
	}
	//accDays, coupon, residual, accInt, techValue, parity, lastCoupon, _ := extendedInfo(&settlementDate, &cashFlow, &p, cfIndex)

	c.JSON(http.StatusOK, gin.H{
		"Price":                 p,
		"MDuration":             mduration,
		"Convexity":             conv,
		"TNA":                   tna,
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
		"Maturity":              Bonds[index].Maturity,
	})

}

func extendedInfo(settlementDate *time.Time, cashflow *[]Flujo, p *float64, cfIndex int, ratio float64, dayCountConv int) extInfo {
	var info extInfo

	//teng que dejar cfIndex = 0 siempre y cuando el bono sea zerocoupon

	if cfIndex == 0 {
		info.accDays = 0
		info.currCoupon = (*cashflow)[cfIndex+0].Rate //because is the coupon on the next cashflow that will be paid.
		info.residual = 100
		info.lastCoupon = Fecha(*settlementDate)
		info.lastAmort = 0
		info.accInt = 0 // No hay interés devengado antes del primer cupón
	} else {
		// Calcular días devengados según la convención
		lastCouponDate := time.Time((*cashflow)[cfIndex].Date)
		accDaysFloat := calculateDays(dayCountConv, lastCouponDate, *settlementDate)

		// Para Actual/Actual, necesitamos calcular el año fraccional correctamente
		var yearFraction float64
		if dayCountConv == DayCountActualActual {
			// Calcular días reales del período
			actualDays := settlementDate.Sub(lastCouponDate).Hours() / 24
			// Calcular días del año para la fecha del último cupón
			daysInYear := calculateDaysInYear(lastCouponDate)
			yearFraction = actualDays / daysInYear
		} else {
			yearFraction = accDaysFloat
		}

		info.accDays = int(settlementDate.Sub(lastCouponDate).Hours() / 24) // Mantener días enteros para display
		info.currCoupon = (*cashflow)[cfIndex+1].Rate                       //because is the coupon on the next cashflow that will be paid.
		// Residual debe reflejar el saldo vivo luego del último flujo aplicado.
		// Tomamos el residual del flujo actual (cfIndex) que ya incorpora la amortización pagada en ese cupón.
		info.residual = (*cashflow)[cfIndex].Residual
		info.lastCoupon = (*cashflow)[cfIndex].Date
		info.lastAmort = (*cashflow)[cfIndex].Amort

		// Calcular interés devengado usando la fracción de año correcta
		info.accInt = yearFraction * info.currCoupon * info.residual * ratio
	}

	info.techValue = float64(info.accInt) + info.residual*ratio
	info.parity = *p / info.techValue * 100

	return info

}

// Convexity calcula la convexidad (años^2) usando los mismos flujos y convención de días
// Se asume rate en términos efectivos para el período usado (misma convención que Mduration/Yield)
func Convexity(flow []Flujo, rate float64, settlementDate time.Time, initialFee float64, endingFee float64, price float64, dayCountConv int) (float64, error) {
	values, dates, _ := GenerateArrays(flow, settlementDate, initialFee, endingFee, 0)
	if len(values) != len(dates) {
		return 0, errors.New("values and dates must have the same length")
	}

	denom := price
	if denom == 0 {
		denom = 1 // evitar división por cero; retornará 0 si no hay precio
	}

	var num float64
	for i := 1; i < len(values); i++ { // desde el primer flujo (excluye el inicial en 0)
		tYears := calculateDays(dayCountConv, settlementDate, dates[i])
		discount := math.Pow(1+rate, tYears)
		if discount == 0 {
			return 0, errors.New("invalid discount factor")
		}
		num += values[i] * tYears * (tYears + 1) / discount
	}

	conv := num / (denom * math.Pow(1+rate, 2))
	return conv, nil
}

// days360Between calcula días en base 360 (30/360) entre dos fechas, devolviendo días enteros
func days360Between(startDate, endDate time.Time) int {
	d1 := startDate.Day()
	m1 := int(startDate.Month())
	y1 := startDate.Year()

	d2 := endDate.Day()
	m2 := int(endDate.Month())
	y2 := endDate.Year()

	// Ajustar D1 si es 31
	//originalD1 := d1 // se usa abajo para debug
	if d1 == 31 {
		d1 = 30
	}

	// Ajustar D2 si es 31 y D1 es 30 o 31
	//originalD2 := d2 // se usa abajo para debug
	if d2 == 31 && (d1 == 30 || d1 == 31) {
		d2 = 30
	}

	// Calcular días: (Y2-Y1)*360 + (M2-M1)*30 + (D2-D1)
	yearDiff := y2 - y1
	monthDiff := m2 - m1
	dayDiff := d2 - d1
	days := yearDiff*360 + monthDiff*30 + dayDiff

	/*fmt.Printf("DEBUG days360Between: startDate=%s (D=%d M=%d Y=%d), endDate=%s (D=%d M=%d Y=%d), d1_original=%d->%d, d2_original=%d->%d, yearDiff=%d, monthDiff=%d, dayDiff=%d, days360=%d\n",
	startDate.Format(DateFormat), originalD1, m1, y1,
	endDate.Format(DateFormat), originalD2, m2, y2,
	originalD1, d1, originalD2, d2, yearDiff, monthDiff, dayDiff, days) */

	return days
}

// CalculateTNA calcula la Tasa Nominal Anual según el tipo de bono
func CalculateTNA(yield float64, price float64, cashflow []Flujo, issueDate Fecha, maturity Fecha, settlementDate time.Time) (float64, error) {
	if price == 0 {
		return 0, errors.New("price cannot be zero for TNA calculation")
	}

	// Bono cero cupón: un solo flujo
	if len(cashflow) == 1 {
		amount := cashflow[0].Amount
		//issueDateTime := time.Time(issueDate) // se usa abajo para debug
		maturityTime := time.Time(maturity)
		days360 := days360Between(settlementDate, maturityTime)
		if days360 == 0 {
			return 0, errors.New("days360 cannot be zero for zero coupon bond TNA calculation")
		}
		amountOverPrice := amount / price
		ratio := (amountOverPrice - 1)
		daysRatio := 360.0 / float64(days360)
		tna := ratio * daysRatio
		/*fmt.Printf("DEBUG TNA (cero cupón): issueDate=%s, maturity=%s, days360=%d, amount=%.6f, price=%.6f, amount/price=%.6f, (amount/price-1)=%.6f, days360/360=%.6f, TNA=%.6f\n",
		issueDateTime.Format(DateFormat), maturityTime.Format(DateFormat), days360, amount, price, amountOverPrice, ratio, daysRatio, tna) */
		return tna, nil
	}

	// Bono con cupones: usar período entre 1er y 2do flujo
	if len(cashflow) < 2 {
		return 0, errors.New("cashflow must have at least 2 flows for coupon bond TNA calculation")
	}

	days360 := days360Between(time.Time(cashflow[0].Date), time.Time(cashflow[1].Date))
	if days360 == 0 {
		return 0, errors.New("days360 cannot be zero for coupon bond TNA calculation")
	}

	// TNA = ((1 + yield)^(dias360/360) - 1) * (360/dias360)
	periodYears := float64(days360) / 360.0
	tna := (math.Pow(1+yield, periodYears) - 1) * (360.0 / float64(days360))
	return tna, nil
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
		//fmt.Println(settlementDate.Add(-24 * time.Hour))
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

func getBondsData(ctx context.Context) error {
	fmt.Println("Leyendo data de bonos...")
	fmt.Println()

	if bondRepo == nil {
		return fmt.Errorf("bondRepo no inicializado")
	}

	var err error
	Bonds, err = bondRepo.LoadAllBonds(ctx)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Llenado de data de bonos exitosa")
	fmt.Println("Cantidad de bonos cargados: ", len(Bonds))
	fmt.Println()

	return nil

}
