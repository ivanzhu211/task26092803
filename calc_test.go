package worktime

import (
	"reflect"
	"testing"
	"time"
)

// 参考周：2026-09-27 为周日，W0 = 09-27..10-03，W1 自 10-04 起。
var loc = time.FixedZone("TEST", 8*3600)

func tm(m, d, hh, mm, ss int) time.Time {
	return time.Date(2026, time.Month(m), d, hh, mm, ss, 0, loc)
}

func p(id, emp string, typ PunchType, t time.Time) Punch {
	return Punch{ID: id, EmployeeID: emp, Type: typ, Time: t}
}

var defaultCfg = Config{RoundingMinutes: 5, DailyOTEnabled: true}

func seg(start, end time.Time, cat Category, ids ...string) Segment {
	return Segment{Start: start, End: end, Category: cat, RecordIDs: ids}
}

func findEmp(t *testing.T, res []EmployeeResult, emp string) EmployeeResult {
	t.Helper()
	for _, r := range res {
		if r.EmployeeID == emp {
			return r
		}
	}
	t.Fatalf("employee %s not found", emp)
	return EmployeeResult{}
}

func findWeek(t *testing.T, r EmployeeResult, weekStart time.Time) WeekResult {
	t.Helper()
	for _, w := range r.Weeks {
		if w.WeekStart.Equal(weekStart) {
			return w
		}
	}
	t.Fatalf("week %s not found for %s", weekStart, r.EmployeeID)
	return WeekResult{}
}

func checkWeek(t *testing.T, w WeekResult, reg, ot, dbl int, segs []Segment) {
	t.Helper()
	if w.RegularMinutes != reg || w.OvertimeMinutes != ot || w.DoubleMinutes != dbl {
		t.Errorf("totals = (%d,%d,%d), want (%d,%d,%d)",
			w.RegularMinutes, w.OvertimeMinutes, w.DoubleMinutes, reg, ot, dbl)
	}
	if !reflect.DeepEqual(w.Segments, segs) {
		t.Errorf("segments mismatch:\n got: %+v\nwant: %+v", w.Segments, segs)
	}
}

// 场景 1：周六深夜上班、周日早上下班，中途越过 8 小时的夜班。
// 22:00-08:00（10h），自动扣餐 03:00-03:30；当日 8h 后记加班；周日在周 W1。
func TestNightShiftCrossingMidnightAndWeek(t *testing.T) {
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			p("N1", "E1", PunchIn, tm(10, 3, 22, 0, 0)),
			p("N2", "E1", PunchOut, tm(10, 4, 8, 0, 0)),
		},
	})
	emp := findEmp(t, res, "E1")
	if len(emp.Anomalies) != 0 {
		t.Fatalf("unexpected anomalies: %+v", emp.Anomalies)
	}

	w0 := findWeek(t, emp, tm(9, 27, 0, 0, 0))
	checkWeek(t, w0, 120, 0, 0, []Segment{
		seg(tm(10, 3, 22, 0, 0), tm(10, 4, 0, 0, 0), Regular, "N1"),
	})

	w1 := findWeek(t, emp, tm(10, 4, 0, 0, 0))
	checkWeek(t, w1, 360, 90, 0, []Segment{
		seg(tm(10, 4, 0, 0, 0), tm(10, 4, 3, 0, 0), Regular, "N1"),
		seg(tm(10, 4, 3, 30, 0), tm(10, 4, 6, 30, 0), Regular, "N2"),
		seg(tm(10, 4, 6, 30, 0), tm(10, 4, 8, 0, 0), Overtime, "N2"),
	})
}

