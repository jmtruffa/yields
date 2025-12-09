package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/rickar/cal/v2"
)

var calendar = cal.NewBusinessCalendar()

// Carga inicial y programa recarga diaria desde Postgres.
func SetUpCalendar() {
	ctx := context.Background()
	if err := LoadHolidaysFromDB(ctx); err != nil {
		fmt.Println("No se pudieron cargar feriados desde DB:", err)
	}
	// Recarga diaria
	t := time.NewTicker(24 * time.Hour)
	go func() {
		for range t.C {
			if err := LoadHolidaysFromDB(ctx); err != nil {
				fmt.Println("Recarga de feriados falló:", err)
			} else {
				fmt.Println("Feriados recargados desde DB")
			}
		}
	}()
}

// Re-carga feriados desde la tabla `calendario_feriados`.
// Espera columnas: date (DATE/TIMESTAMP) y name (TEXT).
func LoadHolidaysFromDB(ctx context.Context) error {
	dbUser := os.Getenv("POSTGRES_USER")
	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	dbHost := os.Getenv("POSTGRES_HOST")
	dbPort := os.Getenv("POSTGRES_PORT")
	dbName := os.Getenv("POSTGRES_DB")

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName,
	)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	// Ajusta los nombres de columnas si en tu tabla difieren.
	rows, err := db.QueryContext(ctx, `SELECT date FROM "calendarioFeriados"`)
	if err != nil {
		return fmt.Errorf("query feriados: %w", err)
	}
	defer rows.Close()

	newCal := cal.NewBusinessCalendar()

	var d time.Time
	count := 0
	for rows.Next() {
		if err := rows.Scan(&d); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		y, m, day := d.Date()
		h := &cal.Holiday{
			Name:      "Feriado",
			Type:      cal.ObservancePublic,
			StartYear: y,
			EndYear:   y,
			Month:     m,
			Day:       day,
			Func:      cal.CalcDayOfMonth,
		}
		newCal.AddHoliday(h)
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}

	calendar = newCal
	fmt.Println("Feriados cargados desde DB:", count)
	return nil
}
