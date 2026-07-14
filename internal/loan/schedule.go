package loan

import (
	"time"
)

const TenorWeeks = 50

type Terms struct {
	Principal  int64
	RateBps    int       // basis points, flat p.a. 1000 = 10.00%
	TenorWeeks int       // number of installments
	StartDate  time.Time // week w falls due StartDate + 7w days
}

type Installment struct {
	WeekNumber int
	DueDate    time.Time
	Amount     int64
}

type Schedule struct {
	TotalPayable      int64
	InstallmentAmount int64 // the uniform weekly amount
	Installments      []Installment
}

func GenerateSchedule(t Terms) Schedule {
	totalPayable := t.Principal + (t.Principal*int64(t.RateBps))/10000

	weekly := totalPayable / int64(t.TenorWeeks)
	lastWeek := totalPayable - weekly*int64(t.TenorWeeks-1)

	start := utcDate(t.StartDate)

	installments := make([]Installment, 0, t.TenorWeeks)
	for w := 1; w <= t.TenorWeeks; w++ {
		amount := weekly
		if w == t.TenorWeeks {
			amount = lastWeek
		}

		installments = append(installments, Installment{
			WeekNumber: w,
			DueDate:    start.AddDate(0, 0, 7*w),
			Amount:     amount,
		})
	}

	return Schedule{
		TotalPayable:      totalPayable,
		InstallmentAmount: weekly,
		Installments:      installments,
	}
}

// utcDate strips the time of day. Due dates are DATEs: an installment due on day
// D is payable through D 23:59:59 UTC and becomes overdue at D+1 00:00:00 UTC.
func utcDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
