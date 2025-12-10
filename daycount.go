package main

import (
	"time"
)

// Constantes para convenciones de conteo de días
const (
	DayCount30_360       = 1
	DayCountActual365    = 2
	DayCountActualActual = 3
	DayCountActual360    = 4
)

// calculateDays calcula los días entre dos fechas según la convención especificada
func calculateDays(convention int, startDate, endDate time.Time) float64 {
	switch convention {
	case DayCount30_360:
		return thirty360(startDate, endDate)
	case DayCountActual365:
		return actual365(startDate, endDate)
	case DayCountActualActual:
		return actualActual(startDate, endDate)
	case DayCountActual360:
		return actual360(startDate, endDate)
	default:
		// Default a 30/360 si no se especifica o es inválido
		return thirty360(startDate, endDate)
	}
}

// actual360: días reales / 360
func actual360(startDate, endDate time.Time) float64 {
	days := endDate.Sub(startDate).Hours() / 24
	return days / 360.0
}

// actual365: días reales / 365
func actual365(startDate, endDate time.Time) float64 {
	days := endDate.Sub(startDate).Hours() / 24
	return days / 365.0
}

// actualActual: días reales del período / días reales del año
// Para períodos que cruzan años, se calcula proporcionalmente
func actualActual(startDate, endDate time.Time) float64 {
	days := endDate.Sub(startDate).Hours() / 24

	// Si el período es menor a un año, calcular días del año
	if days < 365 {
		year := startDate.Year()
		yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, startDate.Location())
		yearEnd := time.Date(year+1, 1, 1, 0, 0, 0, 0, startDate.Location())
		daysInYear := yearEnd.Sub(yearStart).Hours() / 24
		return days / daysInYear
	}

	// Para períodos mayores a un año, calcular año por año
	totalYears := 0.0
	current := startDate
	for current.Before(endDate) {
		year := current.Year()
		yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, current.Location())
		yearEnd := time.Date(year+1, 1, 1, 0, 0, 0, 0, current.Location())
		daysInYear := yearEnd.Sub(yearStart).Hours() / 24

		var periodEnd time.Time
		if yearEnd.After(endDate) {
			periodEnd = endDate
		} else {
			periodEnd = yearEnd
		}

		periodDays := periodEnd.Sub(current).Hours() / 24
		totalYears += periodDays / daysInYear
		current = yearEnd
	}

	return totalYears
}

// thirty360: convención 30/360 (reglas simplificadas)
// D1/M1/Y1 a D2/M2/Y2
// Si D1 = 31, entonces D1 = 30
// Si D2 = 31 y D1 = 30 o 31, entonces D2 = 30
func thirty360(startDate, endDate time.Time) float64 {
	d1 := startDate.Day()
	m1 := int(startDate.Month())
	y1 := startDate.Year()

	d2 := endDate.Day()
	m2 := int(endDate.Month())
	y2 := endDate.Year()

	// Ajustar D1 si es 31
	if d1 == 31 {
		d1 = 30
	}

	// Ajustar D2 si es 31 y D1 es 30 o 31
	if d2 == 31 && (d1 == 30 || d1 == 31) {
		d2 = 30
	}

	// Calcular días: (Y2-Y1)*360 + (M2-M1)*30 + (D2-D1)
	days := float64((y2-y1)*360 + (m2-m1)*30 + (d2 - d1))
	return days / 360.0
}

// calculateDaysInYear calcula los días en el año para una fecha dada (útil para Actual/Actual)
func calculateDaysInYear(date time.Time) float64 {
	year := date.Year()
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, date.Location())
	yearEnd := time.Date(year+1, 1, 1, 0, 0, 0, 0, date.Location())
	return yearEnd.Sub(yearStart).Hours() / 24
}

// calculateAccruedInterest calcula el interés devengado según la convención
func calculateAccruedInterest(convention int, accDays float64, coupon float64, residual float64, ratio float64) float64 {
	var yearFraction float64

	switch convention {
	case DayCount30_360:
		// Para 30/360, accDays ya está en días, dividir por 360
		yearFraction = accDays / 360.0
	case DayCountActual365:
		yearFraction = accDays / 365.0
	case DayCountActualActual:
		// Para Actual/Actual, necesitamos calcular el año fraccional correctamente
		// Esto se maneja mejor en el contexto donde tenemos las fechas completas
		yearFraction = accDays / 365.0 // Aproximación, se ajustará en extendedInfo
	case DayCountActual360:
		yearFraction = accDays / 360.0
	default:
		yearFraction = accDays / 360.0
	}

	return yearFraction * coupon * residual * ratio
}