// 场景 2：同一日期两个班次合计超过 12 小时。
// 05:00-13:00（自动扣餐 10:00-10:30）+ 14:00-20:00（6h 不扣），合计 13.5h。
func TestTwoShiftsSameDayOver12Hours(t *testing.T) {
	res := Calculate(Input{
		Config: defaultCfg,
		Punches: []Punch{
			p("A1", "E1", PunchIn, tm(9, 28, 5, 0, 0)),
			p("A2", "E1", PunchOut, tm(9, 28, 13, 0, 0)),
			p("B1", "E1", PunchIn, tm(9, 28, 14, 0, 0)),
			p("B2", "E1", PunchOut, tm(9, 28, 20, 0, 0)),
		},
	})
	emp := findEmp(t, res, "E1")
	w := findWeek(t, emp, tm(9, 27, 0, 0, 0))
	checkWeek(t, w, 480, 240, 90, []Segment{
		seg(tm(9, 28, 5, 0, 0), tm(9, 28, 10, 0, 0), Regular, "A1"),
		seg(tm(9, 28, 10, 30, 0), tm(9, 28, 13, 0, 0), Regular, "A2"),
		seg(tm(9, 28, 14, 0, 0), tm(9, 28, 14, 30, 0), Regular, "B1", "B2"),
		seg(tm(9, 28, 14, 30, 0), tm(9, 28, 18, 30, 0), Overtime, "B1", "B2"),
		seg(tm(9, 28, 18, 30, 0), tm(9, 28, 20, 0, 0), Double, "B1", "B2"),
	})
}

// 场景 3：关闭 8 小时规则后，双倍照常执行。
// 06:00-19:00（自动扣餐 11:00-11:30），12.5h：12h 正常 + 0.5h 双倍，无加班。
func TestDoubleTimeWithDailyOTDisabled(t *testing.T) {
	res := Calculate(Input{
		Config: Config{RoundingMinutes: 5, DailyOTEnabled: false},
		Punches: []Punch{
			p("A1", "E1", PunchIn, tm(9, 28, 6, 0, 0)),
			p("A2", "E1", PunchOut, tm(9, 28, 19, 0, 0)),
		},
	})
	emp := findEmp(t, res, "E1")
	w := findWeek(t, emp, tm(9, 27, 0, 0, 0))
	checkWeek(t, w, 720, 0, 30, []Segment{
		seg(tm(9, 28, 6, 0, 0), tm(9, 28, 11, 0, 0), Regular, "A1"),
		seg(tm(9, 28, 11, 30, 0), tm(9, 28, 18, 30, 0), Regular, "A2"),
		seg(tm(9, 28, 18, 30, 0), tm(9, 28, 19, 0, 0), Double, "A2"),
	})
}

// 场景 4：第 40 小时落在班次中间。
// 周一至周五各 09:00-17:00（扣餐 13:00-13:30，每日 7.5h），累计 37.5h；
// 周六 09:00-15:00（6h），前 2.5h 正常，其余 3.5h 改记加班。
func TestWeekly40HoursFallsMidShift(t *testing.T) {
	punches := []Punch{}
	for d := 28; d <= 30; d++ {
		punches = append(punches,
			p("IN"+string(rune('0'+d)), "E1", PunchIn, tm(9, d, 9, 0, 0)),
			p("OUT"+string(rune('0'+d)), "E1", PunchOut, tm(9, d, 17, 0, 0)))
	}
	for d := 1; d <= 2; d++ {
		punches = append(punches,
			p("IN1"+string(rune('0'+d)), "E1", PunchIn, tm(10, d, 9, 0, 0)),
			p("OUT1"+string(rune('0'+d)), "E1", PunchOut, tm(10, d, 17, 0, 0)))
	}
	punches = append(punches,
		p("SAT1", "E1", PunchIn, tm(10, 3, 9, 0, 0)),
		p("SAT2", "E1", PunchOut, tm(10, 3, 15, 0, 0)))

	res := Calculate(Input{Config: defaultCfg, Punches: punches})
	emp := findEmp(t, res, "E1")
	w := findWeek(t, emp, tm(9, 27, 0, 0, 0))
	if w.RegularMinutes != 2400 || w.OvertimeMinutes != 210 || w.DoubleMinutes != 0 {
		t.Fatalf("totals = (%d,%d,%d), want (2400,210,0)",
			w.RegularMinutes, w.OvertimeMinutes, w.DoubleMinutes)
	}
	// 第 40 小时落在周六班次 11:30 处。
	last := w.Segments[len(w.Segments)-2:]
	want := []Segment{
		seg(tm(10, 3, 9, 0, 0), tm(10, 3, 11, 30, 0), Regular, "SAT1", "SAT2"),
		seg(tm(10, 3, 11, 30, 0), tm(10, 3, 15, 0, 0), Overtime, "SAT1", "SAT2"),
	}
	if !reflect.DeepEqual(last, want) {
		t.Errorf("tail segments = %+v, want %+v", last, want)
	}
}
