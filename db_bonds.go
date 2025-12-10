package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"strings"
)

// BondRepository encapsula persistencia en DB para bonos.
// Mantiene el contrato actual: los bonos siguen cargándose a memoria.
type BondRepository struct {
	db *sql.DB
}

// Instancia un repositorio sobre PostgreSQL usando las mismas envs que CER.
func NewBondRepository() (*BondRepository, error) {
	dbUser := os.Getenv("POSTGRES_USER")
	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	dbHost := os.Getenv("POSTGRES_HOST")
	dbPort := os.Getenv("POSTGRES_PORT")
	dbName := os.Getenv("POSTGRES_DB")

	if dbUser == "" || dbPassword == "" || dbHost == "" || dbPort == "" || dbName == "" {
		return nil, fmt.Errorf("faltan variables de entorno de Postgres (POSTGRES_USER/PASSWORD/HOST/PORT/DB)")
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &BondRepository{db: db}, nil
}

// EnsureSchema crea tablas si no existen.
func (r *BondRepository) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS index_types (
			id SERIAL PRIMARY KEY,
			code TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			description TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS bonds (
			id SERIAL PRIMARY KEY,
			ticker TEXT UNIQUE NOT NULL,
			issue_date DATE NOT NULL,
			maturity DATE NOT NULL,
			coupon NUMERIC NOT NULL,
			index_type_id INT REFERENCES index_types(id),
			"offset" INT DEFAULT 0,
			active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT now()
		);`,
		`CREATE TABLE IF NOT EXISTS bond_cashflows (
			id SERIAL PRIMARY KEY,
			bond_id INT REFERENCES bonds(id) ON DELETE CASCADE,
			seq INT NOT NULL,
			date DATE NOT NULL,
			rate NUMERIC NOT NULL,
			amort NUMERIC NOT NULL,
			residual NUMERIC NOT NULL,
			amount NUMERIC NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
			UNIQUE (bond_id, seq)
		);`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// seed mínimo de catálogo CER si no existe
	_, _ = r.db.ExecContext(ctx,
		`INSERT INTO index_types (code, name, description)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (code) DO NOTHING`, "CER", "CER", "Coeficiente de Estabilización de Referencia")
	return nil
}

// LoadAllBonds trae todos los bonos activos con sus cashflows.
func (r *BondRepository) LoadAllBonds(ctx context.Context) ([]Bond, error) {
	type bondRow struct {
		id           int
		ticker       string
		issue        time.Time
		maturity     time.Time
		coupon       float64
		indexCode    sql.NullString
		offset       int
		dayCountConv sql.NullInt64
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT b.id, b.ticker, b.issue_date, b.maturity, b.coupon,
		       it.code, b."offset", COALESCE(b.day_count_conv, 1) as day_count_conv
		FROM bonds b
		LEFT JOIN index_types it ON it.id = b.index_type_id
		WHERE b.active = TRUE
		ORDER BY b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bondRows []bondRow
	for rows.Next() {
		var br bondRow
		if err := rows.Scan(&br.id, &br.ticker, &br.issue, &br.maturity, &br.coupon, &br.indexCode, &br.offset, &br.dayCountConv); err != nil {
			return nil, err
		}
		bondRows = append(bondRows, br)
	}

	// Traer cashflows en un solo query
	cfRows, err := r.db.QueryContext(ctx, `
		SELECT bond_id, seq, date, rate, amort, residual, amount
		FROM bond_cashflows
		ORDER BY bond_id, seq`)
	if err != nil {
		return nil, err
	}
	defer cfRows.Close()

	cfMap := make(map[int][]Flujo)
	for cfRows.Next() {
		var bondID, seq int
		var date time.Time
		var rate, amort, residual, amount float64
		if err := cfRows.Scan(&bondID, &seq, &date, &rate, &amort, &residual, &amount); err != nil {
			return nil, err
		}
		cfMap[bondID] = append(cfMap[bondID], Flujo{
			Date:     Fecha(date),
			Rate:     rate,
			Amort:    amort,
			Residual: residual,
			Amount:   amount,
		})
	}

	var bonds []Bond
	for _, br := range bondRows {
		dayCountConv := DayCount30_360 // Default a 30/360
		if br.dayCountConv.Valid {
			dayCountConv = int(br.dayCountConv.Int64)
		}
		bonds = append(bonds, Bond{
			ID:           strconv.Itoa(br.id),
			Ticker:       br.ticker,
			IssueDate:    Fecha(br.issue),
			Maturity:     Fecha(br.maturity),
			Coupon:       br.coupon,
			Cashflow:     cfMap[br.id],
			Index:        br.indexCode.String,
			Offset:       br.offset,
			DayCountConv: dayCountConv,
		})
	}
	return bonds, nil
}

// InsertBondWithCashflows inserta bono + cashflows y devuelve el ID como string.
func (r *BondRepository) InsertBondWithCashflows(ctx context.Context, bond *Bond) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var indexTypeID *int
	if bond.Index != "" {
		var id int
		err := tx.QueryRowContext(ctx,
			`INSERT INTO index_types (code, name)
			 VALUES ($1, $1)
			 ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
			 RETURNING id`, bond.Index).Scan(&id)
		if err != nil {
			return "", err
		}
		indexTypeID = &id
	}

	var bondID int
	dayCountConv := bond.DayCountConv
	if dayCountConv == 0 {
		dayCountConv = DayCount30_360 // Default a 30/360
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO bonds (ticker, issue_date, maturity, coupon, index_type_id, "offset", day_count_conv)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (ticker) DO UPDATE SET
			issue_date = EXCLUDED.issue_date,
			maturity = EXCLUDED.maturity,
			coupon = EXCLUDED.coupon,
			index_type_id = EXCLUDED.index_type_id,
			"offset" = EXCLUDED."offset",
			day_count_conv = EXCLUDED.day_count_conv,
			updated_at = now()
		RETURNING id`,
		bond.Ticker, time.Time(bond.IssueDate), time.Time(bond.Maturity), bond.Coupon, indexTypeID, bond.Offset, dayCountConv).
		Scan(&bondID)
	if err != nil {
		return "", err
	}

	// Limpiamos cashflows previos para reimportar completo (manteniendo contrato actual).
	if _, err := tx.ExecContext(ctx, `DELETE FROM bond_cashflows WHERE bond_id = $1`, bondID); err != nil {
		return "", err
	}

	for i, cf := range bond.Cashflow {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO bond_cashflows (bond_id, seq, date, rate, amort, residual, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			bondID, i+1, time.Time(cf.Date), cf.Rate, cf.Amort, cf.Residual, cf.Amount); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return strconv.Itoa(bondID), nil
}

// SeedFromJSON carga bonds.json a DB. No borra datos existentes; actualiza por ticker.
func (r *BondRepository) SeedFromJSON(ctx context.Context, path string) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var bonds []Bond
	if err := json.Unmarshal(payload, &bonds); err != nil {
		return err
	}
	for i := range bonds {
		// respeta mayúsculas para tickers, IDs se regeneran en DB
		bonds[i].Ticker = strings.ToUpper(bonds[i].Ticker)
		if _, err := r.InsertBondWithCashflows(ctx, &bonds[i]); err != nil {
			return fmt.Errorf("falló insert para ticker %s: %w", bonds[i].Ticker, err)
		}
	}
	return nil
}
