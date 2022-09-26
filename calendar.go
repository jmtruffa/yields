package main

// here I setup the calendar to use

import (
	"time"

	"github.com/rickar/cal/v2"
	"github.com/rickar/cal/v2/ar"
)

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
	Name:  "Aniversario paso a la inmortalidad del General Juan Manuel Belgrano",
	Type:  cal.ObservancePublic,
	Month: time.June,
	Day:   20,
	Func:  cal.CalcDayOfMonth,
}

var IntentoAsesinatoCFK = &cal.Holiday{
	Name:      "Feriado por intento asesinato CFK",
	Type:      cal.ObservancePublic,
	StartYear: 2022,
	EndYear:   2022,
	Month:     time.September,
	Day:       2,
	Func:      cal.CalcDayOfMonth,
}

var FeriadoTuristicoOctubre2022 = &cal.Holiday{
	Name:      "Feriado turístico ad hoc",
	Type:      cal.ObservancePublic,
	StartYear: 2022,
	EndYear:   2022,
	Month:     time.October,
	Day:       7,
	Func:      cal.CalcDayOfMonth,
}

var FeriadoTuristicoNoviembre2022 = &cal.Holiday{
	Name:      "Feriado turístico ad hoc",
	Type:      cal.ObservancePublic,
	StartYear: 2022,
	EndYear:   2022,
	Month:     time.November,
	Day:       21,
	Func:      cal.CalcDayOfMonth,
}

var FeriadoTuristicoDiciembre2022 = &cal.Holiday{
	Name:      "Feriado turístico ad hoc",
	Type:      cal.ObservancePublic,
	StartYear: 2022,
	EndYear:   2022,
	Month:     time.December,
	Day:       9,
	Func:      cal.CalcDayOfMonth,
}

var calendar = cal.NewBusinessCalendar()

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
		IntentoAsesinatoCFK,
	)
}
